package sysdeps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func specs(s Set) []string {
	var out []string
	for _, d := range s.Deps {
		p := d.Spec
		if d.Optional {
			p = "?" + p
		}
		out = append(out, p)
	}
	return out
}

func TestParsePKGBUILD(t *testing.T) {
	pkgbuild := `# Maintainer: someone
pkgname=myapp-bin
pkgver=1.2.0
_dep=qt6-base
depends=('gtk4' "libadwaita>=1.4" # the UI
         glib2 "$_dep" ` + "`echo evil`" + ` 'bad name' '-rf')
depends_x86_64=(lib32-foo)
depends_aarch64=(arm-only)
optdepends=('ffmpeg: video export'
            "wl-clipboard: copy to the clipboard"
            'gtk4: duplicate of a depends')
makedepends=(go git)
depends+=(hicolor-icon-theme)

build() {
  depends=(inside-build)
}

package() {
  install -Dm755 myapp "$pkgdir/usr/bin/myapp"
}
`
	got := Parse("PKGBUILD", pkgbuild, "myapp", "amd64")
	want := []string{"gtk4", "libadwaita>=1.4", "glib2", "hicolor-icon-theme", "lib32-foo",
		"?ffmpeg", "?wl-clipboard"}
	if !reflect.DeepEqual(specs(got), want) {
		t.Fatalf("deps = %v, want %v", specs(got), want)
	}
	if got.Deps[5].Reason != "video export" {
		t.Errorf("reason = %q", got.Deps[5].Reason)
	}
	if got.Source != "PKGBUILD" {
		t.Errorf("source = %q", got.Source)
	}
	arm := Parse("PKGBUILD", pkgbuild, "myapp", "arm64")
	if !strings.Contains(strings.Join(specs(arm), " "), "arm-only") || strings.Contains(strings.Join(specs(arm), " "), "lib32-foo") {
		t.Errorf("arm64 deps = %v", specs(arm))
	}
}

func TestParseSplitPKGBUILD(t *testing.T) {
	pkgbuild := `pkgbase=suite
pkgname=(suite-cli suite)
depends=(glibc)
package_suite-cli() {
  depends=(cli-only)
}
package_suite() {
  depends=(glibc gui-lib)
  optdepends=('extra-thing: more')
}
`
	if got := specs(Parse("PKGBUILD", pkgbuild, "suite", "amd64")); !reflect.DeepEqual(got, []string{"glibc", "gui-lib", "?extra-thing"}) {
		t.Errorf("suite = %v", got)
	}
	// No exact match: the first package whose name starts with the repository's.
	if got := specs(Parse("PKGBUILD", pkgbuild, "other", "amd64")); !reflect.DeepEqual(got, []string{"cli-only"}) {
		t.Errorf("fallback = %v", got)
	}
}

func TestParseSRCINFO(t *testing.T) {
	srcinfo := `pkgbase = myapp
	pkgver = 1.0
	arch = x86_64
	depends = gtk4
	depends = libadwaita>=1.4
	depends_x86_64 = lib32-foo
	optdepends = ffmpeg: video export
	makedepends = go

pkgname = myapp-docs
	depends =

pkgname = myapp
	optdepends = ffmpeg: video export
	optdepends = wl-clipboard: clipboard
`
	got := Parse("packaging/.SRCINFO", srcinfo, "myapp", "amd64")
	want := []string{"gtk4", "libadwaita>=1.4", "lib32-foo", "?ffmpeg", "?wl-clipboard"}
	if !reflect.DeepEqual(specs(got), want) {
		t.Fatalf("deps = %v, want %v", specs(got), want)
	}
	// An empty value clears the base's list for that package.
	if got := specs(Parse(".SRCINFO", srcinfo, "myapp-docs", "amd64")); !reflect.DeepEqual(got, []string{"lib32-foo", "?ffmpeg"}) {
		t.Errorf("docs = %v", got)
	}
}

func TestCandidates(t *testing.T) {
	files := []string{
		"README.md",
		"packaging/arch/PKGBUILD",
		"packaging/arch-bin/PKGBUILD",
		"packaging/arch-bin/.SRCINFO",
		"PKGBUILD",
		"tests/fixtures/PKGBUILD",
		"a/b/c/d/PKGBUILD",
	}
	got := Candidates(files)
	want := []string{"PKGBUILD", "packaging/arch-bin/.SRCINFO", "packaging/arch-bin/PKGBUILD", "packaging/arch/PKGBUILD"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

// fakeRun answers pacman like a system with glibc installed and ffmpeg in extra.
type fakeRun struct {
	calls    [][]string
	pkexec   int // exit code of pkexec
	pkStderr string
}

func (f *fakeRun) run(_ context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	switch {
	case filepath.Base(name) == "pkexec":
		return nil, []byte(f.pkStderr), f.pkexec, nil
	case args[0] == "-T":
		var missing []string
		for _, a := range args[2:] {
			if a != "glibc" {
				missing = append(missing, a)
			}
		}
		code := 0
		if len(missing) > 0 {
			code = 127
		}
		return []byte(strings.Join(missing, "\n") + "\n"), nil, code, nil
	case args[0] == "-Sddp":
		switch target := args[len(args)-1]; target {
		case "ffmpeg", "java-runtime":
			return []byte("extra/" + map[string]string{"ffmpeg": "ffmpeg", "java-runtime": "jre-openjdk"}[target] + "\n"), nil, 0, nil
		default:
			return nil, []byte("error: target not found: " + target + "\n"), 1, nil
		}
	}
	return nil, []byte("unexpected"), 1, nil
}

func newPacman(t *testing.T, f *fakeRun) *Pacman {
	bin := filepath.Join(t.TempDir(), "pacman")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	return &Pacman{Pacman: bin, Pkexec: "/usr/bin/pkexec", run: f.run}
}

func TestCheckAndInstall(t *testing.T) {
	f := &fakeRun{}
	p := newPacman(t, f)
	set := Set{Source: "PKGBUILD", Deps: []Dep{
		{Spec: "glibc"}, {Spec: "java-runtime>=17"}, {Spec: "ffmpeg", Optional: true}, {Spec: "aur-only"},
	}}
	rep, err := p.Check(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range rep.Deps {
		got = append(got, d.Spec+"="+d.Status+":"+d.Package)
	}
	want := []string{"glibc=installed:", "java-runtime>=17=available:extra/jre-openjdk",
		"ffmpeg=available:extra/ffmpeg", "aur-only=unavailable:"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("report = %v, want %v", got, want)
	}
	// The version constraint is checked by pacman -T but not passed to -S.
	if last := f.calls[1]; last[len(last)-1] != "java-runtime" {
		t.Errorf("resolve call = %v", last)
	}

	if err := p.Install(context.Background(), rep.ToInstall()); err != nil {
		t.Fatal(err)
	}
	call := f.calls[len(f.calls)-1]
	wantCall := []string{"/usr/bin/pkexec", p.Pacman, "-S", "--needed", "--noconfirm", "--", "extra/jre-openjdk", "extra/ffmpeg"}
	if !reflect.DeepEqual(call, wantCall) {
		t.Errorf("install call = %v, want %v", call, wantCall)
	}

	f.pkexec, f.pkStderr = 126, "Error executing command as another user: Request dismissed"
	if err := p.Install(context.Background(), []string{"extra/ffmpeg"}); !errors.Is(err, ErrDenied) {
		t.Errorf("dismissed: err = %v", err)
	}
	f.pkexec, f.pkStderr = 1, "error: failed to init transaction (unable to lock database)"
	if err := p.Install(context.Background(), []string{"extra/ffmpeg"}); err == nil || !strings.Contains(err.Error(), "unable to lock") {
		t.Errorf("pacman failure: err = %v", err)
	}
}

func TestInstallRejectsBadNames(t *testing.T) {
	f := &fakeRun{}
	p := newPacman(t, f)
	for _, pkg := range []string{"ffmpeg", "extra/-rf", "extra/a b", "../x/y", "extra/$(id)"} {
		if err := p.Install(context.Background(), []string{pkg}); err == nil {
			t.Errorf("%q accepted", pkg)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("commands ran: %v", f.calls)
	}
}

func TestCheckWithoutPacman(t *testing.T) {
	p := &Pacman{Pacman: filepath.Join(t.TempDir(), "missing")}
	rep, err := p.Check(context.Background(), Set{Deps: []Dep{{Spec: "gtk4"}}})
	if !errors.Is(err, ErrNoPacman) {
		t.Fatalf("err = %v", err)
	}
	if len(rep.Deps) != 1 || rep.Deps[0].Status != StatusUnknown {
		t.Errorf("report = %+v", rep)
	}
}
