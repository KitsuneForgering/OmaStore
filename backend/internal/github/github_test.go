package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newTest(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := New(Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeJSON(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func TestSearchByTopicPaginates(t *testing.T) {
	mux := http.NewServeMux()
	var srvURL string
	mux.HandleFunc("GET /search/repositories", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "topic:omarchy archived:false fork:false" {
			t.Errorf("q = %q", got)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing token: %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, fixture(t, "search_p2.json"))
			return
		}
		w.Header().Set("Link", `<`+srvURL+`/search/repositories?q=x&page=2>; rel="next"`)
		writeJSON(w, fixture(t, "search_p1.json"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	c, _ := New(Options{BaseURL: srv.URL, Token: "tok"})

	got, err := c.SearchByTopic(context.Background(), "omarchy", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"acme/omaphoto", "acme/omavm", "other/rawmakase"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	two, _ := c.SearchByTopic(context.Background(), "omarchy", 2)
	if len(two) != 2 {
		t.Errorf("max 2: %v", two)
	}
}

func TestGetRepoETag(t *testing.T) {
	mux := http.NewServeMux()
	var hits atomic.Int32
	mux.HandleFunc("GET /repos/acme/omaphoto", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("If-None-Match") == `"abc"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"abc"`)
		writeJSON(w, fixture(t, "repo.json"))
	})
	c := newTest(t, mux)
	ctx := context.Background()

	r, etag, nm, err := c.GetRepo(ctx, "acme/omaphoto", "")
	if err != nil {
		t.Fatal(err)
	}
	if nm || etag != `"abc"` {
		t.Errorf("nm=%v etag=%q", nm, etag)
	}
	if r.Stars != 42 || r.License != "MIT" || r.DefaultBranch != "main" || len(r.Topics) != 2 ||
		!r.PushedAt.Equal(time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)) || r.Owner != "acme" {
		t.Errorf("repo = %+v", r)
	}

	r, etag2, nm, err := c.GetRepo(ctx, "acme/omaphoto", etag)
	if err != nil {
		t.Fatal(err)
	}
	if !nm || r != nil || etag2 != etag {
		t.Errorf("expected 304: nm=%v r=%v etag=%q", nm, r, etag2)
	}
	if hits.Load() != 2 {
		t.Errorf("hits = %d", hits.Load())
	}
}

func TestHeadSHA(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/acme/omaphoto/commits/main", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"deadbeef"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Write([]byte("deadbeef"))
	})
	c := newTest(t, mux)
	sha, err := c.HeadSHA(context.Background(), "acme/omaphoto", "main", "")
	if err != nil || sha != "deadbeef" {
		t.Fatalf("sha=%q err=%v", sha, err)
	}
	sha, err = c.HeadSHA(context.Background(), "acme/omaphoto", "main", "deadbeef")
	if err != nil || sha != "deadbeef" {
		t.Fatalf("304: sha=%q err=%v", sha, err)
	}
}

func TestLatestRelease(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/acme/omaphoto/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, fixture(t, "release.json"))
	})
	mux.HandleFunc("GET /repos/acme/none/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, []byte(`{"message":"Not Found"}`))
	})
	c := newTest(t, mux)
	rel, err := c.LatestRelease(context.Background(), "acme/omaphoto")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v1.2.0" || rel.Body != "## Changes\r\n\r\n- Faster export" || len(rel.Assets) != 2 || rel.Assets[0].Size != 1024 ||
		rel.Assets[0].Digest == "" || rel.Assets[1].Digest != "" {
		t.Errorf("release = %+v", rel)
	}
	none, err := c.LatestRelease(context.Background(), "acme/none")
	if err != nil || none != nil {
		t.Errorf("no release: %v %v", none, err)
	}
}

func TestReadme(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/acme/omaphoto/readme", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, fixture(t, "readme.json"))
	})
	c := newTest(t, mux)
	content, path, err := c.Readme(context.Background(), "acme/omaphoto")
	if err != nil {
		t.Fatal(err)
	}
	if path != "README.md" || content != "# OmaPhoto\n\nPhoto editor.\n" {
		t.Errorf("path=%q content=%q", path, content)
	}
	none, _, err := c.Readme(context.Background(), "acme/x")
	if err != nil || none != "" {
		t.Errorf("no readme: %q %v", none, err)
	}
}

func TestRateLimit(t *testing.T) {
	reset := time.Now().Add(10 * time.Minute).Unix()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/acme/omaphoto", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "60")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		w.WriteHeader(http.StatusForbidden)
		writeJSON(w, []byte(`{"message":"API rate limit exceeded"}`))
	})
	c := newTest(t, mux)
	_, _, _, err := c.GetRepo(context.Background(), "acme/omaphoto", "")
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want RateLimitError", err)
	}
	if rl.Reset.Unix() != reset {
		t.Errorf("reset = %v", rl.Reset)
	}
}

func TestSecondaryRateLimitRetries(t *testing.T) {
	mux := http.NewServeMux()
	var hits atomic.Int32
	mux.HandleFunc("GET /repos/acme/omaphoto/readme", func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusForbidden)
			writeJSON(w, []byte(`{"message":"slow down","documentation_url":"https://docs.github.com/rest/overview/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`))
			return
		}
		writeJSON(w, fixture(t, "readme.json"))
	})
	c := newTest(t, mux)
	if _, _, err := c.Readme(context.Background(), "acme/omaphoto"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Errorf("hits = %d", hits.Load())
	}
}

func TestSplitFullName(t *testing.T) {
	for _, bad := range []string{"", "a", "a/", "/b", "a/b/c"} {
		if _, _, err := SplitFullName(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	o, r, err := SplitFullName("a/b")
	if err != nil || o != "a" || r != "b" {
		t.Errorf("a/b: %q %q %v", o, r, err)
	}
}

func TestTokenFromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", " x ")
	if got := TokenFromEnv(context.Background()); got != "x" {
		t.Errorf("got %q", got)
	}
}

func TestTree(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/acme/omaphoto/git/trees/abc", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("recursive") == "" {
			t.Error("missing recursive")
		}
		writeJSON(w, []byte(`{"sha":"abc","truncated":true,"tree":[
			{"path":"assets","type":"tree"},
			{"path":"assets/icon.svg","type":"blob"},
			{"path":"README.md","type":"blob"}]}`))
	})
	c := newTest(t, mux)
	files, trunc, err := c.Tree(context.Background(), "acme/omaphoto", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if !trunc || len(files) != 2 || files[0] != "assets/icon.svg" {
		t.Errorf("files=%v trunc=%v", files, trunc)
	}
}

func TestGetRepoNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/acme/gone", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, []byte(`{"message":"Not Found"}`))
	})
	c := newTest(t, mux)
	_, _, _, err := c.GetRepo(context.Background(), "acme/gone", "")
	if !errors.Is(err, ErrNotFound) || !IsNotFound(err) {
		t.Errorf("err = %v", err)
	}
}

func TestFile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/acme/app/contents/omastore.toml", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != "abc" {
			t.Errorf("ref = %q", r.URL.Query().Get("ref"))
		}
		writeJSON(w, []byte(`{"type":"file","encoding":"base64","size":11,"path":"omastore.toml","content":"bmFtZSA9ICJ4Igo="}`))
	})
	mux.HandleFunc("GET /repos/acme/empty/contents/omastore.toml", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []byte(`{"type":"file","encoding":"base64","size":0,"content":""}`))
	})
	mux.HandleFunc("GET /repos/acme/big/contents/omastore.toml", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []byte(`{"type":"file","encoding":"base64","size":999999,"content":""}`))
	})
	mux.HandleFunc("GET /repos/acme/dir/contents/omastore.toml", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []byte(`[{"type":"file","name":"a"}]`))
	})
	c := newTest(t, mux)
	ctx := context.Background()
	got, found, err := c.File(ctx, "acme/app", "omastore.toml", "abc", 1024)
	if err != nil || !found || got != "name = \"x\"\n" {
		t.Errorf("got %q %v, %v", got, found, err)
	}
	if got, found, err := c.File(ctx, "acme/none", "omastore.toml", "abc", 1024); err != nil || found || got != "" {
		t.Errorf("missing: %q %v %v", got, found, err)
	}
	if got, found, err := c.File(ctx, "acme/empty", "omastore.toml", "abc", 1024); err != nil || !found || got != "" {
		t.Errorf("empty file must exist: %q %v %v", got, found, err)
	}
	if _, _, err := c.File(ctx, "acme/big", "omastore.toml", "abc", 1024); err == nil {
		t.Error("large file accepted")
	}
	if _, _, err := c.File(ctx, "acme/dir", "omastore.toml", "abc", 1024); err == nil {
		t.Error("directory accepted as a file")
	}
}
