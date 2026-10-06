package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// selfEnv is an install.sh installation of version cur, with links, menu
// entry and skill copies, plus a release tarball of next served over https.
type selfEnv struct {
	*env
	root string
	cur  SelfInstall
	rel  SelfRelease
}

func newSelfEnv(t *testing.T, cur, next string) *selfEnv {
	t.Helper()
	e := newEnv(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	root := e.in.SelfRoot()
	for _, b := range selfBinaries {
		p := filepath.Join(root, cur, "bin", b)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("old "+b), 0o755)
		os.MkdirAll(e.paths.BinDir, 0o755)
		if err := os.Symlink(p, filepath.Join(e.paths.BinDir, b)); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(root, selfMarker), nil, 0o644)
	os.MkdirAll(e.paths.Applications, 0o755)
	os.WriteFile(filepath.Join(e.paths.Applications, "omastore.desktop"), []byte("[Desktop Entry]\nName=Old\n"), 0o644)
	skills := filepath.Join(e.paths.Home, ".claude", "skills")
	os.MkdirAll(filepath.Join(skills, "omastore-check"), 0o755)
	os.WriteFile(filepath.Join(skills, "omastore-check", "SKILL.md"), []byte("old skill"), 0o644)
	os.WriteFile(filepath.Join(skills, "omastore-check", skillMarker), nil, 0o644)
	os.MkdirAll(filepath.Join(skills, "omastore-release"), 0o755)             // the user's own: no marker
	gone := filepath.Join(e.paths.Home, ".agents", "skills", "omastore-gone") // dropped by the release
	os.MkdirAll(gone, 0o755)
	os.WriteFile(filepath.Join(gone, skillMarker), nil, 0o644)
	os.MkdirAll(filepath.Join(e.paths.Home, ".hermes", "profiles", "work"), 0o755)
	os.WriteFile(filepath.Join(skills, "omastore-release", "SKILL.md"), []byte("mine"), 0o644)

	name := SelfAssetName(next, "amd64")
	data := tarGz(t, []entry{
		{name: "usr/bin/omastore", body: "new omastore", mode: 0o755},
		{name: "usr/bin/omastored", body: "new omastored", mode: 0o755},
		{name: "usr/bin/omastore-gui", body: "new omastore-gui", mode: 0o755},
		{name: selfDesktop, body: "[Desktop Entry]\nName=OmaStore\nExec=omastore-gui %u\nIcon=omastore\n", mode: 0o644},
		{name: selfIcon, body: "<svg/>", mode: 0o644},
		{name: selfSkills + "/omastore-check/SKILL.md", body: "new skill", mode: 0o644},
		{name: selfSkills + "/omastore-release/SKILL.md", body: "new release skill", mode: 0o644},
	})
	e.files["/dl/"+name] = data
	return &selfEnv{env: e, root: root,
		cur: SelfInstall{Mode: SelfManaged, Version: cur, Root: root},
		rel: SelfRelease{Tag: "v" + next, AssetURL: e.url + "/dl/" + name, Digest: "sha256:" + sha(data)}}
}

func (s *selfEnv) link(t *testing.T, name string) string {
	t.Helper()
	target, err := os.Readlink(filepath.Join(s.paths.BinDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDetectSelf(t *testing.T) {
	s := newSelfEnv(t, "0.1.0", "0.2.0")
	got := s.in.DetectSelf(filepath.Join(s.paths.BinDir, "omastored")) // through the link
	if got.Mode != SelfManaged || got.Version != "0.1.0" {
		t.Errorf("install.sh link = %+v", got)
	}
	if got := s.in.DetectSelf("/usr/bin/omastored"); got.Mode != SelfPackage {
		t.Errorf("/usr/bin = %+v", got)
	}
	if got := s.in.DetectSelf("/home/dev/OmaStore/bin/omastored"); got.Mode != SelfDev {
		t.Errorf("dev build = %+v", got)
	}
	os.Remove(filepath.Join(s.root, selfMarker))
	if got := s.in.DetectSelf(filepath.Join(s.root, "0.1.0", "bin", "omastored")); got.Mode != SelfDev {
		t.Errorf("without the install.sh marker = %+v", got)
	}
}

func TestSelfUpdate(t *testing.T) {
	s := newSelfEnv(t, "0.1.0", "0.2.0")
	os.MkdirAll(filepath.Join(s.root, "0.0.9", "bin"), 0o755) // two versions back: pruned
	var stages []string
	r, err := s.in.SelfUpdate(context.Background(), s.cur, s.rel, func(p Progress) {
		if len(stages) == 0 || stages[len(stages)-1] != p.Stage {
			stages = append(stages, p.Stage)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.From != "0.1.0" || r.To != "0.2.0" || r.GUI != filepath.Join(s.paths.BinDir, "omastore-gui") {
		t.Errorf("result = %+v", r)
	}
	if want := "download verify extract integrate done"; strings.Join(stages, " ") != want {
		t.Errorf("stages = %v, want %s", stages, want)
	}
	for _, b := range selfBinaries {
		if got, want := s.link(t, b), filepath.Join(s.root, "0.2.0", "bin", b); got != want {
			t.Errorf("%s -> %s, want %s", b, got, want)
		}
		if got := readFile(t, filepath.Join(s.paths.BinDir, b)); got != "new "+b {
			t.Errorf("%s = %q", b, got)
		}
	}
	// The running version stays (its processes use it); older ones go.
	if _, err := os.Stat(filepath.Join(s.root, "0.1.0", "bin", "omastored")); err != nil {
		t.Errorf("previous version removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.root, "0.0.9")); !os.IsNotExist(err) {
		t.Errorf("old version kept: %v", err)
	}
	desktop := readFile(t, filepath.Join(s.paths.Applications, "omastore.desktop"))
	if !strings.Contains(desktop, "Exec="+filepath.Join(s.paths.BinDir, "omastore-gui")+" %u\n") {
		t.Errorf("menu entry Exec not pointed at the launcher:\n%s", desktop)
	}
	if got := readFile(t, filepath.Join(s.paths.Icons, "scalable", "apps", "omastore.svg")); got != "<svg/>" {
		t.Errorf("icon = %q", got)
	}
	skills := filepath.Join(s.paths.Home, ".claude", "skills")
	if got := readFile(t, filepath.Join(skills, "omastore-check", "SKILL.md")); got != "new skill" {
		t.Errorf("managed skill = %q", got)
	}
	if _, err := os.Stat(filepath.Join(skills, "omastore-check", skillMarker)); err != nil {
		t.Errorf("managed skill lost its marker: %v", err)
	}
	if got := readFile(t, filepath.Join(skills, "omastore-release", "SKILL.md")); got != "mine" {
		t.Errorf("the user's skill was replaced: %q", got)
	}
	// Every installed agent gets them (Omarchy's directories), missing ones too.
	for _, dir := range []string{".agents/skills", ".hermes/profiles/work/skills"} {
		p := filepath.Join(s.paths.Home, dir, "omastore-release", "SKILL.md")
		if got := readFile(t, p); got != "new release skill" {
			t.Errorf("%s = %q", p, got)
		}
	}
	if _, err := os.Stat(filepath.Join(s.paths.Home, ".codex")); !os.IsNotExist(err) {
		t.Errorf("an agent that is not installed got a skills directory: %v", err)
	}
	// A skill the release dropped goes, when it is ours.
	if _, err := os.Stat(filepath.Join(s.paths.Home, ".agents", "skills", "omastore-gone")); !os.IsNotExist(err) {
		t.Errorf("dropped skill kept: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(s.root, ".staging-*")); len(left) > 0 {
		t.Errorf("staging left behind: %v", left)
	}
	if left, _ := filepath.Glob(filepath.Join(s.paths.BinDir, "*.omastore-bak-*")); len(left) > 0 {
		t.Errorf("backups left behind: %v", left)
	}

	// Already at the latest version.
	s.cur.Version = "0.2.0"
	if _, err := s.in.SelfUpdate(context.Background(), s.cur, s.rel, nil); !errors.Is(err, ErrUpToDate) {
		t.Errorf("same version: %v", err)
	}
}

// unchanged checks that a failed update left the installation as it was.
func (s *selfEnv) unchanged(t *testing.T) {
	t.Helper()
	for _, b := range selfBinaries {
		if got, want := s.link(t, b), filepath.Join(s.root, "0.1.0", "bin", b); got != want {
			t.Errorf("%s -> %s after a failed update", b, got)
		}
	}
	if _, err := os.Stat(filepath.Join(s.root, "0.2.0")); !os.IsNotExist(err) {
		t.Errorf("new version directory left behind: %v", err)
	}
	if got := readFile(t, filepath.Join(s.paths.Applications, "omastore.desktop")); !strings.Contains(got, "Name=Old") {
		t.Errorf("menu entry changed: %q", got)
	}
}

func TestSelfUpdateRefusals(t *testing.T) {
	ctx := context.Background()
	t.Run("checksum mismatch", func(t *testing.T) {
		s := newSelfEnv(t, "0.1.0", "0.2.0")
		s.rel.Digest = "sha256:" + strings.Repeat("0", 64)
		if _, err := s.in.SelfUpdate(ctx, s.cur, s.rel, nil); !errors.Is(err, ErrChecksum) {
			t.Fatalf("err = %v", err)
		}
		s.unchanged(t)
	})
	t.Run("no checksum", func(t *testing.T) {
		s := newSelfEnv(t, "0.1.0", "0.2.0")
		s.rel.Digest = ""
		if _, err := s.in.SelfUpdate(ctx, s.cur, s.rel, nil); !errors.Is(err, ErrSelfUnverified) {
			t.Fatalf("err = %v", err)
		}
		s.unchanged(t)
	})
	t.Run("checksum file", func(t *testing.T) {
		s := newSelfEnv(t, "0.1.0", "0.2.0")
		name := filepath.Base(s.rel.AssetURL)
		s.files["/dl/"+name+".sha256"] = []byte(strings.TrimPrefix(s.rel.Digest, "sha256:") + "  " + name + "\n")
		s.rel.Digest, s.rel.ChecksumURL = "", s.rel.AssetURL+".sha256"
		if _, err := s.in.SelfUpdate(ctx, s.cur, s.rel, nil); err != nil {
			t.Fatalf("with a .sha256: %v", err)
		}
	})
	t.Run("not install.sh", func(t *testing.T) {
		s := newSelfEnv(t, "0.1.0", "0.2.0")
		for _, mode := range []string{SelfPackage, SelfDev} {
			s.cur.Mode = mode
			if _, err := s.in.SelfUpdate(ctx, s.cur, s.rel, nil); !errors.Is(err, ErrSelfNotManaged) {
				t.Errorf("%s: %v", mode, err)
			}
		}
	})
	t.Run("wrong asset", func(t *testing.T) {
		s := newSelfEnv(t, "0.1.0", "0.2.0")
		s.in.GOARCH = "arm64"
		if _, err := s.in.SelfUpdate(ctx, s.cur, s.rel, nil); !errors.Is(err, ErrSelfNoAsset) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("link not ours", func(t *testing.T) {
		s := newSelfEnv(t, "0.1.0", "0.2.0")
		gui := filepath.Join(s.paths.BinDir, "omastore-gui")
		os.Remove(gui)
		os.WriteFile(gui, []byte("someone else's"), 0o755)
		if _, err := s.in.SelfUpdate(ctx, s.cur, s.rel, nil); !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v", err)
		}
		if got := readFile(t, gui); got != "someone else's" {
			t.Errorf("foreign file replaced: %q", got)
		}
		for _, b := range []string{"omastore", "omastored"} {
			if got, want := s.link(t, b), filepath.Join(s.root, "0.1.0", "bin", b); got != want {
				t.Errorf("%s not rolled back: %s", b, got)
			}
		}
		if _, err := os.Stat(filepath.Join(s.root, "0.2.0")); !os.IsNotExist(err) {
			t.Errorf("new version directory left behind: %v", err)
		}
	})
	t.Run("missing binary", func(t *testing.T) {
		s := newSelfEnv(t, "0.1.0", "0.2.0")
		name := filepath.Base(s.rel.AssetURL)
		data := tarGz(t, []entry{{name: "usr/bin/omastore", body: "x", mode: 0o755}})
		s.files["/dl/"+name] = data
		s.rel.Digest = "sha256:" + sha(data)
		if _, err := s.in.SelfUpdate(ctx, s.cur, s.rel, nil); err == nil || !strings.Contains(err.Error(), "omastored") {
			t.Fatalf("err = %v", err)
		}
		s.unchanged(t)
	})
}

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.10.0", "0.9.0", true},
		{"1.0.0", "1.0.0", false},
		{"0.1.0", "0.2.0", false},
		{"1.0.0", "1.0.0-rc.1", true},
		{"1.0.0-rc.1", "1.0.0", false},
		{"1.0.0-rc.10", "1.0.0-rc.2", true},
		{"1.0.0-rc.1", "1.0.0-beta", true},
		{"1.0.0-alpha.1", "1.0.0-alpha", true},
	} {
		if got := NewerVersion(c.a, c.b); got != c.want {
			t.Errorf("NewerVersion(%s, %s) = %v", c.a, c.b, got)
		}
	}
	for tag, ok := range map[string]bool{"v1.2.3": true, "v1.2.3-rc.1": true, "1.2.3": false, "v1.2": false, "v1.2.3/..": false} {
		if _, got := SelfTag(tag); got != ok {
			t.Errorf("SelfTag(%q) = %v", tag, got)
		}
	}
}

func TestSelfUpdateKeepsNoSkills(t *testing.T) {
	s := newSelfEnv(t, "0.1.0", "0.2.0")
	os.WriteFile(filepath.Join(s.root, selfNoSkills), nil, 0o644)
	if _, err := s.in.SelfUpdate(context.Background(), s.cur, s.rel, nil); err != nil {
		t.Fatal(err)
	}
	skills := filepath.Join(s.paths.Home, ".claude", "skills")
	if got := readFile(t, filepath.Join(skills, "omastore-check", "SKILL.md")); got != "old skill" {
		t.Errorf("install.sh --no-skills was ignored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(s.paths.Home, ".agents", "skills", "omastore-check")); !os.IsNotExist(err) {
		t.Errorf("skill installed despite --no-skills: %v", err)
	}
}
