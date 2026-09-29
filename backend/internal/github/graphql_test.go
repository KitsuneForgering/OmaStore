package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const gqlRepoJSON = `{
  "nameWithOwner": "pch/rawmakase", "name": "rawmakase", "owner": {"login": "pch"},
  "description": "Free Lightroom alternative", "stargazerCount": 116,
  "pushedAt": "2026-09-28T08:24:36Z", "isArchived": false, "isFork": false,
  "url": "https://github.com/pch/rawmakase", "licenseInfo": {"spdxId": "MIT", "name": "MIT License"},
  "repositoryTopics": {"nodes": [{"topic": {"name": "photo"}}]},
  "defaultBranchRef": {"name": "main", "target": {"oid": "5a02a0e6"}},
  "manifest": {"text": "kind = \"app\"\n", "byteSize": 13, "isBinary": false, "isTruncated": false},
  "latestRelease": {"tagName": "v0.1.6", "name": "", "isPrerelease": false, "isDraft": false,
    "publishedAt": "2026-09-28T05:42:15Z",
    "releaseAssets": {"nodes": [{"name": "rawmakase-0.1.6-x86_64-linux.tar.gz", "size": 10,
      "downloadUrl": "https://github.com/pch/rawmakase/releases/download/v0.1.6/r.tar.gz",
      "contentType": "application/gzip", "digest": "sha256:abc"}]}}
}`

type gqlReq struct {
	Query     string            `json:"query"`
	Variables map[string]string `json:"variables"`
}

func gqlServer(t *testing.T, handle func(w http.ResponseWriter, req gqlReq)) (*Client, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing token")
		}
		b, _ := io.ReadAll(r.Body)
		var req gqlReq
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatalf("corpo: %v (%s)", err, b)
		}
		handle(w, req)
	})
	return newTest(t, mux), &hits
}

func TestSnapshots(t *testing.T) {
	c, hits := gqlServer(t, func(w http.ResponseWriter, req gqlReq) {
		// Names go in variables, never in the query text.
		if strings.Contains(req.Query, "rawmakase") || strings.Contains(req.Query, "does-not-exist") {
			t.Errorf("name interpolated into the query")
		}
		if req.Variables["o0"] != "pch" || req.Variables["n1"] != "nothing" {
			t.Errorf("variables = %v", req.Variables)
		}
		writeJSON(w, []byte(`{"data": {"r0": `+gqlRepoJSON+`, "r1": null},
			"errors": [{"type": "NOT_FOUND", "path": ["r1"], "message": "nope"}]}`))
	})
	got, err := c.Snapshots(context.Background(), []string{"pch/rawmakase", "does-not-exist/nothing"})
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || len(got) != 2 {
		t.Fatalf("hits=%d got=%v", hits.Load(), got)
	}
	if s, ok := got["does-not-exist/nothing"]; !ok || s != nil {
		t.Errorf("missing repo must be nil: %v %v", s, ok)
	}
	s := got["pch/rawmakase"]
	if s.HeadSHA != "5a02a0e6" || s.Repo.Stars != 116 || s.Repo.License != "MIT" || s.Repo.DefaultBranch != "main" ||
		len(s.Repo.Topics) != 1 || s.Release == nil || s.Release.Tag != "v0.1.6" ||
		s.Release.Assets[0].Digest != "sha256:abc" || s.Release.Assets[0].Size != 10 ||
		s.Manifest == nil || *s.Manifest != "kind = \"app\"\n" {
		t.Errorf("snapshot = %+v", s)
	}
	if c.Requests() < 1 {
		t.Error("request counter did not move")
	}
}

func TestSnapshotsBatchesOf50(t *testing.T) {
	old := batchSize
	batchSize = 50
	defer func() { batchSize = old }()
	var mu sync.Mutex
	var sizes []int
	c, hits := gqlServer(t, func(w http.ResponseWriter, req gqlReq) {
		n := len(req.Variables) / 2
		mu.Lock()
		sizes = append(sizes, n)
		mu.Unlock()
		data := map[string]json.RawMessage{}
		for i := 0; i < n; i++ {
			data["r"+itoa(i)] = json.RawMessage("null")
		}
		b, _ := json.Marshal(map[string]any{"data": data})
		// All missing, but without a NOT_FOUND error for r0: also accepted.
		writeJSON(w, b)
	})
	names := make([]string, 120)
	for i := range names {
		names[i] = "o/r" + itoa(i)
	}
	got, err := c.Snapshots(context.Background(), names)
	if err != nil {
		t.Fatal(err)
	}
	sort.Ints(sizes)
	if hits.Load() != 3 || len(got) != 120 || sizes[0] != 20 || sizes[2] != 50 {
		t.Errorf("hits=%d sizes=%v len=%d", hits.Load(), sizes, len(got))
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestSnapshotsErrors(t *testing.T) {
	anon, err := New(Options{BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := anon.Snapshots(context.Background(), []string{"a/b"}); !errors.Is(err, ErrNoToken) {
		t.Errorf("no token: %v", err)
	}

	c, _ := gqlServer(t, func(w http.ResponseWriter, req gqlReq) {
		writeJSON(w, []byte(`{"data": null, "errors": [{"type": "RATE_LIMITED", "message": "API rate limit exceeded"}]}`))
	})
	var rl *RateLimitError
	if _, err := c.Snapshots(context.Background(), []string{"a/b"}); !errors.As(err, &rl) {
		t.Errorf("rate limit: %v", err)
	}

	c2, _ := gqlServer(t, func(w http.ResponseWriter, req gqlReq) {
		writeJSON(w, []byte(`{"data": {"r0": {"nameWithOwner": "a/b", "latestRelease": {"tagName": "v2-rc", "isPrerelease": true}}}}`))
	})
	got, err := c2.Snapshots(context.Background(), []string{"a/b"})
	if err != nil || got["a/b"] == nil || got["a/b"].Release != nil {
		t.Errorf("pre-release must be ignored: %+v %v", got["a/b"], err)
	}

	c3, _ := gqlServer(t, func(w http.ResponseWriter, req gqlReq) {
		writeJSON(w, []byte(`{"data": {}}`))
	})
	if _, err := c3.Snapshots(context.Background(), []string{"a/b"}); err == nil {
		t.Error("response without the alias should be an error")
	}
}

func TestSecondaryRateLimitRetryResendsBody(t *testing.T) {
	var n atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusForbidden)
			writeJSON(w, []byte(`{"message":"slow down","documentation_url":"https://docs.github.com/rest/overview/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`))
			return
		}
		if len(b) == 0 {
			t.Error("second attempt sent an empty body")
		}
		writeJSON(w, []byte(`{"data": {"r0": null}, "errors": [{"type":"NOT_FOUND","path":["r0"]}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL, Token: "tok"})
	if _, err := c.Snapshots(context.Background(), []string{"a/b"}); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Errorf("tentativas = %d", n.Load())
	}
}

func TestSnapshotManifestAbsentOrUnusable(t *testing.T) {
	c, _ := gqlServer(t, func(w http.ResponseWriter, req gqlReq) {
		writeJSON(w, []byte(`{"data": {
			"r0": {"nameWithOwner": "a/none", "manifest": null},
			"r1": {"nameWithOwner": "a/bin", "manifest": {"text": null, "byteSize": 10, "isBinary": true}},
			"r2": {"nameWithOwner": "a/big", "manifest": {"text": "x", "byteSize": 999999, "isBinary": false}}}}`))
	})
	got, err := c.Snapshots(context.Background(), []string{"a/none", "a/bin", "a/big"})
	if err != nil {
		t.Fatal(err)
	}
	for n, s := range got {
		if s.Manifest != nil {
			t.Errorf("%s: manifest should be nil", n)
		}
	}
}

func TestSearchManifests(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/code", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "filename:omastore.toml" {
			t.Errorf("q = %q", r.URL.Query().Get("q"))
		}
		writeJSON(w, []byte(`{"total_count": 3, "items": [
			{"path": "omastore.toml", "repository": {"full_name": "a/app"}},
			{"path": "docs/omastore.toml", "repository": {"full_name": "b/exemplo"}},
			{"path": "omastore.toml", "repository": {"full_name": "A/App"}},
			{"path": "omastore.toml", "repository": {"full_name": "c/other"}}]}`))
	})
	c := newTest(t, mux)
	got, err := c.SearchManifests(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a/app" || got[1] != "c/other" {
		t.Errorf("got %v", got)
	}
	anon, _ := New(Options{BaseURL: "http://127.0.0.1:1"})
	if _, err := anon.SearchManifests(context.Background(), 0); !errors.Is(err, ErrNoToken) {
		t.Errorf("no token: %v", err)
	}
}
