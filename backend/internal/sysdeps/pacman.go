package sysdeps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Status of a dependency on this system.
const (
	StatusInstalled   = "installed"   // satisfied by an installed package
	StatusAvailable   = "available"   // missing, and a pacman repository has it
	StatusUnavailable = "unavailable" // missing, and no configured repository has it (AUR, typo...)
	StatusUnknown     = "unknown"     // this system has no pacman
)

// DepState is a dependency with its state on this system.
type DepState struct {
	Dep
	Status string `json:"status"`
	// Package is the repository package that satisfies it ("extra/ffmpeg"),
	// when Status is StatusAvailable.
	Package string `json:"package,omitempty"`
}

// LibState is a shared library the installed executable needs and the
// system does not have (see internal/elfdeps).
type LibState struct {
	Name string `json:"name"` // soname, e.g. libwebkit2gtk-4.1.so.0
	// StatusAvailable (Package provides it), StatusUnavailable (no
	// repository has it) or StatusUnknown (no pacman file database to ask:
	// pacman -Fy creates it).
	Status  string `json:"status"`
	Package string `json:"package,omitempty"`
}

// Report is the state of an app's dependencies.
type Report struct {
	Source string     `json:"source"`
	Deps   []DepState `json:"deps"`
	// Libraries the installed executable needs and are missing.
	Libraries []LibState `json:"libraries,omitempty"`
	// WrongArch names the machine the installed executable was built for when
	// it is not this one ("" otherwise).
	WrongArch string `json:"wrongArch,omitempty"`
}

// ToInstall lists the repository packages that would be installed.
func (r Report) ToInstall() []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range r.Deps {
		if d.Status == StatusAvailable && !seen[d.Package] {
			seen[d.Package] = true
			out = append(out, d.Package)
		}
	}
	for _, l := range r.Libraries {
		if l.Status == StatusAvailable && !seen[l.Package] {
			seen[l.Package] = true
			out = append(out, l.Package)
		}
	}
	return out
}

// ErrNoPacman means this system has no pacman (not Arch-based).
var ErrNoPacman = errors.New("pacman not found: system dependencies can only be checked on Arch Linux")

// ErrStaleDatabase means pacman could not download a package the local sync
// database lists: the mirrors moved on since the last system update. The fix
// is a full system update (omarchy update), never "pacman -Sy" (a partial
// upgrade, which Arch does not support).
var ErrStaleDatabase = errors.New("the package database is older than the mirrors: update the system (omarchy update) and try again")

// ErrDenied means the administrator authentication was canceled or refused.
var ErrDenied = errors.New("administrator authentication was canceled or refused")

// ErrUnavailable means dependencies are missing that no pacman repository has.
var ErrUnavailable = errors.New("some dependencies are not in any pacman repository")

// Pacman runs the pacman queries and the privileged installation. The paths
// are absolute on purpose: never resolved through PATH, which includes
// ~/.local/bin, where downloaded apps live.
type Pacman struct {
	Pacman  string // default /usr/bin/pacman
	Pkexec  string // default /usr/bin/pkexec
	SyncDir string // default /var/lib/pacman/sync (where pacman -Fy keeps the file databases)
	// run executes a command (tests replace it). It returns stdout, stderr
	// and the exit code; err is set only when the command could not run.
	run func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, code int, err error)
}

func (p *Pacman) pacman() string {
	if p.Pacman != "" {
		return p.Pacman
	}
	return "/usr/bin/pacman"
}

func (p *Pacman) pkexec() string {
	if p.Pkexec != "" {
		return p.Pkexec
	}
	return "/usr/bin/pkexec"
}

func (p *Pacman) exec(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	if p.run != nil {
		return p.run(ctx, name, args...)
	}
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	// The C locale keeps pacman's output parseable.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.Bytes(), errb.Bytes(), ee.ExitCode(), nil
	}
	return out.Bytes(), errb.Bytes(), 0, err
}

// HasFileDB reports whether pacman can answer "which package has this
// file" (pacman -F needs the .files databases that pacman -Fy downloads).
func (p *Pacman) HasFileDB() bool {
	dir := p.SyncDir
	if dir == "" {
		dir = "/var/lib/pacman/sync"
	}
	m, _ := filepath.Glob(filepath.Join(dir, "*.files"))
	return len(m) > 0
}

// LibraryPackage finds the repository package ("repo/name") that ships the
// shared library lib in usr/lib, through pacman's file database. known is
// false when there is no such database to ask.
func (p *Pacman) LibraryPackage(ctx context.Context, lib string) (pkg string, known bool, err error) {
	if !p.Available() || !p.HasFileDB() {
		return "", false, nil
	}
	if !validLib(lib) {
		return "", true, fmt.Errorf("invalid library name %q", lib)
	}
	out, stderr, code, err := p.exec(ctx, p.pacman(), "-F", "--machinereadable", "--", lib)
	if err != nil {
		return "", true, fmt.Errorf("run pacman -F: %w", err)
	}
	if code != 0 && len(bytes.TrimSpace(out)) == 0 {
		if code == 1 { // not found
			return "", true, nil
		}
		return "", true, fmt.Errorf("pacman -F %s: exit %d: %s", lib, code, lastLine(stderr))
	}
	// repo\0name\0version\0path per line. Prefer the library in usr/lib: the
	// same soname also exists in lib32 packages (multilib).
	fallback := ""
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) != 4 || !ValidName(f[0]) || !ValidName(f[1]) {
			continue
		}
		if f[3] == "usr/lib/"+lib {
			return f[0] + "/" + f[1], true, nil
		}
		if fallback == "" && !strings.Contains(f[3], "lib32") {
			fallback = f[0] + "/" + f[1]
		}
	}
	return fallback, true, nil
}

// validLib accepts sonames only (no paths, no options).
func validLib(lib string) bool {
	if lib == "" || len(lib) > 200 || strings.HasPrefix(lib, "-") {
		return false
	}
	for _, r := range lib {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+-", r)) {
			return false
		}
	}
	return true
}

// Available reports whether pacman exists on this system.
func (p *Pacman) Available() bool {
	st, err := os.Stat(p.pacman())
	return err == nil && !st.IsDir()
}

// Check finds which dependencies are missing and which repository package
// satisfies each of them. Nothing here needs root. Without pacman, it returns
// ErrNoPacman with every dependency as StatusUnknown.
func (p *Pacman) Check(ctx context.Context, set Set) (Report, error) {
	rep := Report{Source: set.Source}
	if len(set.Deps) == 0 {
		return rep, nil
	}
	if !p.Available() {
		for _, d := range set.Deps {
			rep.Deps = append(rep.Deps, DepState{Dep: d, Status: StatusUnknown})
		}
		return rep, ErrNoPacman
	}
	specs := make([]string, 0, len(set.Deps))
	for _, d := range set.Deps {
		if !validSpec(d.Spec) {
			return rep, fmt.Errorf("invalid dependency %q", d.Spec)
		}
		specs = append(specs, d.Spec)
	}
	// pacman -T prints the dependencies no installed package satisfies
	// (it understands versions and provides); exit code 127 when any is missing.
	out, stderr, code, err := p.exec(ctx, p.pacman(), append([]string{"-T", "--"}, specs...)...)
	if err != nil {
		return rep, fmt.Errorf("run pacman -T: %w", err)
	}
	if code != 0 && code != 127 {
		return rep, fmt.Errorf("pacman -T: exit %d: %s", code, lastLine(stderr))
	}
	missing := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			missing[l] = true
		}
	}
	for _, d := range set.Deps {
		ds := DepState{Dep: d, Status: StatusInstalled}
		if missing[d.Spec] {
			ds.Status = StatusUnavailable
			pkg, err := p.resolve(ctx, d.Name())
			if err != nil {
				return rep, err
			}
			if pkg != "" {
				ds.Status, ds.Package = StatusAvailable, pkg
			}
		}
		rep.Deps = append(rep.Deps, ds)
	}
	return rep, nil
}

// resolve returns the repository package ("repo/name") pacman would install
// for name, following provides, or "" if no configured repository has it.
func (p *Pacman) resolve(ctx context.Context, name string) (string, error) {
	// -dd resolves only the target itself; --noconfirm picks the default
	// provider instead of asking.
	out, stderr, code, err := p.exec(ctx, p.pacman(), "-Sddp", "--noconfirm", "--print-format", "%r/%n", "--", name)
	if err != nil {
		return "", fmt.Errorf("run pacman -Sp: %w", err)
	}
	if code != 0 {
		if strings.Contains(string(stderr), "target not found") {
			return "", nil
		}
		return "", fmt.Errorf("pacman -Sp %s: exit %d: %s", name, code, lastLine(stderr))
	}
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		repo, pkg, ok := strings.Cut(l, "/")
		if ok && ValidName(pkg) && ValidName(repo) {
			return l, nil
		}
	}
	return "", nil
}

// Install installs the repository packages in pkgs as root through pkexec,
// which asks for the administrator password (polkit). They are installed as
// explicit packages: nothing in pacman's database depends on them, so as
// dependencies they would be removed as orphans.
func (p *Pacman) Install(ctx context.Context, pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	if !p.Available() {
		return ErrNoPacman
	}
	for _, pkg := range pkgs {
		repo, name, ok := strings.Cut(pkg, "/")
		if !ok || !ValidName(repo) || !ValidName(name) {
			return fmt.Errorf("invalid package %q", pkg)
		}
	}
	args := append([]string{p.pacman(), "-S", "--needed", "--noconfirm", "--"}, pkgs...)
	_, stderr, code, err := p.exec(ctx, p.pkexec(), args...)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("pkexec not found (install polkit): %w", err)
		}
		return fmt.Errorf("run pkexec: %w", err)
	}
	switch code {
	case 0:
		return nil
	case 126, 127: // pkexec: dialog dismissed / not authorized
		if code == 127 && !bytes.Contains(stderr, []byte("uthoriz")) && !bytes.Contains(stderr, []byte("uthentic")) {
			break
		}
		return ErrDenied
	}
	if bytes.Contains(stderr, []byte("failed retrieving file")) || bytes.Contains(stderr, []byte("failed to retrieve some files")) {
		return fmt.Errorf("%w (%s)", ErrStaleDatabase, lastLine(stderr))
	}
	return fmt.Errorf("pacman -S: exit %d: %s", code, lastLine(stderr))
}

// lastLine is the last non-empty line of a command's stderr.
func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return "no error output"
}
