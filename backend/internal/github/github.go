// Package github wraps go-github with what OmaStore needs: topic search,
// repository metadata (with ETag), HEAD, latest release and README.
// Rate limit errors are converted into *RateLimitError.
package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/repoid"
	gh "github.com/google/go-github/v92/github"
)

// Client accesses the GitHub API.
type Client struct {
	gh            *gh.Client
	anonymous     *gh.Client
	authenticated bool
	authRejected  atomic.Bool
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

	baseOpts := []gh.ClientOptionsFunc{gh.WithHTTPClient(hc), gh.WithUserAgent("omastore")}
	if o.BaseURL != "" {
		u := strings.TrimSuffix(o.BaseURL, "/") + "/"
		baseOpts = append(baseOpts, gh.WithURLs(&u, &u))
	}
	anonymous, err := gh.NewClient(baseOpts...)
	if err != nil {
		return nil, fmt.Errorf("create GitHub client: %w", err)
	}
	cl.anonymous = anonymous
	cl.gh = anonymous
	if o.Token != "" {
		authOpts := append(append([]gh.ClientOptionsFunc(nil), baseOpts...), gh.WithAuthToken(o.Token))
		cl.gh, err = gh.NewClient(authOpts...)
		if err != nil {
			return nil, fmt.Errorf("create authenticated GitHub client: %w", err)
		}
	}
	return cl, nil
}

func (c *Client) api() *gh.Client {
	if c.authenticated && !c.authRejected.Load() {
		return c.gh
	}
	return c.anonymous
}

func (c *Client) rejectAuth() {
	if c.authRejected.CompareAndSwap(false, true) {
		slog.Warn("GitHub rejected the configured token; public requests will continue anonymously")
	}
}

// TokenFromEnv checks explicit environment credentials, gh, Git helpers, then ~/.netrc.
func TokenFromEnv(ctx context.Context) string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	if _, err := exec.LookPath("gh"); err == nil {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
		cancel()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return strings.TrimSpace(string(out))
		}
	}
	if token := gitCredentialToken(ctx); token != "" {
		return token
	}
	return netrcToken()
}

func gitCredentialToken(ctx context.Context) string {
	if _, err := exec.LookPath("git"); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "credential", "fill")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	var username, password string
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "username":
			username = value
		case "password":
			password = value
		}
	}
	if username != "" && password != "" {
		return password
	}
	return ""
}

func netrcToken() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".netrc"))
	if err != nil {
		return ""
	}
	tokens, err := netrcTokens(string(b))
	if err != nil {
		return ""
	}
	for i := 0; i < len(tokens); {
		if tokens[i] != "machine" || i+1 >= len(tokens) {
			i++
			continue
		}
		host := tokens[i+1]
		i += 2
		var login, password string
		for i < len(tokens) && tokens[i] != "machine" && tokens[i] != "default" {
			if i+1 >= len(tokens) {
				break
			}
			key, value := tokens[i], tokens[i+1]
			i += 2
			switch key {
			case "login":
				login = value
			case "password":
				password = value
			}
		}
		if (host == "github.com" || host == "api.github.com") && login != "" && password != "" {
			return password
		}
	}
	return ""
}

// netrcTokens handles the quoting and comments used by ordinary .netrc
// machine entries; malformed input is ignored instead of partially trusted.
func netrcTokens(s string) ([]string, error) {
	var tokens []string
	for i := 0; i < len(s); {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
			i++
		}
		if i == len(s) {
			break
		}
		if s[i] == '#' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		var token strings.Builder
		quote := byte(0)
		if s[i] == '\'' || s[i] == '"' {
			quote = s[i]
			i++
		}
		closed := quote == 0
		for i < len(s) {
			ch := s[i]
			if ch == '\\' && i+1 < len(s) {
				i++
				token.WriteByte(s[i])
				i++
				continue
			}
			if quote != 0 {
				if ch == quote {
					i++
					closed = true
					break
				}
				token.WriteByte(ch)
				i++
				continue
			}
			if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
				break
			}
			token.WriteByte(ch)
			i++
		}
		if !closed {
			return nil, errors.New("unterminated .netrc quote")
		}
		tokens = append(tokens, token.String())
	}
	return tokens, nil
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

// callGitHub retries a public read once without credentials after a 401. Once
// rejected, the token is disabled for the rest of this client lifetime.
func callGitHub[T any](ctx context.Context, c *Client, fn func(*gh.Client) (T, *gh.Response, error)) (T, *gh.Response, error) {
	api := c.api()
	v, resp, err := call(ctx, func() (T, *gh.Response, error) { return fn(api) })
	if statusOf(err) != http.StatusUnauthorized || api == c.anonymous || !c.authenticated {
		return v, resp, err
	}
	c.rejectAuth()
	return call(ctx, func() (T, *gh.Response, error) { return fn(c.anonymous) })
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
		res, resp, err := callGitHub(ctx, c, func(api *gh.Client) (*gh.RepositoriesSearchResult, *gh.Response, error) {
			return api.Search.Repositories(ctx, q, opts)
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

// SearchByTopicPushed returns one page from a pushed-date slice. It makes
// exactly one API request so the indexer can enforce a per-run search budget.
func (c *Client) SearchByTopicPushed(ctx context.Context, topic string, after, before time.Time) ([]string, error) {
	q := fmt.Sprintf("topic:%s archived:false fork:false pushed:%s..%s", topic,
		after.UTC().Format("2006-01-02"), before.UTC().Format("2006-01-02"))
	opts := &gh.SearchOptions{Sort: "updated", Order: "desc", ListOptions: gh.ListOptions{PerPage: 100}}
	api := c.api()
	res, _, err := api.Search.Repositories(ctx, q, opts)
	if err != nil {
		if statusOf(err) == http.StatusUnauthorized && api != c.anonymous {
			c.rejectAuth()
		}
		var rl *gh.RateLimitError
		if errors.As(err, &rl) {
			return nil, &RateLimitError{Reset: rl.Rate.Reset.Time, Err: err}
		}
		return nil, fmt.Errorf("search topic:%s pushed:%s..%s: %w", topic,
			after.UTC().Format("2006-01-02"), before.UTC().Format("2006-01-02"), err)
	}
	out := make([]string, 0, len(res.Repositories))
	for _, r := range res.Repositories {
		out = append(out, r.GetFullName())
	}
	return out, nil
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
	r, resp, err := callGitHub(ctx, c, func(api *gh.Client) (*gh.Repository, *gh.Response, error) {
		return api.Repositories.Get(ctx, owner, name)
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
	sha, _, err := callGitHub(ctx, c, func(api *gh.Client) (string, *gh.Response, error) {
		return api.Repositories.GetCommitSHA1(ctx, owner, name, ref, lastSHA)
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
	r, _, err := callGitHub(ctx, c, func(api *gh.Client) (*gh.RepositoryRelease, *gh.Response, error) {
		return api.Repositories.GetLatestRelease(ctx, owner, name)
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
	rc, _, err := callGitHub(ctx, c, func(api *gh.Client) (*gh.RepositoryContent, *gh.Response, error) {
		return api.Repositories.GetReadme(ctx, owner, name, nil)
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
	t, _, err := callGitHub(ctx, c, func(api *gh.Client) (*gh.Tree, *gh.Response, error) {
		return api.Git.GetTree(ctx, owner, name, sha, true)
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
	r, _, err := callGitHub(ctx, c, func(api *gh.Client) (res, *gh.Response, error) {
		fc, _, resp, err := api.Repositories.GetContents(ctx, owner, name, path, &gh.RepositoryContentGetOptions{Ref: ref})
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
	if !c.Authenticated() {
		return nil, ErrNoToken
	}
	if max <= 0 || max > 1000 {
		max = 1000
	}
	opts := &gh.SearchOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	seen := map[string]bool{}
	var out []string
	for {
		api := c.api()
		res, resp, err := call(ctx, func() (*gh.CodeSearchResult, *gh.Response, error) {
			return api.Search.Code(ctx, "filename:"+ManifestPath, opts)
		})
		if err != nil {
			if statusOf(err) == http.StatusUnauthorized {
				c.rejectAuth()
				return out, ErrNoToken
			}
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
func (c *Client) Authenticated() bool { return c.authenticated && !c.authRejected.Load() }

// IsStarred reports whether the authenticated user starred the repository.
func (c *Client) IsStarred(ctx context.Context, fullName string) (bool, error) {
	owner, name, err := c.starTarget(fullName)
	if err != nil {
		return false, err
	}
	api := c.api()
	starred, _, err := call(ctx, func() (bool, *gh.Response, error) {
		return api.Activity.IsStarred(ctx, owner, name)
	})
	if err != nil {
		if statusOf(err) == http.StatusUnauthorized {
			c.rejectAuth()
			return false, ErrNoToken
		}
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
	api := c.api()
	_, _, err = call(ctx, func() (struct{}, *gh.Response, error) {
		var resp *gh.Response
		var err error
		if starred {
			resp, err = api.Activity.Star(ctx, owner, name)
		} else {
			resp, err = api.Activity.Unstar(ctx, owner, name)
		}
		return struct{}{}, resp, err
	})
	if err != nil {
		if statusOf(err) == http.StatusUnauthorized {
			c.rejectAuth()
			return ErrNoToken
		}
		return starError(fullName, err)
	}
	return nil
}

func (c *Client) starTarget(fullName string) (owner, name string, err error) {
	if !c.Authenticated() {
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
