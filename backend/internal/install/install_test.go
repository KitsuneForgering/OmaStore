package install

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/manifest"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/xdg"
)

func pngBytes(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, x%h, color.NRGBA{255, 0, 0, 255})
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

type env struct {
	in    *Installer
	st    *store.Store
	paths xdg.Paths
	files map[string][]byte // served by httptest
	url   string
	hits  atomic.Int32
}

func newEnv(t *testing.T) *env {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	paths, err := xdg.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), paths.DB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	e := &env{st: st, paths: paths, files: map[string][]byte{}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.hits.Add(1)
		b, ok := e.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	e.url = srv.URL

	in, err := New(st, paths)
	if err != nil {
		t.Fatal(err)
	}
	in.HTTP = srv.Client()
	in.GOARCH = "amd64"
	in.Hooks = false
	in.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	e.in = in
	return e
}

// publish stores in the store a release of acme/omaphoto with the given asset.
func (e *env) publish(t *testing.T, tag string, asset []byte, format string, digest bool) {
	t.Helper()
	name := "omaphoto-" + tag + "-x86_64-linux." + format
	e.files["/dl/"+name] = asset
	e.files["/icon.png"] = pngBytes(300, 200)
	a := store.Asset{Tag: tag, Name: name, URL: e.url + "/dl/" + name, Arch: index.ArchAMD64,
		Format: format, Size: int64(len(asset))}
	if digest {
		a.Digest = "sha256:" + sha(asset)
	}
	err := e.st.SaveIndexed(context.Background(),
		store.Repo{FullName: "acme/omaphoto", LatestTag: tag, Topics: []string{"omarchy", "photo"}},
		store.App{Name: "OmaPhoto", Summary: "Photo\nX-Evil=1 editor 100%", Category: "Graphics",
			Installable: true, IconURL: e.url + "/icon.png"},
		[]store.Asset{a})
	if err != nil {
		t.Fatal(err)
	}
}

func appTarGz(t *testing.T, version string) []byte {
	return tarGz(t, []entry{
		{name: "omaphoto-" + version + "/omaphoto", body: string(elfBin) + version, mode: 0o755},
		{name: "omaphoto-" + version + "/README.md", body: "x"},
	})
}

func TestInstallUpdateUninstall(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.publish(t, "v1.0.0", appTarGz(t, "1"), index.FormatTarGz, true)

	var stages []string
	inst, err := e.in.Install(ctx, "acme/omaphoto", func(p Progress) {
		if len(stages) == 0 || stages[len(stages)-1] != p.Stage {
			stages = append(stages, p.Stage)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(stages, ",") != "download,verify,extract,integrate,done" {
		t.Errorf("etapas = %v", stages)
	}

	versionDir := filepath.Join(e.paths.AppsDir, "acme__omaphoto", "v1.0.0")
	link := filepath.Join(e.paths.BinDir, "omaphoto")
	desktop := filepath.Join(e.paths.Applications, "omastore-acme-omaphoto.desktop")
	icon := filepath.Join(e.paths.Icons, "256x256", "apps", "omastore-acme-omaphoto.png")

	if target, ok := launcherTarget(link); !ok || target != filepath.Join(versionDir, "omaphoto-1", "omaphoto") {
		t.Errorf("launcher → %q (%v)", target, ok)
	}
	if inst.ExecPath != filepath.Join(versionDir, "omaphoto-1", "omaphoto") {
		t.Errorf("exec = %s", inst.ExecPath)
	}
	if _, err := os.Stat(icon); err != nil {
		t.Errorf("icon: %v", err)
	}
	content, _ := os.ReadFile(desktop)
	for _, want := range []string{
		"Name=OmaPhoto\n",
		"Comment=Photo X-Evil=1 editor 100%\n",
		"Exec=" + inst.ExecPath + "\n",
		"Icon=omastore-acme-omaphoto\n",
		"Categories=Graphics;\n",
		"Terminal=false\n",
		"X-OmaStore-Version=v1.0.0\n",
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf(".desktop without %q:\n%s", want, content)
		}
	}
	if strings.Contains(string(content), "\nX-Evil") {
		t.Error("README line break injected a key into the .desktop")
	}
	for _, f := range inst.Files {
		if !within(e.paths.Home, f) {
			t.Errorf("file outside HOME: %s", f)
		}
	}
	// No leftover staging, backup or temporary files.
	leftovers, _ := filepath.Glob(filepath.Join(e.paths.AppsDir, "acme__omaphoto", ".staging-*"))
	tmps, _ := filepath.Glob(filepath.Join(e.paths.CacheDir, "install-*"))
	if len(leftovers)+len(tmps) != 0 {
		t.Errorf("sobras: %v %v", leftovers, tmps)
	}

	// Update without a new version.
	if _, err := e.in.Update(ctx, "acme/omaphoto", nil); !errors.Is(err, ErrUpToDate) {
		t.Errorf("update without changes: %v", err)
	}

	// Update to v2: swaps the launcher and removes the old version.
	e.publish(t, "v2.0.0", appTarGz(t, "2"), index.FormatTarGz, true)
	inst2, err := e.in.Update(ctx, "acme/omaphoto", nil)
	if err != nil {
		t.Fatal(err)
	}
	if inst2.Version != "v2.0.0" {
		t.Errorf("version = %s", inst2.Version)
	}
	if _, err := os.Stat(versionDir); !os.IsNotExist(err) {
		t.Error("old version was not removed")
	}
	if target, _ := launcherTarget(link); !strings.Contains(target, "/v2.0.0/") {
		t.Errorf("launcher does not point to v2: %s", target)
	}

	// Uninstall: everything registered goes away, and only that.
	keep := filepath.Join(e.paths.BinDir, "other-program")
	os.WriteFile(keep, []byte("x"), 0o755)
	if err := e.in.Uninstall(ctx, "acme/omaphoto"); err != nil {
		t.Fatal(err)
	}
	for _, p := range append(inst2.Files, filepath.Join(e.paths.AppsDir, "acme__omaphoto")) {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s continua existindo", p)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("uninstall removed a foreign file")
	}
	if _, err := e.st.GetInstall(ctx, "acme/omaphoto"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("registro continua: %v", err)
	}
	if err := e.in.Uninstall(ctx, "acme/omaphoto"); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("second uninstall: %v", err)
	}
}

func TestInstallChecksumMismatch(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.publish(t, "v1", appTarGz(t, "1"), index.FormatTarGz, false)
	d, _ := e.st.GetApp(ctx, "acme/omaphoto")
	a := d.Assets[0]
	a.Digest = "sha256:" + strings.Repeat("0", 64)
	e.st.SaveIndexed(ctx, d.Repo, d.App, []store.Asset{a})

	_, err := e.in.Install(ctx, "acme/omaphoto", nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("err = %v", err)
	}
	assertClean(t, e)
}

func TestInstallChecksumFile(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	asset := appTarGz(t, "1")
	e.publish(t, "v1", asset, index.FormatTarGz, false)
	d, _ := e.st.GetApp(ctx, "acme/omaphoto")
	a := d.Assets[0]
	a.ChecksumURL = e.url + "/checksums.txt"
	e.st.SaveIndexed(ctx, d.Repo, d.App, []store.Asset{a})

	e.files["/checksums.txt"] = []byte(sha([]byte("other")) + "  other.tar.gz\n" + sha(asset) + " *" + a.Name + "\n")
	if _, err := e.in.Install(ctx, "acme/omaphoto", nil); err != nil {
		t.Fatalf("checksum certo: %v", err)
	}
	e.in.Uninstall(ctx, "acme/omaphoto")

	e.files["/checksums.txt"] = []byte(sha([]byte("x")) + "  " + a.Name + "\n")
	if _, err := e.in.Install(ctx, "acme/omaphoto", nil); !errors.Is(err, ErrChecksum) {
		t.Errorf("checksum errado: %v", err)
	}
}

func assertClean(t *testing.T, e *env) {
	t.Helper()
	for _, dir := range []string{e.paths.BinDir, e.paths.Applications, e.paths.Icons} {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			var names []string
			filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					names = append(names, p)
				}
				return nil
			})
			if len(names) > 0 {
				t.Errorf("leftover in %s: %v", dir, names)
			}
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(e.paths.AppsDir, "*", "*")); len(matches) > 0 {
		t.Errorf("leftover in apps: %v", matches)
	}
}

func TestInstallRollbackOnConflict(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.publish(t, "v1", appTarGz(t, "1"), index.FormatTarGz, true)
	// A third-party .desktop with the same name: the installation must fail
	// without leaving anything behind and without touching the existing file.
	os.MkdirAll(e.paths.Applications, 0o755)
	foreign := filepath.Join(e.paths.Applications, "omastore-acme-omaphoto.desktop")
	os.WriteFile(foreign, []byte("alheio"), 0o644)

	_, err := e.in.Install(ctx, "acme/omaphoto", nil)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(foreign); string(b) != "alheio" {
		t.Error("foreign file was changed")
	}
	os.Remove(foreign)
	assertClean(t, e)
	if _, err := e.st.GetInstall(ctx, "acme/omaphoto"); !errors.Is(err, store.ErrNotFound) {
		t.Error("installation recorded despite the failure")
	}
}

func TestInstallRollbackRestoresPrevious(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.publish(t, "v1", appTarGz(t, "1"), index.FormatTarGz, true)
	first, err := e.in.Install(ctx, "acme/omaphoto", nil)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(first.DesktopPath)

	// v2 fails at the last step: everything must point to v1 again.
	e.publish(t, "v2", appTarGz(t, "2"), index.FormatTarGz, true)
	testHookBeforeSave = func() error { return errors.New("simulated failure") }
	defer func() { testHookBeforeSave = nil }()
	_, err = e.in.Install(ctx, "acme/omaphoto", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	after, _ := os.ReadFile(first.DesktopPath)
	if !bytes.Equal(before, after) {
		t.Error(".desktop was not restored")
	}
	if target, _ := launcherTarget(filepath.Join(e.paths.BinDir, "omaphoto")); target != first.ExecPath {
		t.Errorf("launcher = %s", target)
	}
	if _, err := os.Stat(first.ExecPath); err != nil {
		t.Errorf("v1 sumiu: %v", err)
	}
	backups, _ := filepath.Glob(filepath.Join(e.paths.Applications, "*.omastore-bak-*"))
	if len(backups) != 0 {
		t.Errorf("backups sobrando: %v", backups)
	}
}

func TestInstallNotInstallable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.st.SaveIndexed(ctx, store.Repo{FullName: "acme/lib", LatestTag: "v1"},
		store.App{Name: "lib"}, []store.Asset{{Tag: "v1", Name: "x-arm64", Arch: "arm64", Format: "binary", URL: "https://x"}})
	if _, err := e.in.Install(ctx, "acme/lib", nil); !errors.Is(err, ErrNotInstallable) {
		t.Errorf("err = %v", err)
	}
	if _, err := e.in.Install(ctx, "acme/none", nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestInstallBinaryAndPkg(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.publish(t, "v1", elfBin, index.FormatBinary, true)
	inst, err := e.in.Install(ctx, "acme/omaphoto", nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(inst.ExecPath) != "omaphoto" {
		t.Errorf("exec = %s", inst.ExecPath)
	}

	pkg := tarZst(t, []entry{
		{name: ".INSTALL", body: "rm -rf ~"},
		{name: "usr/bin/omaphoto", body: string(elfBin), mode: 0o755},
		{name: "usr/share/icons/hicolor/scalable/apps/omaphoto.svg", body: `<svg xmlns="http://www.w3.org/2000/svg"/>`},
	})
	e.publish(t, "v2", pkg, index.FormatPkg, true)
	inst, err = e.in.Install(ctx, "acme/omaphoto", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(inst.ExecPath, "/v2/usr/bin/omaphoto") {
		t.Errorf("exec = %s", inst.ExecPath)
	}
	// The package icon (SVG) takes precedence over the repository's.
	if _, err := os.Stat(filepath.Join(e.paths.Icons, "scalable", "apps", "omastore-acme-omaphoto.svg")); err != nil {
		t.Errorf("package icon: %v", err)
	}
	// The previous version's PNG was cleaned up.
	if _, err := os.Stat(filepath.Join(e.paths.Icons, "256x256", "apps", "omastore-acme-omaphoto.png")); !os.IsNotExist(err) {
		t.Error("old icon not removed")
	}
}

func TestInstallRefusesForeignBinLink(t *testing.T) {
	e := newEnv(t)
	e.publish(t, "v1", appTarGz(t, "1"), index.FormatTarGz, true)
	os.MkdirAll(e.paths.BinDir, 0o755)
	os.WriteFile(filepath.Join(e.paths.BinDir, "omaphoto"), []byte("#!/bin/sh\n"), 0o755)
	_, err := e.in.Install(context.Background(), "acme/omaphoto", nil)
	if !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v", err)
	}
}

func TestInstallRefusesShadowingCommands(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	old := systemBinDirs
	systemBinDirs = []string{dir}
	defer func() { systemBinDirs = old }()
	os.WriteFile(filepath.Join(dir, "omaphoto"), []byte("x"), 0o755)

	e.publish(t, "v1", appTarGz(t, "1"), index.FormatTarGz, true)
	_, err := e.in.Install(context.Background(), "acme/omaphoto", nil)
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "shadowed") {
		t.Fatalf("err = %v", err)
	}
	assertClean(t, e)

	if err := e.in.checkCommandName("omastored"); !errors.Is(err, ErrConflict) {
		t.Errorf("reserved name: %v", err)
	}
}

func TestNewRejectsOutsideHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "/srv/data")
	p, _ := xdg.Resolve()
	if _, err := New(nil, p); !errors.Is(err, ErrOutsideHome) {
		t.Errorf("err = %v", err)
	}
}

func TestSelectAsset(t *testing.T) {
	assets := []store.Asset{
		{Name: "a.AppImage", Format: index.FormatAppImage, Arch: "amd64"},
		{Name: "a-linux.tar.gz", Format: index.FormatTarGz},
		{Name: "a-amd64", Format: index.FormatBinary, Arch: "amd64"},
		{Name: "a-arm64.tar.gz", Format: index.FormatTarGz, Arch: "arm64"},
	}
	if a, _ := SelectAsset(assets, "amd64", nil); a.Name != "a-amd64" {
		t.Errorf("amd64 → %s", a.Name)
	}
	if a, _ := SelectAsset(assets, "arm64", nil); a.Name != "a-arm64.tar.gz" {
		t.Errorf("arm64 → %s", a.Name)
	}
	if a, ok := SelectAsset(assets[1:2], "riscv64", nil); !ok || a.Name != "a-linux.tar.gz" {
		t.Errorf("generic → %s", a.Name)
	}
	if _, ok := SelectAsset(nil, "amd64", nil); ok {
		t.Error("empty")
	}
}

func TestSplitNameAndVersion(t *testing.T) {
	for _, bad := range []string{"a", "a/../b", "../a/b", "a/b/c", "a/.hidden", "a/b c"} {
		if _, _, err := splitName(bad); err == nil {
			t.Errorf("%q aceito", bad)
		}
	}
	if v := sanitizeVersion("../v1.0/x"); strings.Contains(v, "/") || strings.HasPrefix(v, ".") {
		t.Errorf("version = %q", v)
	}
	if sanitizeVersion("..") != "latest" {
		t.Error("..")
	}
}

func TestSelectAssetPrefersManifest(t *testing.T) {
	assets := []store.Asset{
		{Tag: "v2", Name: "a-2-x86_64.tar.gz", Format: index.FormatTarGz, Arch: "amd64"},
		{Tag: "v2", Name: "A-2-x86_64.AppImage", Format: index.FormatAppImage, Arch: "amd64"},
	}
	m, _, _ := manifest.Parse([]byte("[linux.x86_64]\nasset = \"A-{version}-x86_64.AppImage\"\n"), true)
	if a, _ := SelectAsset(assets, "amd64", m); a.Name != "A-2-x86_64.AppImage" {
		t.Errorf("manifest ignored: %s", a.Name)
	}
	// A pattern that matches nothing falls back to the heuristic.
	m2, _, _ := manifest.Parse([]byte("[linux.x86_64]\nasset = \"nothing-{version}\"\n"), true)
	if a, _ := SelectAsset(assets, "amd64", m2); a.Name != "a-2-x86_64.tar.gz" {
		t.Errorf("fallback: %s", a.Name)
	}
}

func TestInstallWithManifest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	// Package with two ELF files; the heuristic would pick "omaphoto" by name,
	// but the manifest declares the other one.
	pkg := tarGz(t, []entry{
		{name: "app/omaphoto", body: string(elfBin) + "helper", mode: 0o755},
		{name: "app/bin/real-app", body: string(elfBin) + "real", mode: 0o755},
		{name: "app/evil", typ: tar.TypeSymlink, link: "real-app"},
	})
	e.publish(t, "v1", pkg, index.FormatTarGz, true)
	d, _ := e.st.GetApp(ctx, "acme/omaphoto")
	m, _, _ := manifest.Parse([]byte(`
categories = ["Graphics", "Photography"]
terminal = true
[linux.x86_64]
exec = "app/bin/real-app"
`), true)
	d.App.Manifest = m.Encode()
	e.st.SaveIndexed(ctx, d.Repo, d.App, d.Assets)

	inst, err := e.in.Install(ctx, "acme/omaphoto", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(inst.ExecPath, "/app/bin/real-app") {
		t.Errorf("exec = %s", inst.ExecPath)
	}
	desktop, _ := os.ReadFile(inst.DesktopPath)
	if !strings.Contains(string(desktop), "Categories=Graphics;Photography;\n") ||
		!strings.Contains(string(desktop), "Terminal=true\n") {
		t.Errorf(".desktop:\n%s", desktop)
	}
}

func TestDeclaredExecRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	os.WriteFile(filepath.Join(dir, "bin", "ok"), elfBin, 0o755)
	os.Symlink("/bin/sh", filepath.Join(dir, "bin", "sh"))
	if _, err := declaredExec(dir, "bin/ok"); err != nil {
		t.Errorf("ok: %v", err)
	}
	for _, bad := range []string{"bin/sh", "../x", "bin"} {
		if _, err := declaredExec(dir, bad); err == nil {
			t.Errorf("%q aceito", bad)
		}
	}
}

func TestInstallExecWithVersionPlaceholder(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	pkg := tarGz(t, []entry{
		{name: "tool-3.1.0-x86_64-linux/bin/tool-cli", body: string(elfBin), mode: 0o755},
		{name: "tool-3.1.0-x86_64-linux/bin/helper", body: string(elfBin) + "h", mode: 0o755},
	})
	e.publish(t, "v3.1.0", pkg, index.FormatTarGz, true)
	d, _ := e.st.GetApp(ctx, "acme/omaphoto")
	m, _, _ := manifest.Parse([]byte("[linux.x86_64]\nexec = \"tool-{version}-x86_64-linux/bin/tool-cli\"\n"), true)
	d.App.Manifest = m.Encode()
	e.st.SaveIndexed(ctx, d.Repo, d.App, d.Assets)
	inst, err := e.in.Install(ctx, "acme/omaphoto", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(inst.ExecPath, "/tool-3.1.0-x86_64-linux/bin/tool-cli") {
		t.Errorf("exec = %s", inst.ExecPath)
	}
}
