package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/KitsuneForgering/OmaStore/backend/internal/manifest"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

// serviceChange captures systemd's previous state so a failed file/database
// transaction can restore it. The file transaction itself remains install.tx.
type serviceChange struct {
	in     *Installer
	units  []string
	before map[string]unitState
}

type unitState struct{ load, fragment, active, enabled string }

func (in *Installer) userSystemctl(ctx context.Context, args ...string) (string, error) {
	if in.systemctl != nil {
		return in.systemctl(ctx, args...)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/systemctl", append([]string{"--user", "--no-pager"}, args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("systemctl --user %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (in *Installer) unitStatus(ctx context.Context, unit string) (unitState, error) {
	out, err := in.userSystemctl(ctx, "show", "--property=LoadState,FragmentPath,ActiveState,UnitFileState", unit)
	if err != nil {
		return unitState{}, err
	}
	var s unitState
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "LoadState":
			s.load = v
		case "FragmentPath":
			s.fragment = v
		case "ActiveState":
			s.active = v
		case "UnitFileState":
			s.enabled = v
		}
	}
	if s.load == "" {
		return s, errors.New("systemctl returned no LoadState")
	}
	return s, nil
}

func (in *Installer) serviceCommand(ctx context.Context, op, unit string) error {
	_, err := in.userSystemctl(ctx, op, unit)
	if err != nil {
		return fmt.Errorf("%s %s: %w", op, unit, err)
	}
	return nil
}

func (in *Installer) daemonReload(ctx context.Context) error {
	_, err := in.userSystemctl(ctx, "daemon-reload")
	if err != nil {
		return fmt.Errorf("reload systemd user manager: %w", err)
	}
	return nil
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// ownedUnit requires both the recorded path and its exact contents. A user's
// edits or a different unit at the same path must not be overwritten/deleted.
func ownedUnit(v store.Integration, path string) bool {
	if v.Type != "systemd-user" || v.Path != path || v.Digest == "" {
		return false
	}
	b, err := os.ReadFile(path)
	return err == nil && digest(b) == v.Digest
}

func quoteSystemdPath(p string) (string, error) {
	if strings.ContainsFunc(p, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) {
		return "", ErrUnsafePath
	}
	p = strings.ReplaceAll(p, "%", "%%")
	p = strings.ReplaceAll(p, "$", "$$")
	p = strings.ReplaceAll(p, `\`, `\\`)
	p = strings.ReplaceAll(p, `"`, `\"`)
	return `"` + p + `"`, nil
}

func serviceFile(repo string, s manifest.Service, execPath string) ([]byte, error) {
	q, err := quoteSystemdPath(execPath)
	if err != nil {
		return nil, err
	}
	restart := s.Restart
	if restart == "" {
		restart = "no"
	}
	return []byte("# Managed by OmaStore: " + repo + "\n[Unit]\nDescription=" + s.Unit + " for " + repo +
		"\n[Service]\nExecStart=" + q + "\nRestart=" + restart + "\n[Install]\nWantedBy=default.target\n"), nil
}

func serviceExec(versionDir string, rel string) (string, error) {
	p := filepath.Join(versionDir, filepath.FromSlash(rel))
	resolved, err := realPath(p)
	if err != nil || !within(versionDir, resolved) {
		return "", fmt.Errorf("service executable %q leaves package", rel)
	}
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("service executable %q missing or not executable", rel)
	}
	return p, nil
}

func (in *Installer) applyServices(ctx context.Context, t *tx, m *manifest.Manifest, prev *store.Install,
	inst *store.Install, versionDir string, report func(Progress)) (*serviceChange, error) {
	var desired map[string]manifest.Service
	if m != nil {
		desired = m.Services
	}
	if len(desired) == 0 && (prev == nil || len(prev.Integrations) == 0) {
		return nil, nil
	}
	change := &serviceChange{in: in, before: map[string]unitState{}}
	old := map[string]store.Integration{}
	if prev != nil {
		for _, v := range prev.Integrations {
			old[v.Unit] = v
		}
	}
	keys := make([]string, 0, len(desired))
	for k := range desired {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s := desired[k]
		path := filepath.Join(in.Paths.UserUnits, s.Unit)
		if !within(in.Paths.Home, path) {
			return change, ErrOutsideHome
		}
		if resolved, err := realPath(filepath.Dir(path)); err != nil || !within(in.Paths.Home, resolved) {
			return change, fmt.Errorf("user unit directory escapes home")
		}
		status, err := in.unitStatus(ctx, s.Unit)
		if err != nil {
			return change, err
		}
		if status.load != "not-found" && status.fragment != path {
			return change, fmt.Errorf("%w: service %s already exists at %s", ErrConflict, s.Unit, status.fragment)
		}
		prior, had := old[s.Unit]
		if !had && (status.enabled == "enabled" || status.active == "active") {
			return change, fmt.Errorf("%w: service %s has existing user state", ErrConflict, s.Unit)
		}
		if had && !ownedUnit(prior, path) {
			return change, fmt.Errorf("%w: service %s changed outside OmaStore", ErrConflict, s.Unit)
		}
		if !had {
			if _, err := os.Lstat(path); err == nil {
				return change, fmt.Errorf("%w: service %s", ErrConflict, s.Unit)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return change, err
			}
		}
		change.before[s.Unit] = status
		change.units = append(change.units, s.Unit)
		executable, err := serviceExec(versionDir, s.Exec)
		if err != nil {
			return change, err
		}
		content, err := serviceFile(inst.FullName, s, executable)
		if err != nil {
			return change, err
		}
		if err := os.MkdirAll(in.Paths.UserUnits, 0o755); err != nil {
			return change, err
		}
		if err := t.prepare(path); err != nil {
			return change, err
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return change, err
		}
		inst.Files = append(inst.Files, path)
		inst.Integrations = append(inst.Integrations, store.Integration{Type: s.Type, Unit: s.Unit, Path: path,
			EnabledByStore: prior.EnabledByStore, StartedByStore: prior.StartedByStore, Version: inst.Version,
			Digest: digest(content), Exec: s.Exec, Restart: s.Restart, Enable: s.Enable, Start: s.Start})
		report(Progress{Stage: StageService, Current: "Configuring " + s.Unit})
	}
	// Units no longer declared are removed only when their exact contents are ours.
	for unit, v := range old {
		found := false
		for _, s := range desired {
			if s.Unit == unit {
				found = true
				break
			}
		}
		if found {
			continue
		}
		if !ownedUnit(v, filepath.Join(in.Paths.UserUnits, unit)) {
			return change, fmt.Errorf("%w: service %s changed outside OmaStore", ErrConflict, unit)
		}
		status, err := in.unitStatus(ctx, unit)
		if err != nil {
			return change, err
		}
		if status.fragment != v.Path {
			return change, fmt.Errorf("%w: service %s changed source", ErrConflict, unit)
		}
		if (status.active == "active" && !v.StartedByStore) || (status.enabled == "enabled" && !v.EnabledByStore) {
			return change, fmt.Errorf("%w: service %s has user-managed state", ErrConflict, unit)
		}
		change.before[unit] = status
		change.units = append(change.units, unit)
		if v.StartedByStore && status.active == "active" {
			if err := in.serviceCommand(ctx, "stop", unit); err != nil {
				return change, err
			}
		}
		if v.EnabledByStore && status.enabled == "enabled" {
			if err := in.serviceCommand(ctx, "disable", unit); err != nil {
				return change, err
			}
		}
		if err := t.prepare(v.Path); err != nil {
			return change, err
		}
		report(Progress{Stage: StageService, Current: "Removing " + unit})
	}
	if err := in.daemonReload(ctx); err != nil {
		return change, err
	}
	for _, k := range keys {
		s := desired[k]
		status := change.before[s.Unit]
		prior, had := old[s.Unit]
		v := &inst.Integrations[0]
		for i := range inst.Integrations {
			if inst.Integrations[i].Unit == s.Unit {
				v = &inst.Integrations[i]
				break
			}
		}
		if s.Enable && status.enabled != "enabled" {
			report(Progress{Stage: StageService, Current: "Enabling " + s.Unit})
			if err := in.serviceCommand(ctx, "enable", s.Unit); err != nil {
				return change, err
			}
			v.EnabledByStore = true
		} else if !s.Enable && v.EnabledByStore && status.enabled == "enabled" {
			if err := in.serviceCommand(ctx, "disable", s.Unit); err != nil {
				return change, err
			}
			v.EnabledByStore = false
		}
		if status.active == "active" && !s.Start && v.StartedByStore {
			if err := in.serviceCommand(ctx, "stop", s.Unit); err != nil {
				return change, err
			}
			v.StartedByStore = false
		} else if status.active == "active" {
			report(Progress{Stage: StageService, Current: "Restarting " + s.Unit})
			if err := in.serviceCommand(ctx, "restart", s.Unit); err != nil {
				return change, err
			}
		} else if s.Start && (!had || !prior.StartedByStore) {
			report(Progress{Stage: StageService, Current: "Starting " + s.Unit})
			if err := in.serviceCommand(ctx, "start", s.Unit); err != nil {
				return change, err
			}
			v.StartedByStore = true
		}
	}
	return change, nil
}

func (c *serviceChange) beforeRollback(_ context.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var errs []error
	for _, unit := range c.units {
		if err := c.in.serviceCommand(ctx, "stop", unit); err != nil {
			errs = append(errs, err)
		}
		if err := c.in.serviceCommand(ctx, "disable", unit); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *serviceChange) afterRollback(_ context.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var errs []error
	if err := c.in.daemonReload(ctx); err != nil {
		errs = append(errs, err)
	}
	for _, unit := range c.units {
		s := c.before[unit]
		if s.enabled == "enabled" {
			if err := c.in.serviceCommand(ctx, "enable", unit); err != nil {
				errs = append(errs, err)
			}
		}
		if s.active == "active" {
			if err := c.in.serviceCommand(ctx, "start", unit); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (in *Installer) rollbackIntegration(ctx context.Context, c *serviceChange, t *tx) error {
	var errs []error
	if c != nil {
		errs = append(errs, c.beforeRollback(ctx))
	}
	if t != nil {
		errs = append(errs, t.rollback())
	}
	if c != nil {
		errs = append(errs, c.afterRollback(ctx))
	}
	return errors.Join(errs...)
}

// removeServices removes only units whose exact files and active/enabled state
// OmaStore owns. It returns a file transaction for the caller to commit after
// recording the reduced installation, or roll back on failure.
func (in *Installer) removeServices(ctx context.Context, inst *store.Install) (*tx, *serviceChange, error) {
	if len(inst.Integrations) == 0 {
		return nil, nil, nil
	}
	t := &tx{}
	c := &serviceChange{in: in, before: map[string]unitState{}}
	for _, v := range inst.Integrations {
		if v.Type != "systemd-user" || v.Path != filepath.Join(in.Paths.UserUnits, v.Unit) || !ownedUnit(v, v.Path) {
			return t, c, fmt.Errorf("%w: service %s changed outside OmaStore", ErrConflict, v.Unit)
		}
		s, err := in.unitStatus(ctx, v.Unit)
		if err != nil {
			return t, c, err
		}
		if s.load == "not-found" {
			if err := in.daemonReload(ctx); err != nil {
				return t, c, err
			}
			s, err = in.unitStatus(ctx, v.Unit)
			if err != nil {
				return t, c, err
			}
		}
		if s.fragment != v.Path {
			return t, c, fmt.Errorf("%w: service %s changed source", ErrConflict, v.Unit)
		}
		if (s.active == "active" && !v.StartedByStore) || (s.enabled == "enabled" && !v.EnabledByStore) {
			return t, c, fmt.Errorf("%w: service %s has user-managed state", ErrConflict, v.Unit)
		}
		c.before[v.Unit] = s
		c.units = append(c.units, v.Unit)
	}
	for _, v := range inst.Integrations {
		s := c.before[v.Unit]
		if s.active == "active" {
			if err := in.serviceCommand(ctx, "stop", v.Unit); err != nil {
				return t, c, err
			}
		}
		if s.enabled == "enabled" {
			if err := in.serviceCommand(ctx, "disable", v.Unit); err != nil {
				return t, c, err
			}
		}
		if err := t.prepare(v.Path); err != nil {
			return t, c, err
		}
	}
	if err := in.daemonReload(ctx); err != nil {
		return t, c, err
	}
	unitPaths := map[string]bool{}
	for _, v := range inst.Integrations {
		unitPaths[v.Path] = true
	}
	files := inst.Files[:0]
	for _, f := range inst.Files {
		if !unitPaths[f] {
			files = append(files, f)
		}
	}
	inst.Files = files
	inst.Integrations = nil
	return t, c, nil
}
