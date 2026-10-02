package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/KitsuneForgering/OmaStore/backend/internal/github"
	"github.com/KitsuneForgering/OmaStore/backend/internal/install"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
	"github.com/KitsuneForgering/OmaStore/backend/internal/xdg"
)

func selfTarball(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		mode int64
	}{{"usr/bin/omastore", 0o755}, {"usr/bin/omastored", 0o755}, {"usr/bin/omastore-gui", 0o755},
		{"usr/share/applications/omastore.desktop", 0o644}, {"usr/share/icons/hicolor/scalable/apps/omastore.svg", 0o644}} {
		body := "new " + filepath.Base(f.name)
		tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// TestSelfStatusAndUpdate goes from the GitHub release to the switched links,
// with a fake API serving OmaStore's latest release.
func TestSelfStatusAndUpdate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	paths, err := xdg.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	paths.Ensure()
	st, err := store.Open(context.Background(), paths.DB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	tarball := selfTarball(t)
	name := install.SelfAssetName("0.2.0", "amd64")
	var releaseCalls int
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + install.SelfRepo + "/releases/latest":
			releaseCalls++
			sum := sha256.Sum256(tarball)
			json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.2.0", "body": "Faster search",
				"assets": []map[string]any{{
					"name": name, "browser_download_url": srv.URL + "/dl/" + name,
					"size": len(tarball), "digest": "sha256:" + hex.EncodeToString(sum[:]),
				}},
			})
		case "/dl/" + name:
			w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	gh, err := github.New(github.Options{BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := install.New(st, paths)
	if err != nil {
		t.Fatal(err)
	}
	inst.HTTP, inst.GOARCH, inst.Hooks = srv.Client(), "amd64", false
	inst.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	a := &App{Paths: paths, Store: st, GitHub: gh, Installer: inst, Log: inst.Log}

	// An install.sh installation of 0.1.0.
	root := inst.SelfRoot()
	os.MkdirAll(paths.BinDir, 0o755)
	for _, b := range []string{"omastore", "omastored", "omastore-gui"} {
		p := filepath.Join(root, "0.1.0", "bin", b)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("old"), 0o755)
		os.Symlink(p, filepath.Join(paths.BinDir, b))
	}
	os.WriteFile(filepath.Join(root, ".managed"), nil, 0o644)

	// A development build compares with nothing and asks GitHub nothing.
	a.self.exe = "/home/dev/OmaStore/bin/omastored"
	if s, err := a.SelfStatus(context.Background()); err != nil || s.Mode != install.SelfDev || releaseCalls != 0 {
		t.Fatalf("dev status = %+v, %v (calls %d)", s, err, releaseCalls)
	}

	a.self.exe = filepath.Join(paths.BinDir, "omastored")
	s, err := a.SelfStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode != install.SelfManaged || s.Version != "0.1.0" || s.Latest != "0.2.0" || !s.UpdateAvailable || s.Notes != "Faster search" {
		t.Errorf("status = %+v", s)
	}
	a.SelfStatus(context.Background())
	if releaseCalls != 1 {
		t.Errorf("release fetched %d times; the status should reuse it", releaseCalls)
	}

	r, err := a.SelfUpdate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.From != "0.1.0" || r.To != "0.2.0" {
		t.Errorf("result = %+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(paths.BinDir, "omastored")); string(b) != "new omastored" {
		t.Errorf("omastored = %q", b)
	}
	// The new binary is now the running one (as after a restart).
	if s, _ := a.SelfStatus(context.Background()); s.Version != "0.2.0" || s.UpdateAvailable {
		t.Errorf("status after the update = %+v", s)
	}
}
