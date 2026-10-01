// Package github wraps go-github with what OmaStore needs: topic search,
// repository metadata (with ETag), HEAD, latest release and README.
// Rate limit errors are converted into *RateLimitError.
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/repoid"
	gh "github.com/google/go-github/v92/github"
)

// Client accesses the GitHub API.
type Client struct {
	gh            *gh.Client
	authenticated bool
	requests      atomic.Int64
}

// ghResponse is go-github's response type (alias for the helpers).
type ghResponse = gh.Response

// Requests is the number of HTTP requests made by this client.
func (c *Client) Requests() int64 { return c.requests.Load() }

// Options configures New.
type Options struct {
	Token      string       // empty = anonymous
	BaseURL    string       // empty = api.github.com; used by tests
	HTTPClient *http.Client // optional
}

// New creates a client.
func New(o Options) (*Client, error) {
	hc := o.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	base := hc.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	cl := &Client{authenticated: o.Token != ""}
	hc = &http.Client{Transport: countingTransport{etagTransport{base}, &cl.requests}, Timeout: hc.Timeout, Jar: hc.Jar}

	opts := []gh.ClientOptionsFunc{gh.WithHTTPClient(hc), gh.WithUserAgent("omastore")}
	if o.Token != "" {
		opts = append(opts, gh.WithAuthToken(o.Token))
	}
	if o.BaseURL != "" {
		u := strings.TrimSuffix(o.BaseURL, "/") + "/"
		opts = append(opts, gh.WithURLs(&u, &u))
	}
	c, err := gh.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create GitHub client: %w", err)
	}
	cl.gh = c
	return cl, nil
}

// TokenFromEnv looks for a token in GITHUB_TOKEN, GH_TOKEN and, last,
// `gh auth token`. Returns "" if none exists.
func TokenFromEnv(ctx context.Context) string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// RateLimitError means the API limit is exhausted until Reset.
type RateLimitError struct {
	Reset time.Time
	Err   error
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("GitHub rate limit exhausted until %s (set GITHUB_TOKEN or run `gh auth login`): %v",
		e.Reset.Local().Format("15:04:05"), e.Err)
}

func (e *RateLimitError) Unwrap() error { return e.Err }

// maxRetryWait is the longest secondary-limit Retry-After we are willing to wait.
const maxRetryWait = time.Minute

// call runs fn, converting rate limit errors and retrying once on a
// secondary limit with a short wait.
func call[T any](ctx context.Context, fn func() (T, *gh.Response, error)) (T, *gh.Response, error) {
	for attempt := 0; ; attempt++ {
		v, resp, err := fn()
		if err == nil {
			return v, resp, nil
		}
		var rl *gh.RateLimitError
		if errors.As(err, &rl) {
			return v, resp, &RateLimitError{Reset: rl.Rate.Reset.Time, Err: err}
		}
		var abuse *gh.AbuseRateLimitError
		if errors.As(err, &abuse) {
			wait := maxRetryWait
			if abuse.RetryAfter != nil {
				wait = *abuse.RetryAfter
			}
			if attempt > 0 || wait > maxRetryWait {
				return v, resp, &RateLimitError{Reset: time.Now().Add(wait), Err: err}
			}
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return v, resp, ctx.Err()
			}
		}
		return v, resp, err
	}
}

func statusOf(err error) int {
	var er *gh.ErrorResponse
	if errors.As(err, &er) && er.Response != nil {
		return er.Response.StatusCode
	}
	return 0
}

// ErrNotFound means the resource does not exist (404).
var ErrNotFound = errors.New("not found on GitHub")

// IsNotFound reports whether err is an API 404.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || statusOf(err) == http.StatusNotFound
}

func isNotModified(err error) bool { return statusOf(err) == http.StatusNotModified }

// SplitFullName splits "owner/repo".
func SplitFullName(fullName string) (owner, repo string, err error) {
	return repoid.Split(fullName)
}

// SearchByTopic returns the repositories (owner/repo) with the topic, sorted
// by stars, up to max results (the search API caps at 1000).
func (c *Client) SearchByTopic(ctx context.Context, topic string, max int) ([]string, error) {
	if max <= 0 || max > 1000 {
		max = 1000
	}
	q := fmt.Sprintf("topic:%s archived:false fork:false", topic)
	opts := &gh.SearchOptions{Sort: "stars", Order: "desc", ListOptions: gh.ListOptions{PerPage: 100}}
	var out []string
	for {
		res, resp, err := call(ctx, func() (*gh.RepositoriesSearchResult, *gh.Response, error) {
			return c.gh.Search.Repositories(ctx, q, opts)
		})
		if err != nil {
			return out, fmt.Errorf("search topic:%s: %w", topic, err)
		}
		for _, r := range res.Repositories {
			out = append(out, r.GetFullName())
			if len(out) >= max {
				return out, nil
			}
		}
		if resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// Repo is a repository's metadata.
type Repo struct {
	FullName      string
	Owner         string
	Name          string
	Description   string
	Stars         int
	Topics        []string
	License       string
	HTMLURL       string
	DefaultBranch string
	PushedAt      time.Time
	Archived      bool
	Fork          bool
}

// GetRepo fetches a repository's metadata. If etag is not empty, it makes
// a conditional request: if nothing changed, it returns notModified = true and
// a nil repo (304 responses do not count toward the rate limit).
func (c *Client) GetRepo(ctx context.Context, fullName, etag string) (repo *Repo, newETag string, notModified bool, err error) {
	owner, name, err := SplitFullName(fullName)
	if err != nil {
		return nil, "", false, err
	}
	ctx = withETag(ctx, etag)
	r, resp, err := call(ctx, func() (*gh.Repository, *gh.Response, error) {
		return c.gh.Repositories.Get(ctx, owner, name)
	})
	if isNotModified(err) {
		return nil, etag, true, nil
	}
	if IsNotFound(err) {
		return nil, "", false, fmt.Errorf("repo %s: %w", fullName, ErrNotFound)
	}
	if err != nil {
		return nil, "", false, fmt.Errorf("fetch repo %s: %w", fullName, err)
	}
	out := &Repo{
		FullName:      r.GetFullName(),
		Owner:         r.GetOwner().GetLogin(),
		Name:          r.GetName(),
		Description:   r.GetDescription(),
		Stars:         r.GetStargazersCount(),
		Topics:        r.Topics,
		HTMLURL:       r.GetHTMLURL(),
		DefaultBranch: r.GetDefaultBranch(),
		PushedAt:      r.GetPushedAt().Time,
		Archived:      r.GetArchived(),
		Fork:          r.GetFork(),
	}
	if l := r.GetLicense(); l != nil {
		out.License = l.GetSPDXID()
		if out.License == "" || out.License == "NOASSERTION" {
			out.License = l.GetName()
		}
	}
	if out.Topics == nil {
		out.Topics = []string{}
	}
	return out, resp.Header.Get("ETag"), false, nil
}

// HeadSHA returns the commit SHA at ref. With lastSHA, it makes a conditional
// request and returns lastSHA if nothing changed.
func (c *Client) HeadSHA(ctx context.Context, fullName, ref, lastSHA string) (string, error) {
	owner, name, err := SplitFullName(fullName)
	if err != nil {
		return "", err
	}
	if ref == "" {
		ref = "HEAD"
	}
	sha, _, err := call(ctx, func() (string, *gh.Response, error) {
		return c.gh.Repositories.GetCommitSHA1(ctx, owner, name, ref, lastSHA)
	})
	if isNotModified(err) {
		return lastSHA, nil
	}
	if err != nil {
		return "", fmt.Errorf("fetch HEAD of %s: %w", fullName, err)
	}
	return sha, nil
}

// Release is a published release.
type Release struct {
	Tag         string
	Name        string
	Body        string // release notes (markdown), as the author wrote them
	PublishedAt time.Time
	Prerelease  bool
	Assets      []ReleaseAsset
}

// ReleaseAsset is a release file.
type ReleaseAsset struct {
	Name        string
	URL         string // browser_download_url
	Size        int64
	ContentType string
	Digest      string // "sha256:<hex>" when the API provides it
}

// LatestRelease returns the latest stable release, or nil if there is none.
func (c *Client) LatestRelease(ctx context.Context, fullName string) (*Release, error) {
	owner, name, err := SplitFullName(fullName)
	if err != nil {
		return nil, err
	}
	r, _, err := call(ctx, func() (*gh.RepositoryRelease, *gh.Response, error) {
		return c.gh.Repositories.GetLatestRelease(ctx, owner, name)
	})
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch release of %s: %w", fullName, err)
	}
	out := &Release{
		Tag:         r.GetTagName(),
		Name:        r.GetName(),
		Body:        r.GetBody(),
		PublishedAt: r.GetPublishedAt().Time,
		Prerelease:  r.GetPrerelease(),
	}
	for _, a := range r.Assets {
		out.Assets = append(out.Assets, ReleaseAsset{
			Name:        a.GetName(),
			URL:         a.GetBrowserDownloadURL(),
			Size:        int64(a.GetSize()),
			ContentType: a.GetContentType(),
			Digest:      a.GetDigest(),
		})
	}
	return out, nil
}

// Readme returns the README content (markdown) and path, or "" if there is none.
func (c *Client) Readme(ctx context.Context, fullName string) (content, path string, err error) {
	owner, name, err := SplitFullName(fullName)
	if err != nil {
		return "", "", err
	}
	rc, _, err := call(ctx, func() (*gh.RepositoryContent, *gh.Response, error) {
		return c.gh.Repositories.GetReadme(ctx, owner, name, nil)
	})
	if IsNotFound(err) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("fetch README of %s: %w", fullName, err)
	}
	content, err = rc.GetContent()
	if err != nil {
		return "", "", fmt.Errorf("decode README of %s: %w", fullName, err)
	}
	return content, rc.GetPath(), nil
}

// countingTransport counts requests (to measure the cost of indexing).
type countingTransport struct {
	base http.RoundTripper
	n    *atomic.Int64
}

func (t countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.n.Add(1)
	return t.base.RoundTrip(req)
}

// etagKey carries a conditional request's ETag through the context.
type etagKey struct{}

func withETag(ctx context.Context, etag string) context.Context {
	if etag == "" {
		return ctx
	}
	return context.WithValue(ctx, etagKey{}, etag)
}

// etagTransport adds If-None-Match when the context carries an ETag.
type etagTransport struct{ base http.RoundTripper }

func (t etagTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if etag, ok := req.Context().Value(etagKey{}).(string); ok && req.Header.Get("If-None-Match") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("If-None-Match", etag)
	}
	return t.base.RoundTrip(req)
}

// Tree lists the files (blobs) of commit sha. truncated means the API cut the
// listing short (large repository); in that case cloning is worth it.
func (c *Client) Tree(ctx context.Context, fullName, sha string) (files []string, truncated bool, err error) {
	owner, name, err := SplitFullName(fullName)
	if err != nil {
		return nil, false, err
	}
	t, _, err := call(ctx, func() (*gh.Tree, *gh.Response, error) {
		return c.gh.Git.GetTree(ctx, owner, name, sha, true)
	})
	if err != nil {
		return nil, false, fmt.Errorf("list tree of %s: %w", fullName, err)
	}
	for _, e := range t.Entries {
		if e.GetType() == "blob" {
			files = append(files, e.GetPath())
		}
	}
	return files, t.GetTruncated(), nil
}

// File returns the content of a repository file at ref. found is false if
// it does not exist (an empty file has found true). Files larger than
// maxBytes are rejected.
func (c *Client) File(ctx context.Context, fullName, path, ref string, maxBytes int) (content string, found bool, err error) {
	owner, name, err := SplitFullName(fullName)
	if err != nil {
		return "", false, err
	}
	type res struct{ fc *gh.RepositoryContent }
	r, _, err := call(ctx, func() (res, *gh.Response, error) {
		fc, _, resp, err := c.gh.Repositories.GetContents(ctx, owner, name, path, &gh.RepositoryContentGetOptions{Ref: ref})
		return res{fc}, resp, err
	})
	if IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read %s of %s: %w", path, fullName, err)
	}
	if r.fc == nil {
		return "", false, fmt.Errorf("%s in %s is not a file", path, fullName)
	}
	if r.fc.GetSize() > maxBytes {
		return "", false, fmt.Errorf("%s in %s is larger than %d bytes", path, fullName, maxBytes)
	}
	content, err = r.fc.GetContent()
	if err != nil {
		return "", false, fmt.Errorf("decode %s of %s: %w", path, fullName, err)
	}
	return content, true, nil
}

// SearchManifests finds repositories with omastore.toml at the root through
// code search (requires a token). Returns owner/repo in a stable order.
func (c *Client) SearchManifests(ctx context.Context, max int) ([]string, error) {
	if !c.authenticated {
		return nil, ErrNoToken
	}
	if max <= 0 || max > 1000 {
		max = 1000
	}
	opts := &gh.SearchOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	seen := map[string]bool{}
	var out []string
	for {
		res, resp, err := call(ctx, func() (*gh.CodeSearchResult, *gh.Response, error) {
			return c.gh.Search.Code(ctx, "filename:"+ManifestPath, opts)
		})
		if err != nil {
			return out, fmt.Errorf("search %s: %w", ManifestPath, err)
		}
		for _, r := range res.CodeResults {
			// Only the root file counts.
			if r.GetPath() != ManifestPath {
				continue
			}
			name := r.GetRepository().GetFullName()
			if name == "" || seen[strings.ToLower(name)] {
				continue
			}
			seen[strings.ToLower(name)] = true
			out = append(out, name)
			if len(out) >= max {
				return out, nil
			}
		}
		if resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// ErrStarForbidden means the token cannot star repositories (a fine-grained
// token without the "Starring" permission, or a revoked one).
var ErrStarForbidden = errors.New("the GitHub token is not allowed to star repositories")

// Authenticated reports whether the client has a token.
func (c *Client) Authenticated() bool { return c.authenticated }

// IsStarred reports whether the authenticated user starred the repository.
func (c *Client) IsStarred(ctx context.Context, fullName string) (bool, error) {
	owner, name, err := c.starTarget(fullName)
	if err != nil {
		return false, err
	}
	starred, _, err := call(ctx, func() (bool, *gh.Response, error) {
		return c.gh.Activity.IsStarred(ctx, owner, name)
	})
	if err != nil {
		return false, starError(fullName, err)
	}
	return starred, nil
}

// SetStarred stars (or unstars) the repository as the authenticated user.
// Both are idempotent on GitHub.
func (c *Client) SetStarred(ctx context.Context, fullName string, starred bool) error {
	owner, name, err := c.starTarget(fullName)
	if err != nil {
		return err
	}
	_, _, err = call(ctx, func() (struct{}, *gh.Response, error) {
		var resp *gh.Response
		var err error
		if starred {
			resp, err = c.gh.Activity.Star(ctx, owner, name)
		} else {
			resp, err = c.gh.Activity.Unstar(ctx, owner, name)
		}
		return struct{}{}, resp, err
	})
	if err != nil {
		return starError(fullName, err)
	}
	return nil
}

func (c *Client) starTarget(fullName string) (owner, name string, err error) {
	if !c.authenticated {
		return "", "", ErrNoToken
	}
	return SplitFullName(fullName)
}

func starError(fullName string, err error) error {
	switch statusOf(err) {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("star %s: %w: %v", fullName, ErrStarForbidden, err)
	case http.StatusNotFound:
		return fmt.Errorf("star %s: %w", fullName, ErrNotFound)
	}
	return fmt.Errorf("star %s: %w", fullName, err)
}
