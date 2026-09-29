package imagecache

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func TestGet(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/a.png":
			time.Sleep(20 * time.Millisecond)
			w.Header().Set("Content-Type", "text/html") // the server lies; the content wins
			w.Write(pngHeader)
		case "/logo.svg":
			w.Write([]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`))
		case "/page":
			w.Write([]byte("<html>oi</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Cache{Dir: t.TempDir(), HTTP: srv.Client()}
	ctx := context.Background()

	var wg sync.WaitGroup
	paths := make([]string, 5)
	for i := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := c.Get(ctx, srv.URL+"/a.png")
			if err != nil {
				t.Error(err)
			}
			paths[i] = p
		}()
	}
	wg.Wait()
	if hits.Load() != 1 {
		t.Errorf("concurrent downloads were not shared: %d", hits.Load())
	}
	if filepath.Ext(paths[0]) != ".png" || paths[0] != paths[4] {
		t.Errorf("paths = %v", paths)
	}
	if b, _ := os.ReadFile(paths[0]); string(b) != string(pngHeader) {
		t.Error("wrong content")
	}
	if _, err := c.Get(ctx, srv.URL+"/a.png"); err != nil || hits.Load() != 1 {
		t.Errorf("disk cache not used: %v %d", err, hits.Load())
	}

	if p, err := c.Get(ctx, srv.URL+"/logo.svg"); err != nil || filepath.Ext(p) != ".svg" {
		t.Errorf("svg: %q %v", p, err)
	}
	if _, err := c.Get(ctx, srv.URL+"/page"); !errors.Is(err, ErrNotImage) {
		t.Errorf("html: %v", err)
	}
	if _, err := c.Get(ctx, srv.URL+"/404"); err == nil {
		t.Error("404 should fail")
	}
	for _, bad := range []string{"http://x/a.png", "file:///etc/passwd", "nothing"} {
		if _, err := c.Get(ctx, bad); err == nil {
			t.Errorf("%q aceito", bad)
		}
	}
}
