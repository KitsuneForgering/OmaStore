package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrNoToken means the operation requires authentication (GraphQL, code
// search and starring do not allow anonymous access). The indexer falls back
// to the REST API.
var ErrNoToken = errors.New("a usable GitHub token is required for this operation (set GITHUB_TOKEN or run `gh auth login`)")

// batchSize is how many repositories go in each GraphQL query and
// batchParallel how many queries run at once. Measured with ~230 real
// repositories: 5 batches of 50 in series took ~30 s (each heavy query
// takes ~6 s on the server).
var (
	batchSize     = 25
	batchParallel = 3
)

// Snapshot is a repository's state fetched in a batch: everything the
// cache check compares, plus the latest release with its assets.
type Snapshot struct {
	Repo    Repo
	HeadSHA string
	Release *Release // nil if there is no stable release
	// Manifest is the content of the root omastore.toml at HEAD; nil if the
	// file does not exist (or is not text, or is too large).
	Manifest *string
}

// ManifestPath is the file fetched along with each repository's state.
const ManifestPath = "omastore.toml"

// maxManifestBytes discards manifests larger than this (the parser's
// limit is the same).
const maxManifestBytes = 64 << 10

// repoFields are the fields requested for each repository.
const repoFields = `
	nameWithOwner name owner { login } description stargazerCount pushedAt isArchived isFork url
	licenseInfo { spdxId name }
	repositoryTopics(first: 20) { nodes { topic { name } } }
	defaultBranchRef { name target { oid } }
	manifest: object(expression: "HEAD:omastore.toml") { ... on Blob { text byteSize isBinary isTruncated } }
	latestRelease {
		tagName name description isPrerelease isDraft publishedAt
		releaseAssets(first: 100) { nodes { name size downloadUrl contentType digest } }
	}`

type gqlRepo struct {
	NameWithOwner string `json:"nameWithOwner"`
	Name          string `json:"name"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
	Description    string    `json:"description"`
	StargazerCount int       `json:"stargazerCount"`
	PushedAt       time.Time `json:"pushedAt"`
	IsArchived     bool      `json:"isArchived"`
	IsFork         bool      `json:"isFork"`
	URL            string    `json:"url"`
	LicenseInfo    *struct {
		SpdxID string `json:"spdxId"`
		Name   string `json:"name"`
	} `json:"licenseInfo"`
	RepositoryTopics struct {
		Nodes []struct {
			Topic struct {
				Name string `json:"name"`
			} `json:"topic"`
		} `json:"nodes"`
	} `json:"repositoryTopics"`
	Manifest *struct {
		Text        *string `json:"text"`
		ByteSize    int     `json:"byteSize"`
		IsBinary    bool    `json:"isBinary"`
		IsTruncated bool    `json:"isTruncated"`
	} `json:"manifest"`
	DefaultBranchRef *struct {
		Name   string `json:"name"`
		Target struct {
			OID string `json:"oid"`
		} `json:"target"`
	} `json:"defaultBranchRef"`
	LatestRelease *struct {
		TagName       string    `json:"tagName"`
		Name          string    `json:"name"`
		Description   string    `json:"description"`
		IsPrerelease  bool      `json:"isPrerelease"`
		IsDraft       bool      `json:"isDraft"`
		PublishedAt   time.Time `json:"publishedAt"`
		ReleaseAssets struct {
			Nodes []struct {
				Name        string `json:"name"`
				Size        int64  `json:"size"`
				DownloadURL string `json:"downloadUrl"`
				ContentType string `json:"contentType"`
				Digest      string `json:"digest"`
			} `json:"nodes"`
		} `json:"releaseAssets"`
	} `json:"latestRelease"`
}

func (g *gqlRepo) snapshot() *Snapshot {
	r := Repo{
		FullName:    g.NameWithOwner,
		Owner:       g.Owner.Login,
		Name:        g.Name,
		Description: g.Description,
		Stars:       g.StargazerCount,
		Topics:      []string{},
		HTMLURL:     g.URL,
		PushedAt:    g.PushedAt,
		Archived:    g.IsArchived,
		Fork:        g.IsFork,
	}
	if g.LicenseInfo != nil {
		r.License = g.LicenseInfo.SpdxID
		if r.License == "" || r.License == "NOASSERTION" {
			r.License = g.LicenseInfo.Name
		}
	}
	for _, n := range g.RepositoryTopics.Nodes {
		r.Topics = append(r.Topics, n.Topic.Name)
	}
	s := &Snapshot{Repo: r}
	if mf := g.Manifest; mf != nil && mf.Text != nil && !mf.IsBinary && !mf.IsTruncated && mf.ByteSize <= maxManifestBytes {
		s.Manifest = mf.Text
	}
	if b := g.DefaultBranchRef; b != nil {
		s.Repo.DefaultBranch = b.Name
		s.HeadSHA = b.Target.OID
	}
	if lr := g.LatestRelease; lr != nil && !lr.IsPrerelease && !lr.IsDraft {
		rel := &Release{Tag: lr.TagName, Name: lr.Name, Body: lr.Description, PublishedAt: lr.PublishedAt}
		for _, a := range lr.ReleaseAssets.Nodes {
			rel.Assets = append(rel.Assets, ReleaseAsset{Name: a.Name, URL: a.DownloadURL, Size: a.Size,
				ContentType: a.ContentType, Digest: a.Digest})
		}
		s.Release = rel
	}
	return s
}

type gqlError struct {
	Type    string   `json:"type"`
	Path    []string `json:"path"`
	Message string   `json:"message"`
}

type gqlResponse struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []gqlError                 `json:"errors"`
}

// Snapshots fetches the state of many repositories in batches. The map has
// one entry per requested name; the value is nil when the repository does not
// exist, and the entry is absent when GraphQL reported another error for that
// repository (the caller should use REST for it). Without a token, returns
// ErrNoToken.
func (c *Client) Snapshots(ctx context.Context, names []string) (map[string]*Snapshot, error) {
	if !c.Authenticated() {
		return nil, ErrNoToken
	}
	// Batches run in parallel with a low limit: GitHub discourages many
	// concurrent GraphQL queries (secondary limit).
	out := make(map[string]*Snapshot, len(names))
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
		sem      = make(chan struct{}, batchParallel)
	)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for start := 0; start < len(names); start += batchSize {
		batch := names[start:min(start+batchSize, len(names))]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			part := map[string]*Snapshot{}
			err := c.snapshotBatch(ctx, batch, part)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				return
			}
			for k, v := range part {
				out[k] = v
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil && len(out) < len(names) {
		return nil, err
	}
	return out, nil
}

func (c *Client) snapshotBatch(ctx context.Context, names []string, out map[string]*Snapshot) error {
	// Names go in as variables, never interpolated into the query text.
	var decl, body strings.Builder
	vars := map[string]any{}
	for i, n := range names {
		owner, repo, err := SplitFullName(n)
		if err != nil {
			return err
		}
		fmt.Fprintf(&decl, "$o%d: String!, $n%d: String!, ", i, i)
		fmt.Fprintf(&body, "r%d: repository(owner: $o%d, name: $n%d) { %s }\n", i, i, i, repoFields)
		vars[fmt.Sprintf("o%d", i)] = owner
		vars[fmt.Sprintf("n%d", i)] = repo
	}
	query := fmt.Sprintf("query(%s) {\n%s}", strings.TrimSuffix(decl.String(), ", "), body.String())

	var resp gqlResponse
	api := c.api()
	if _, _, err := call(ctx, func() (struct{}, *ghResponse, error) {
		// The request is built on every attempt: the body is consumed when sent.
		req, err := api.NewRequest(ctx, "POST", "graphql", map[string]any{"query": query, "variables": vars})
		if err != nil {
			return struct{}{}, nil, err
		}
		resp = gqlResponse{}
		r, err := api.Do(req, &resp)
		return struct{}{}, r, err
	}); err != nil {
		if statusOf(err) == http.StatusUnauthorized {
			c.rejectAuth()
			return ErrNoToken
		}
		return fmt.Errorf("GraphQL query: %w", err)
	}

	notFound := map[string]bool{}
	// failed are aliases with another per-repository error (e.g. FORBIDDEN for
	// a blocked repository): they are left out of the map, so the indexer
	// uses the REST API for them instead of failing the whole batch.
	failed := map[string]bool{}
	for _, e := range resp.Errors {
		switch {
		case e.Type == "RATE_LIMITED":
			return &RateLimitError{Reset: time.Now().Add(time.Hour), Err: errors.New(e.Message)}
		case e.Type == "NOT_FOUND" && len(e.Path) > 0:
			notFound[e.Path[0]] = true
		case len(e.Path) > 0 && strings.HasPrefix(e.Path[0], "r"):
			failed[e.Path[0]] = true
		default:
			return fmt.Errorf("GraphQL error (%s): %s", e.Type, e.Message)
		}
	}
	for i, n := range names {
		alias := fmt.Sprintf("r%d", i)
		if failed[alias] {
			continue
		}
		raw, ok := resp.Data[alias]
		if !ok || string(raw) == "null" {
			if !ok && !notFound[alias] {
				return fmt.Errorf("GraphQL response missing %s (%s)", alias, n)
			}
			out[n] = nil
			continue
		}
		var g gqlRepo
		if err := json.Unmarshal(raw, &g); err != nil {
			return fmt.Errorf("decode %s: %w", n, err)
		}
		out[n] = g.snapshot()
	}
	return nil
}
