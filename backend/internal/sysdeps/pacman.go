package sysdeps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

// Report is the state of an app's dependencies.
type Report struct {
	Source string     `json:"source"`
	Deps   []DepState `json:"deps"`
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
	return out
}

// ErrNoPacman means this system has no pacman (not Arch-based).
var ErrNoPacman = errors.New("pacman not found: system dependencies can only be checked on Arch Linux")

// ErrDenied means the administrator authentication was canceled or refused.
var ErrDenied = errors.New("administrator authentication was canceled or refused")

// ErrUnavailable means dependencies are missing that no pacman repository has.
var ErrUnavailable = errors.New("some dependencies are not in any pacman repository")

// Pacman runs the pacman queries and the privileged installation. The paths
// are absolute on purpose: never resolved through PATH, which includes
// ~/.local/bin, where downloaded apps live.
type Pacman struct {
	Pacman string // default /usr/bin/pacman
	Pkexec string // default /usr/bin/pkexec
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
