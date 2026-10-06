package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/rpc"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

var _ rpc.AutoUpdater = (*App)(nil)

func TestAutoUpdates(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &App{Store: st}

	exe := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("ab", 32)
	for _, c := range []struct {
		repo, installed, exec string
		asset                 store.Asset
	}{
		{"a/verified", "v1", exe, store.Asset{Digest: digest}},
		{"a/checksumfile", "v1", exe, store.Asset{ChecksumURL: "https://x/sums"}},
		{"a/unverified", "v1", exe, store.Asset{}},
		{"a/broken", "v1", filepath.Join(t.TempDir(), "gone"), store.Asset{Digest: digest}},
		{"a/current", "v2", exe, store.Asset{Digest: digest}},
	} {
		as := c.asset
		as.Tag, as.Name, as.URL, as.Arch, as.Format = "v2", "app.tar.gz", "https://x/a", runtime.GOARCH, "tar.gz"
		if err := st.SaveIndexed(ctx, store.Repo{FullName: c.repo, LatestTag: "v2"},
			store.App{Name: c.repo, Installable: true}, []store.Asset{as}); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveInstall(ctx, store.Install{FullName: c.repo, Version: c.installed, ExecPath: c.exec}); err != nil {
			t.Fatal(err)
		}
	}

	ready, waiting, err := a.AutoUpdates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := func(items []store.ListItem) (out []string) {
		for _, it := range items {
			out = append(out, it.FullName)
		}
		return out
	}
	if got := strings.Join(names(ready), ","); got != "a/checksumfile,a/verified" {
		t.Errorf("ready = %s", got)
	}
	if got := strings.Join(names(waiting), ","); got != "a/broken,a/unverified" {
		t.Errorf("waiting = %s", got)
	}

	// On by default; a recent check is not due, an old or missing one is.
	if on, _ := a.AutoUpdate(ctx); !on {
		t.Error("automatic updates should start on")
	}
	if due, _ := a.AutoCheckDue(ctx, time.Hour); !due {
		t.Error("never checked: should be due")
	}
	if err := a.MarkAutoCheck(ctx); err != nil {
		t.Fatal(err)
	}
	if due, _ := a.AutoCheckDue(ctx, time.Hour); due {
		t.Error("just checked: should not be due")
	}
	if due, _ := a.AutoCheckDue(ctx, 0); !due {
		t.Error("a zero interval is always due")
	}
	if err := a.SetAutoUpdate(ctx, false); err != nil {
		t.Fatal(err)
	}
	if due, _ := a.AutoCheckDue(ctx, 0); due {
		t.Error("off: never due")
	}
}
