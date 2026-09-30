package install

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/asset"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
)

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
}

// A file that cannot be removed keeps the installation recorded with just the
// pending paths, so uninstalling again finishes the job.
func TestUninstallKeepsPendingFiles(t *testing.T) {
	skipIfRoot(t)
	e := newEnv(t)
	ctx := context.Background()
	e.publish(t, "v1", appTarGz(t, "1"), asset.FormatTarGz, true)
	inst, err := e.in.Install(ctx, "acme/omaphoto", nil)
	if err != nil {
		t.Fatal(err)
	}

	os.Chmod(e.paths.Applications, 0o555)
	defer os.Chmod(e.paths.Applications, 0o755)
	err = e.in.Uninstall(ctx, "acme/omaphoto")
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("err = %v, want ErrIncomplete", err)
	}
	left, err := e.st.GetInstall(ctx, "acme/omaphoto")
	if err != nil {
		t.Fatalf("record dropped with files left behind: %v", err)
	}
	if !slices.Equal(left.Files, []string{inst.DesktopPath}) {
		t.Errorf("pending = %v, want [%s]", left.Files, inst.DesktopPath)
	}
	if _, err := os.Lstat(filepath.Join(e.paths.BinDir, "omaphoto")); !os.IsNotExist(err) {
		t.Error("the removable files should be gone already")
	}

	os.Chmod(e.paths.Applications, 0o755)
	if err := e.in.Uninstall(ctx, "acme/omaphoto"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(inst.DesktopPath); !os.IsNotExist(err) {
		t.Error(".desktop still there after the retry")
	}
	if _, err := e.st.GetInstall(ctx, "acme/omaphoto"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("record left: %v", err)
	}
}

// An old version that cannot be removed during an update stays recorded in
// the new installation instead of being forgotten.
func TestUpdateRecordsLeftoverOldVersion(t *testing.T) {
	skipIfRoot(t)
	e := newEnv(t)
	ctx := context.Background()
	e.publish(t, "v1", appTarGz(t, "1"), asset.FormatTarGz, true)
	if _, err := e.in.Install(ctx, "acme/omaphoto", nil); err != nil {
		t.Fatal(err)
	}
	oldDir := filepath.Join(e.paths.AppsDir, "acme__omaphoto", "v1")
	locked := filepath.Join(oldDir, "omaphoto-1")
	os.Chmod(locked, 0o555)
	defer os.Chmod(locked, 0o755)

	e.publish(t, "v2", appTarGz(t, "2"), asset.FormatTarGz, true)
	inst, err := e.in.Update(ctx, "acme/omaphoto", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(inst.Files, oldDir) {
		t.Errorf("old version not recorded: %v", inst.Files)
	}
	saved, _ := e.st.GetInstall(ctx, "acme/omaphoto")
	if saved == nil || !slices.Contains(saved.Files, oldDir) {
		t.Errorf("old version not saved: %+v", saved)
	}

	os.Chmod(locked, 0o755)
	if err := e.in.Uninstall(ctx, "acme/omaphoto"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.paths.AppsDir, "acme__omaphoto")); !os.IsNotExist(err) {
		t.Error("app directory left behind")
	}
}

// After the indexer moves an installation to the repository's new name
// (store.RenameInstall), updating reuses the launcher and removes the files
// created under the old name.
func TestUpdateAfterRepositoryRename(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.publish(t, "v1", appTarGz(t, "1"), asset.FormatTarGz, true)
	first, err := e.in.Install(ctx, "acme/omaphoto", nil)
	if err != nil {
		t.Fatal(err)
	}

	// The repository moves to neo/omaphoto and publishes v2.
	e.publish(t, "v2", appTarGz(t, "2"), asset.FormatTarGz, true)
	d, err := e.st.GetApp(ctx, "acme/omaphoto")
	if err != nil {
		t.Fatal(err)
	}
	d.Repo.FullName, d.App.FullName = "neo/omaphoto", "neo/omaphoto"
	if err := e.st.SaveIndexed(ctx, d.Repo, d.App, d.Assets); err != nil {
		t.Fatal(err)
	}
	e.st.RemoveRepo(ctx, "acme/omaphoto")
	if err := e.st.RenameInstall(ctx, "acme/omaphoto", "neo/omaphoto"); err != nil {
		t.Fatal(err)
	}

	inst, err := e.in.Update(ctx, "neo/omaphoto", nil)
	if err != nil {
		t.Fatalf("update after rename: %v", err)
	}
	if target, _ := launcherTarget(filepath.Join(e.paths.BinDir, "omaphoto")); !strings.Contains(target, "/neo__omaphoto/v2/") {
		t.Errorf("launcher → %s", target)
	}
	for _, p := range []string{first.DesktopPath, filepath.Join(e.paths.AppsDir, "acme__omaphoto")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s left from the old name", p)
		}
	}
	if !strings.HasSuffix(inst.DesktopPath, "omastore-neo-omaphoto.desktop") {
		t.Errorf("desktop = %s", inst.DesktopPath)
	}
}

// A connection that stops sending data aborts the download instead of
// hanging until the user cancels.
func TestDownloadAbortsWhenStalled(t *testing.T) {
	e := newEnv(t)
	old := downloadIdleTimeout
	downloadIdleTimeout = 100 * time.Millisecond
	defer func() { downloadIdleTimeout = old }()

	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-release
	}))
	defer srv.Close()
	defer close(release)
	e.in.HTTP = srv.Client()

	done := make(chan error, 1)
	go func() {
		_, _, err := e.in.download(context.Background(), srv.URL+"/app", filepath.Join(t.TempDir(), "a"), nil)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errStalled) {
			t.Errorf("err = %v, want errStalled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stalled download did not abort")
	}
}
