// Package github embrulha o go-github com o que o OmaStore precisa:
// busca por topic, metadados do repositório (com ETag), HEAD, última release
// e README. Erros de rate limit são convertidos em *RateLimitError.
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

	gh "github.com/google/go-github/v92/github"
)

// Client acessa a API do GitHub.
type Client struct {
	gh            *gh.Client
	authenticated bool
	requests      atomic.Int64
}

// ghResponse é o tipo de resposta do go-github (apelido para os helpers).
type ghResponse = gh.Response

// Requests é o número de requisições HTTP feitas por este cliente.
func (c *Client) Requests() int64 { return c.requests.Load() }

// Options configura New.
type Options struct {
	Token      string       // vazio = anônimo
	BaseURL    string       // vazio = api.github.com; usado nos testes
	HTTPClient *http.Client // opcional
}

// New cria um cliente.
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
		return nil, fmt.Errorf("criar cliente do GitHub: %w", err)
	}
	cl.gh = c
	return cl, nil
}

// TokenFromEnv procura um token em GITHUB_TOKEN, GH_TOKEN e, por fim,
// `gh auth token`. Retorna "" se nenhum existir.
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

// RateLimitError indica que o limite da API acabou até Reset.
type RateLimitError struct {
	Reset time.Time
	Err   error
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit do GitHub esgotado até %s (defina GITHUB_TOKEN ou faça `gh auth login`): %v",
		e.Reset.Local().Format("15:04:05"), e.Err)
}

func (e *RateLimitError) Unwrap() error { return e.Err }

// maxRetryWait é o maior Retry-After de limite secundário que aceitamos esperar.
const maxRetryWait = time.Minute

// call executa fn convertendo erros de rate limit e repetindo uma vez em
// caso de limite secundário com espera curta.
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

// ErrNotFound indica que o recurso não existe (404).
var ErrNotFound = errors.New("não encontrado no GitHub")

// IsNotFound diz se err é um 404 da API.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || statusOf(err) == http.StatusNotFound
}

func isNotModified(err error) bool { return statusOf(err) == http.StatusNotModified }

// SplitFullName separa "owner/repo".
func SplitFullName(fullName string) (owner, repo string, err error) {
	owner, repo, ok := strings.Cut(fullName, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", fmt.Errorf("nome de repositório inválido: %q", fullName)
	}
	return owner, repo, nil
}

// SearchByTopic retorna os repositórios (owner/repo) com o topic, ordenados
// por stars, até max resultados (a API de busca limita a 1000).
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
			return out, fmt.Errorf("buscar topic:%s: %w", topic, err)
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

// Repo são os metadados de um repositório.
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

// GetRepo busca os metadados de um repositório. Se etag não for vazio, faz
// uma requisição condicional: se nada mudou, retorna notModified = true e
// repo nil (requisições 304 não contam no rate limit).
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
		return nil, "", false, fmt.Errorf("buscar repo %s: %w", fullName, err)
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

// HeadSHA retorna o SHA do commit em ref. Com lastSHA, usa requisição
// condicional e retorna lastSHA se nada mudou.
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
		return "", fmt.Errorf("buscar HEAD de %s: %w", fullName, err)
	}
	return sha, nil
}

// Release é uma release publicada.
type Release struct {
	Tag         string
	Name        string
	PublishedAt time.Time
	Prerelease  bool
	Assets      []ReleaseAsset
}

// ReleaseAsset é um arquivo de release.
type ReleaseAsset struct {
	Name        string
	URL         string // browser_download_url
	Size        int64
	ContentType string
	Digest      string // "sha256:<hex>" quando a API informa
}

// LatestRelease retorna a última release estável, ou nil se não houver.
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
		return nil, fmt.Errorf("buscar release de %s: %w", fullName, err)
	}
	out := &Release{
		Tag:         r.GetTagName(),
		Name:        r.GetName(),
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

// Readme retorna o conteúdo (markdown) e o caminho do README, ou "" se não houver.
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
		return "", "", fmt.Errorf("buscar README de %s: %w", fullName, err)
	}
	content, err = rc.GetContent()
	if err != nil {
		return "", "", fmt.Errorf("decodificar README de %s: %w", fullName, err)
	}
	return content, rc.GetPath(), nil
}

// countingTransport conta as requisições (para medir o custo da indexação).
type countingTransport struct {
	base http.RoundTripper
	n    *atomic.Int64
}

func (t countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.n.Add(1)
	return t.base.RoundTrip(req)
}

// etagKey carrega o ETag de uma requisição condicional pelo context.
type etagKey struct{}

func withETag(ctx context.Context, etag string) context.Context {
	if etag == "" {
		return ctx
	}
	return context.WithValue(ctx, etagKey{}, etag)
}

// etagTransport adiciona If-None-Match quando o context traz um ETag.
type etagTransport struct{ base http.RoundTripper }

func (t etagTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if etag, ok := req.Context().Value(etagKey{}).(string); ok && req.Header.Get("If-None-Match") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("If-None-Match", etag)
	}
	return t.base.RoundTrip(req)
}

// Tree lista os arquivos (blobs) do commit sha. truncated indica que a API
// cortou a listagem (repositório grande); nesse caso vale clonar.
func (c *Client) Tree(ctx context.Context, fullName, sha string) (files []string, truncated bool, err error) {
	owner, name, err := SplitFullName(fullName)
	if err != nil {
		return nil, false, err
	}
	t, _, err := call(ctx, func() (*gh.Tree, *gh.Response, error) {
		return c.gh.Git.GetTree(ctx, owner, name, sha, true)
	})
	if err != nil {
		return nil, false, fmt.Errorf("listar árvore de %s: %w", fullName, err)
	}
	for _, e := range t.Entries {
		if e.GetType() == "blob" {
			files = append(files, e.GetPath())
		}
	}
	return files, t.GetTruncated(), nil
}

// File retorna o conteúdo de um arquivo do repositório em ref. found é
// false se ele não existir (um arquivo vazio tem found true). Arquivos
// maiores que maxBytes são recusados.
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
		return "", false, fmt.Errorf("ler %s de %s: %w", path, fullName, err)
	}
	if r.fc == nil {
		return "", false, fmt.Errorf("%s em %s não é um arquivo", path, fullName)
	}
	if r.fc.GetSize() > maxBytes {
		return "", false, fmt.Errorf("%s em %s maior que %d bytes", path, fullName, maxBytes)
	}
	content, err = r.fc.GetContent()
	if err != nil {
		return "", false, fmt.Errorf("decodificar %s de %s: %w", path, fullName, err)
	}
	return content, true, nil
}

// SearchManifests encontra repositórios com omastore.toml na raiz, pela
// busca de código (exige token). Retorna owner/repo em ordem estável.
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
			return out, fmt.Errorf("buscar %s: %w", ManifestPath, err)
		}
		for _, r := range res.CodeResults {
			// Só o arquivo da raiz conta.
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
