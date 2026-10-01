package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/notify"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/xdg"
)

// isolate points HOME/XDG to a temporary directory and turns off the network.
func isolate(t *testing.T) xdg.Paths {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GITHUB_TOKEN", "test")
	p, err := xdg.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func seedStore(t *testing.T, p xdg.Paths) {
	t.Helper()
	p.Ensure()
	st, err := store.Open(context.Background(), p.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	err = st.SaveIndexed(context.Background(),
		store.Repo{FullName: "acme/omaphoto", Stars: 7, LatestTag: "v1", Topics: []string{"photo"},
			ReleaseNotes: "## Changes\n\n- one\n- two\n- three\n- four"},
		store.App{Name: "OmaPhoto", Summary: "Photo editor", Category: "Graphics", Installable: true, Score: 7},
		[]store.Asset{{Tag: "v1", Name: "omaphoto-linux-amd64", URL: "https://x", Format: "binary", Arch: "amd64"}})
	if err != nil {
		t.Fatal(err)
	}
}

func runCLI(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = run(context.Background(), args, &o, &e)
	return code, o.String(), e.String()
}

func TestUsage(t *testing.T) {
	isolate(t)
	if code, _, errOut := runCLI(); code != 2 || !strings.Contains(errOut, "usage:") {
		t.Errorf("no args: %d %q", code, errOut)
	}
	if code, _, _ := runCLI("nope"); code != 2 {
		t.Errorf("unknown command: %d", code)
	}
	if code, out, _ := runCLI("help"); code != 0 || !strings.Contains(out, "commands:") {
		t.Errorf("help: %d", code)
	}
	if code, _, _ := runCLI("show"); code != 2 {
		t.Errorf("show without repo: %d", code)
	}
	if code, _, _ := runCLI("install"); code != 2 {
		t.Errorf("install without repo: %d", code)
	}
}

func TestListAndShow(t *testing.T) {
	p := isolate(t)
	seedStore(t, p)

	code, out, errOut := runCLI("list")
	if code != 0 || !strings.Contains(out, "acme/omaphoto") || !strings.Contains(out, "Graphics") {
		t.Errorf("list: %d %q %q", code, out, errOut)
	}
	code, out, _ = runCLI("list", "--json", "--category", "System")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Errorf("empty list: %d %q", code, out)
	}
	code, out, _ = runCLI("show", "--json", "acme/omaphoto")
	var d store.AppDetail
	if code != 0 || json.Unmarshal([]byte(out), &d) != nil || d.Name != "OmaPhoto" || len(d.Assets) != 1 {
		t.Errorf("show json: %d %q", code, out)
	}
	code, out, _ = runCLI("show", "acme/omaphoto")
	if code != 0 || !strings.Contains(out, "OmaPhoto (acme/omaphoto)") || !strings.Contains(out, "no checksum") ||
		!strings.Contains(out, "release notes (v1):\n  ## Changes\n\n  - one\n") {
		t.Errorf("show: %q", out)
	}
	if code, _, errOut := runCLI("show", "x/y"); code != 1 || !strings.Contains(errOut, "not found") {
		t.Errorf("show missing: %d %q", code, errOut)
	}
	if code, out, _ := runCLI("categories"); code != 0 || !strings.Contains(out, "Graphics") {
		t.Errorf("categories: %q", out)
	}
	if code, out, _ := runCLI("update"); code != 0 || !strings.Contains(out, "no apps installed") {
		t.Errorf("update: %q", out)
	}
	if code, _, errOut := runCLI("uninstall", "acme/omaphoto"); code != 1 || !strings.Contains(errOut, "not installed") {
		t.Errorf("uninstall: %d %q", code, errOut)
	}
}

func TestLintManifest(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", "icon.svg"), []byte("<svg/>"), 0o644)
	os.WriteFile(filepath.Join(dir, "omastore.toml"), []byte(`
name = "My App"
categories = ["Graphics"]
icon = "assets/icon.svg"
[linux.x86_64]
asset = "myapp-{version}-x86_64-linux.tar.gz"
exec = "bin/myapp"
`), 0o644)
	code, out, errOut := runCLI("lint-manifest", dir)
	if code != 0 || !strings.Contains(out, "ok:") || !strings.Contains(out, "amd64") {
		t.Errorf("valid: %d %q %q", code, out, errOut)
	}

	os.WriteFile(filepath.Join(dir, "omastore.toml"), []byte(`
icn = "typo.svg"
`), 0o644)
	if code, out, _ := runCLI("lint-manifest", dir); code != 1 || !strings.Contains(out, "icn") {
		t.Errorf("unknown field: %d %q", code, out)
	}

	os.WriteFile(filepath.Join(dir, "omastore.toml"), []byte(`
icon = "missing.svg"
screenshots = ["../outside.png"]
`), 0o644)
	code, out, _ = runCLI("lint-manifest", filepath.Join(dir, "omastore.toml"))
	if code != 1 || !strings.Contains(out, "outside.png") {
		t.Errorf("invalid path: %d %q", code, out)
	}
	// With the directory, it detects a missing file.
	code, out, _ = runCLI("lint-manifest", dir)
	if code != 1 || !strings.Contains(out, "missing.svg") {
		t.Errorf("missing file: %d %q", code, out)
	}
	if code, _, _ := runCLI("lint-manifest", filepath.Join(dir, "does-not-exist")); code != 1 {
		t.Errorf("missing: %d", code)
	}
}

type fakeNotifier struct{ sent *[]string }

func (f fakeNotifier) Notify(ctx context.Context, msg notify.Message) error {
	*f.sent = append(*f.sent, msg.Summary+"|"+msg.Body+"|"+strings.Join(msg.Exec, " "))
	return nil
}

func TestUpdateCheck(t *testing.T) {
	p := isolate(t)
	t.Setenv("XDG_STATE_HOME", "")
	seedStore(t, p)
	var sent []string
	old := newNotifier
	newNotifier = func() notify.Notifier { return fakeNotifier{&sent} }
	defer func() { newNotifier = old }()
	oldGUI := guiPath
	guiPath = func() string { return "/usr/bin/omastore-gui" }
	defer func() { guiPath = oldGUI }()

	if code, out, _ := runCLI("update", "--check"); code != 0 || !strings.Contains(out, "everything is up to date") {
		t.Errorf("none installed: %d %q", code, out)
	}
	st, _ := store.Open(context.Background(), p.DB)
	st.SaveInstall(context.Background(), store.Install{FullName: "acme/omaphoto", Version: "v0", InstalledAt: time.Now()})
	st.Close()

	code, out, _ := runCLI("update", "--check", "--notify")
	if code != 0 || !strings.Contains(out, "acme/omaphoto: v0 → v1\n  ## Changes\n  - one\n  - two\n  …\n") || len(sent) != 1 ||
		!strings.Contains(sent[0], "1 update") || !strings.HasSuffix(sent[0], "|/usr/bin/omastore-gui --open acme/omaphoto") {
		t.Errorf("with an update: %d %q %v", code, out, sent)
	}
	// Same set: does not notify again.
	if _, out, _ := runCLI("update", "--check", "--notify"); len(sent) != 1 || !strings.Contains(out, "already notified") {
		t.Errorf("repetiu: %q %v", out, sent)
	}
	if code, _, _ := runCLI("update", "--notify"); code != 2 {
		t.Errorf("--notify without --check: %d", code)
	}
}
