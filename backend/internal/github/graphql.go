package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrNoToken indica que a operação exige autenticação (GraphQL não aceita
// acesso anônimo). Quem chama deve cair para a API REST.
var ErrNoToken = errors.New("GraphQL exige token do GitHub")

// batchSize é quantos repositórios vão em cada consulta GraphQL e
// batchParallel quantas consultas rodam ao mesmo tempo. Medido com ~230
// repositórios reais: 5 lotes de 50 em série levavam ~30 s (cada consulta
// pesada leva ~6 s no servidor).
var (
	batchSize     = 25
	batchParallel = 3
)

// Snapshot é o estado de um repositório obtido em lote: tudo o que a
// verificação de cache compara, mais a última release com os assets.
type Snapshot struct {
	Repo    Repo
	HeadSHA string
	Release *Release // nil se não houver release estável
	// Manifest é o conteúdo do omastore.toml da raiz no HEAD; nil se o
	// arquivo não existir (ou não for texto, ou for grande demais).
	Manifest *string
}

// ManifestPath é o arquivo buscado junto com o estado de cada repositório.
const ManifestPath = "omastore.toml"

// maxManifestBytes descarta manifestos maiores que isso (o limite do
// parser é o mesmo).
const maxManifestBytes = 64 << 10

// repoFields são os campos pedidos para cada repositório.
const repoFields = `
	nameWithOwner name owner { login } description stargazerCount pushedAt isArchived isFork url
	licenseInfo { spdxId name }
	repositoryTopics(first: 20) { nodes { topic { name } } }
	defaultBranchRef { name target { oid } }
	manifest: object(expression: "HEAD:omastore.toml") { ... on Blob { text byteSize isBinary isTruncated } }
	latestRelease {
		tagName name isPrerelease isDraft publishedAt
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
		rel := &Release{Tag: lr.TagName, Name: lr.Name, PublishedAt: lr.PublishedAt}
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

// Snapshots busca em lote o estado de vários repositórios (uma requisição a
// cada 50). O mapa tem uma entrada por nome pedido; o valor é nil quando o
// repositório não existe. Sem token, retorna ErrNoToken.
func (c *Client) Snapshots(ctx context.Context, names []string) (map[string]*Snapshot, error) {
	if !c.authenticated {
		return nil, ErrNoToken
	}
	// Os lotes rodam em paralelo, com limite baixo: o GitHub desaconselha
	// muitas consultas GraphQL simultâneas (limite secundário).
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
	// Nomes vão como variáveis, nunca interpolados no texto da consulta.
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
	if _, _, err := call(ctx, func() (struct{}, *ghResponse, error) {
		// A requisição é montada a cada tentativa: o corpo é consumido no envio.
		req, err := c.gh.NewRequest(ctx, "POST", "graphql", map[string]any{"query": query, "variables": vars})
		if err != nil {
			return struct{}{}, nil, err
		}
		resp = gqlResponse{}
		r, err := c.gh.Do(req, &resp)
		return struct{}{}, r, err
	}); err != nil {
		return fmt.Errorf("consulta GraphQL: %w", err)
	}

	notFound := map[string]bool{}
	for _, e := range resp.Errors {
		switch e.Type {
		case "NOT_FOUND":
			if len(e.Path) > 0 {
				notFound[e.Path[0]] = true
			}
		case "RATE_LIMITED":
			return &RateLimitError{Reset: time.Now().Add(time.Hour), Err: errors.New(e.Message)}
		default:
			return fmt.Errorf("erro GraphQL (%s): %s", e.Type, e.Message)
		}
	}
	for i, n := range names {
		alias := fmt.Sprintf("r%d", i)
		raw, ok := resp.Data[alias]
		if !ok || string(raw) == "null" {
			if !ok && !notFound[alias] {
				return fmt.Errorf("resposta GraphQL sem %s (%s)", alias, n)
			}
			out[n] = nil
			continue
		}
		var g gqlRepo
		if err := json.Unmarshal(raw, &g); err != nil {
			return fmt.Errorf("decodificar %s: %w", n, err)
		}
		out[n] = g.snapshot()
	}
	return nil
}
