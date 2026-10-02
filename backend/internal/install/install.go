// Package install downloads, verifies and installs the latest release binary
// of an app into the user's $HOME, generating the .desktop file and the icon.
// Nothing is run during installation, nothing is written outside $HOME and there is no sudo.
package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/KitsuneForgering/OmaStore/backend/internal/asset"
	"github.com/KitsuneForgering/OmaStore/backend/internal/flock"
	"github.com/KitsuneForgering/OmaStore/backend/internal/gitrepo"
	"github.com/KitsuneForgering/OmaStore/backend/internal/manifest"
	"github.com/KitsuneForgering/OmaStore/backend/internal/repoid"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
	"github.com/KitsuneForgering/OmaStore/backend/internal/xdg"
)

// Errors returned by the installer.
var (
	ErrNotInstallable = errors.New("app has no binary for this architecture")
	ErrNotInstalled   = errors.New("app is not installed")
	ErrUpToDate       = errors.New("app is already at the latest version")
	ErrConflict       = errors.New("file already exists and does not belong to OmaStore")
	ErrOutsideHome    = errors.New("path outside $HOME")
	ErrBusy           = errors.New("another OmaStore process is changing this app")
	// ErrIncomplete means some registered files could not be removed; they
	// stay recorded so a later attempt can remove them.
	ErrIncomplete = errors.New("some files could not be removed")
	// ErrUnverified means the selected file has neither a GitHub digest nor a
	// published checksum, and the caller did not allow installing it anyway.
	ErrUnverified = errors.New("the release publishes no checksum for this file")
	// ErrNoPrevious means there is no earlier version on disk to go back to.
	ErrNoPrevious = errors.New("no previous version to go back to")
)

// Installer installs and removes apps.
type Installer struct {
	Store *store.Store
	Paths xdg.Paths
	HTTP  *http.Client // default: client with a connection timeout
	// GOARCH decides the asset (default runtime.GOARCH).
	GOARCH string
	// Hooks runs the system's update-desktop-database and gtk-update-icon-cache.
	Hooks bool
	// TUILauncher opens terminal apps through Omarchy (see Desktop); New sets
	// it when Omarchy's launcher exists.
	TUILauncher string
	Log         *slog.Logger

	locks sync.Map // full_name → *sync.Mutex

	defaultHTTP     *http.Client
	defaultHTTPOnce sync.Once
}

// Options of an install or update.
type Options struct {
	Progress func(Progress)
	// AllowUnverified installs a file that has no digest and no published
	// checksum. The one rule for every entry point (GUI, CLI, updates): the
	// user must have said yes to that file, otherwise ErrUnverified.
	AllowUnverified bool
}

// Verifiable reports whether a's download can be checked: a digest from the
// API or a checksum file in the release.
func Verifiable(a store.Asset) bool {
	if algo, hexsum, ok := strings.Cut(a.Digest, ":"); ok && (algo == "sha256" || algo == "sha512") && reHex.MatchString(hexsum) {
		return true
	}
	return a.ChecksumURL != ""
}

// Progress is an installation's progress.
type Progress struct {
	Stage string // download, verify, extract, integrate, done
	Done  int64
	Total int64
}

// Progress.Stage values.
const (
	StageDownload  = "download"
	StageVerify    = "verify"
	StageExtract   = "extract"
	StageIntegrate = "integrate"
	StageDone      = "done"
)

// New validates that every destination is inside $HOME.
func New(st *store.Store, p xdg.Paths) (*Installer, error) {
	for _, d := range []string{p.AppsDir, p.BinDir, p.Applications, p.Icons, p.CacheDir} {
		if !within(p.Home, d) {
			return nil, fmt.Errorf("%w: %s", ErrOutsideHome, d)
		}
	}
	in := &Installer{Store: st, Paths: p, Hooks: true}
	if fi, err := os.Stat(omarchyTUI); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
		in.TUILauncher = omarchyTUI
	}
	return in, nil
}

func (in *Installer) http() *http.Client {
	if in.HTTP != nil {
		return in.HTTP
	}
	// One client for every download, so connections are reused.
	in.defaultHTTPOnce.Do(func() {
		in.defaultHTTP = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 60 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		}}
	})
	return in.defaultHTTP
}

// httpsOnly returns a copy of c that refuses redirects to anything but
// https: checkURL only sees the first URL.
func httpsOnly(c *http.Client) *http.Client {
	cp := *c
	next := c.CheckRedirect
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to %q is not https", req.URL.Redacted())
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &cp
}

func (in *Installer) log() *slog.Logger {
	if in.Log != nil {
		return in.Log
	}
	return slog.Default()
}

func (in *Installer) goarch() string {
	if in.GOARCH != "" {
		return in.GOARCH
	}
	return runtime.GOARCH
}

// lock serializes the operations on an app: a mutex inside this process and
// a file lock against the others (the CLI while the daemon installs). Another
// process holding it makes the operation fail with ErrBusy instead of waiting
// for a download of unknown length.
func (in *Installer) lock(fullName string) (func(), error) {
	owner, repo, err := splitName(fullName)
	if err != nil {
		return nil, err
	}
	key := strings.ToLower(owner + "__" + repo)
	m, _ := in.locks.LoadOrStore(key, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	unlockFile, err := flock.TryLock(filepath.Join(filepath.Dir(in.Paths.AppsDir), "locks", key+".lock"))
	if err != nil {
		mu.Unlock()
		if errors.Is(err, flock.ErrLocked) {
			return nil, fmt.Errorf("%s: %w", fullName, ErrBusy)
		}
		return nil, err
	}
	return func() { unlockFile(); mu.Unlock() }, nil
}

// within reports whether p is inside root (or is root).
func within(root, p string) bool {
	if root == "" || !filepath.IsAbs(p) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// splitName validates the name with repoid, so owner and repo cannot turn
// into dangerous path components.
func splitName(fullName string) (owner, repo string, err error) {
	return repoid.Split(fullName)
}

var reVersionUnsafe = regexp.MustCompile(`[^A-Za-z0-9._+-]`)

func sanitizeVersion(tag string) string {
	v := reVersionUnsafe.ReplaceAllString(tag, "_")
	v = strings.Trim(v, ".")
	if v == "" {
		return "latest"
	}
	return v
}

// appID is the identifier used in file names (.desktop, icon).
func appID(owner, repo string) string {
	return strings.ToLower("omastore-" + owner + "-" + repo)
}

// SelectAsset picks the asset for goarch: the one declared in the manifest (if
// any and present in the release), otherwise exact architecture before
// generic and then the preferred format.
func SelectAsset(assets []store.Asset, goarch string, m *manifest.Manifest) (store.Asset, bool) {
	if t, ok := m.Target(goarch); ok && t.Asset != "" {
		for _, a := range assets {
			if a.Arch == goarch && manifest.MatchAsset(t.Asset, a.Tag, a.Name) && a.Format != "" {
				return a, true
			}
		}
	}
	var cands []store.Asset
	for _, a := range assets {
		if (asset.Info{Format: a.Format, Arch: a.Arch}).Installable(goarch) {
			cands = append(cands, a)
		}
	}
	if len(cands) == 0 {
		return store.Asset{}, false
	}
	sort.SliceStable(cands, func(i, j int) bool {
		ei, ej := cands[i].Arch == goarch, cands[j].Arch == goarch
		if ei != ej {
			return ei
		}
		ri, rj := asset.FormatRank(cands[i].Format), asset.FormatRank(cands[j].Format)
		if ri != rj {
			return ri < rj
		}
		return cands[i].Name < cands[j].Name
	})
	return cands[0], true
}

// Install installs (or reinstalls/updates) the latest release of fullName.
func (in *Installer) Install(ctx context.Context, fullName string, opts Options) (*store.Install, error) {
	progress := opts.Progress
	unlock, err := in.lock(fullName)
	if err != nil {
		return nil, err
	}
	defer unlock()
	report := func(p Progress) {
		if progress != nil {
			progress(p)
		}
	}

	d, err := in.Store.GetApp(ctx, fullName)
	if err != nil {
		return nil, fmt.Errorf("app %s: %w", fullName, err)
	}
	fullName = d.FullName
	owner, repo, err := splitName(fullName)
	if err != nil {
		return nil, err
	}
	m := manifest.Decode(d.Manifest)
	sel, ok := SelectAsset(d.Assets, in.goarch(), m)
	if !d.Installable || !ok {
		return nil, fmt.Errorf("%s: %w", fullName, ErrNotInstallable)
	}
	if !opts.AllowUnverified && !Verifiable(sel) {
		return nil, fmt.Errorf("%s: %w: %s", fullName, ErrUnverified, sel.Name)
	}
	prev, err := in.Store.GetInstall(ctx, fullName)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	// 1. Download into a temporary directory.
	if err := os.MkdirAll(in.Paths.CacheDir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(in.Paths.CacheDir, "install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	archive := filepath.Join(tmp, "asset")
	report(Progress{Stage: StageDownload, Total: sel.Size})
	sum256, sum512, err := in.download(ctx, sel.URL, archive, func(done, total int64) {
		report(Progress{Stage: StageDownload, Done: done, Total: total})
	})
	if err != nil {
		return nil, err
	}

	// 2. Checksum verification, when the release publishes one.
	report(Progress{Stage: StageVerify})
	want, err := in.expected(ctx, sel.Digest, sel.ChecksumURL, sel.Name)
	if err != nil {
		return nil, err
	}
	if want == "" {
		in.log().Warn("release without checksum; installing without verification", "repo", fullName, "asset", sel.Name)
	} else if err := verify(want, sum256, sum512); err != nil {
		return nil, fmt.Errorf("%s: %w", sel.Name, err)
	}

	// 3. Extraction into a staging area inside the app directory.
	report(Progress{Stage: StageExtract})
	appDir := filepath.Join(in.Paths.AppsDir, owner+"__"+repo)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return nil, err
	}
	staging := filepath.Join(appDir, ".staging-"+randSuffix())
	defer os.RemoveAll(staging)
	binName := strings.ToLower(repo)
	if sel.Format == asset.FormatAppImage {
		binName += ".AppImage"
	}
	if err := Extract(archive, sel.Format, staging, binName); err != nil {
		return nil, fmt.Errorf("extract %s: %w", sel.Name, err)
	}
	execAbs, err := in.findExec(staging, repo, sel.Format, sel.Tag, m)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", sel.Name, err)
	}
	execRel, _ := filepath.Rel(staging, execAbs)
	// The path goes into the .desktop Exec key and the launcher, which are
	// line based: a control character would break both.
	if strings.ContainsFunc(execRel, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) {
		return nil, fmt.Errorf("%w: executable path %q has control characters", ErrUnsafePath, execRel)
	}
	if err := os.Chmod(execAbs, 0o755); err != nil {
		return nil, err
	}

	// Reinstalling the version that is running would swap its directory
	// under it (another version is installed beside it instead).
	if prev != nil {
		if ps, err := procsUsing(filepath.Join(appDir, sanitizeVersion(d.Repo.LatestTag))); err == nil && len(ps) > 0 {
			return nil, fmt.Errorf("%s: %w: %s", fullName, ErrInUse, describe(ps))
		}
	}

	// 4. Integration: version directory, launcher, icon and .desktop. Everything
	// goes through tx so it can be undone.
	report(Progress{Stage: StageIntegrate})
	t := &tx{}
	inst, err := in.integrate(ctx, t, d, m, prev, owner, repo, staging, execRel)
	if err != nil {
		if rbErr := t.rollback(); rbErr != nil {
			in.log().Error("incomplete rollback", "repo", fullName, "err", rbErr)
		}
		return nil, err
	}
	if err := t.commit(); err != nil {
		in.log().Warn("could not remove backups", "repo", fullName, "err", err)
	}

	// Remove what the previous installation created and the new one no longer
	// uses (the version before the previous one: the previous one is kept, see
	// integrate). A version directory a process still runs from is left alone.
	// What is not removed stays recorded in the new installation, so
	// uninstalling or the next update tries again.
	if prev != nil {
		keep := map[string]bool{}
		for _, f := range inst.Files {
			keep[f] = true
		}
		var pending []string
		for _, f := range prev.Files {
			if keep[f] {
				continue
			}
			if within(in.Paths.AppsDir, f) {
				if ps, err := procsUsing(f); err == nil && len(ps) > 0 {
					in.log().Info("old version still running; kept for now", "path", f, "processes", describe(ps))
					pending = append(pending, f)
					continue
				}
			}
			if in.removeRegistered(f) != nil {
				pending = append(pending, f)
			}
		}
		if len(pending) > 0 {
			inst.Files = append(inst.Files, pending...)
			if err := in.Store.SaveInstall(ctx, *inst); err != nil {
				in.log().Warn("could not record leftover files", "repo", fullName, "files", pending, "err", err)
			}
		}
	}
	in.runHooks(ctx)
	report(Progress{Stage: StageDone})
	return inst, nil
}

func (in *Installer) integrate(ctx context.Context, t *tx, d *store.AppDetail, m *manifest.Manifest,
	prev *store.Install, owner, repo, staging, execRel string) (*store.Install, error) {
	version := sanitizeVersion(d.Repo.LatestTag)
	appDir := filepath.Join(in.Paths.AppsDir, owner+"__"+repo)
	versionDir := filepath.Join(appDir, version)
	if err := t.prepare(versionDir); err != nil {
		return nil, err
	}
	if err := os.Rename(staging, versionDir); err != nil {
		return nil, err
	}
	execPath := filepath.Join(versionDir, execRel)
	files := []string{versionDir}

	// Launcher in ~/.local/bin named after the executable (see launcherScript).
	cmd := filepath.Base(execPath)
	if strings.EqualFold(filepath.Ext(cmd), ".appimage") {
		cmd = strings.TrimSuffix(cmd, filepath.Ext(cmd))
	}
	cmd = strings.ToLower(cmd)
	if err := in.checkCommandName(cmd); err != nil {
		return nil, err
	}
	link := filepath.Join(in.Paths.BinDir, cmd)
	if err := in.checkOwned(link, appDir, prev); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(in.Paths.BinDir, 0o755); err != nil {
		return nil, err
	}
	if err := t.prepare(link); err != nil {
		return nil, err
	}
	if err := os.WriteFile(link, []byte(launcherScript(d.FullName, execPath)), 0o755); err != nil {
		return nil, err
	}
	files = append(files, link)

	// Icon: from the extracted package or downloaded from the repository. A
	// failure here does not stop the installation.
	id := appID(owner, repo)
	iconName := "application-x-executable"
	if iconPath, err := in.installIcon(ctx, t, id, versionDir, repo, d.IconURL); err != nil {
		in.log().Warn("icon not installed", "repo", d.FullName, "err", err)
	} else if iconPath != "" {
		iconName = id
		files = append(files, iconPath)
	}

	// .desktop
	desktopPath := filepath.Join(in.Paths.Applications, id+".desktop")
	if err := in.checkOwned(desktopPath, "", prev); err != nil {
		return nil, err
	}
	content := in.desktopEntry(d, m, owner, repo, execPath, iconName, d.Repo.LatestTag)
	if err := os.MkdirAll(in.Paths.Applications, 0o755); err != nil {
		return nil, err
	}
	if err := t.prepare(desktopPath); err != nil {
		return nil, err
	}
	if err := os.WriteFile(desktopPath, []byte(content), 0o644); err != nil {
		return nil, err
	}
	files = append(files, desktopPath)

	inst := &store.Install{
		FullName:    d.FullName,
		Version:     d.Repo.LatestTag,
		InstalledAt: time.Now(),
		ExecPath:    execPath,
		DesktopPath: desktopPath,
		Files:       files,
	}
	// The version this update replaces stays on disk and recorded: a copy
	// that is running keeps its files, and Rollback can go back to it. A
	// reinstall of the same version keeps the previous one it had.
	if prev != nil {
		prevDir := filepath.Join(appDir, sanitizeVersion(prev.Version))
		switch {
		case prevDir != versionDir && registered(prev.Files, prevDir) && isDir(prevDir):
			inst.PreviousVersion, inst.PreviousExec = prev.Version, prev.ExecPath
			inst.Files = append(inst.Files, prevDir)
		case prevDir == versionDir && prev.PreviousVersion != "":
			older := filepath.Join(appDir, sanitizeVersion(prev.PreviousVersion))
			if older != versionDir && registered(prev.Files, older) && isDir(older) {
				inst.PreviousVersion, inst.PreviousExec = prev.PreviousVersion, prev.PreviousExec
				inst.Files = append(inst.Files, older)
			}
		}
	}
	if testHookBeforeSave != nil {
		if err := testHookBeforeSave(); err != nil {
			return nil, err
		}
	}
	if err := in.Store.SaveInstall(ctx, *inst); err != nil {
		return nil, err
	}
	return inst, nil
}

func registered(files []string, p string) bool {
	for _, f := range files {
		if f == p {
			return true
		}
	}
	return false
}

func isDir(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.IsDir()
}

// desktopEntry renders the app's .desktop for an executable and version.
func (in *Installer) desktopEntry(d *store.AppDetail, m *manifest.Manifest, owner, repo, execPath, icon, version string) string {
	return Desktop{
		Name:        d.Name,
		Comment:     d.Summary,
		Exec:        execPath,
		Icon:        icon,
		Terminal:    terminalFor(d, m),
		TUILauncher: in.TUILauncher,
		AppID:       "omastore." + strings.ToLower(owner+"."+repo),
		Categories:  categoriesFor(d, m),
		Repo:        d.FullName,
		Version:     version,
	}.Render()
}

// IsBroken reports whether an installation lost its executable outside
// OmaStore (a folder deleted by hand, a cleanup tool): it is still recorded,
// but Open cannot work. Reinstalling repairs it.
func IsBroken(inst store.Install) bool {
	if inst.ExecPath == "" {
		return false
	}
	st, err := os.Stat(inst.ExecPath)
	return err != nil || !st.Mode().IsRegular()
}

// Rollback goes back to the version the last update replaced, still on
// disk: the launcher and the .desktop point at it again, and the two versions
// swap places (so rolling back twice returns to the newer one). The catalog
// then offers the update again; nothing is downloaded.
func (in *Installer) Rollback(ctx context.Context, fullName string) (*store.Install, error) {
	unlock, err := in.lock(fullName)
	if err != nil {
		return nil, err
	}
	defer unlock()
	inst, err := in.Store.GetInstall(ctx, fullName)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%s: %w", fullName, ErrNotInstalled)
	}
	if err != nil {
		return nil, err
	}
	if inst.PreviousVersion == "" || inst.PreviousExec == "" {
		return nil, fmt.Errorf("%s: %w", inst.FullName, ErrNoPrevious)
	}
	if st, err := os.Stat(inst.PreviousExec); err != nil || !st.Mode().IsRegular() || !within(in.Paths.AppsDir, inst.PreviousExec) {
		return nil, fmt.Errorf("%s: %w: %s is gone", inst.FullName, ErrNoPrevious, inst.PreviousVersion)
	}
	d, err := in.Store.GetApp(ctx, inst.FullName)
	if err != nil {
		return nil, fmt.Errorf("app %s: %w", inst.FullName, err)
	}
	owner, repo, err := splitName(inst.FullName)
	if err != nil {
		return nil, err
	}
	m := manifest.Decode(d.Manifest)

	icon := "application-x-executable"
	id := appID(owner, repo)
	var launchers []string
	for _, f := range inst.Files {
		switch {
		case within(in.Paths.Icons, f):
			icon = id
		case filepath.Dir(f) == filepath.Clean(in.Paths.BinDir) && isOurLink(f, in.Paths.AppsDir):
			launchers = append(launchers, f)
		}
	}

	t := &tx{}
	write := func(p, content string, mode os.FileMode) error {
		if err := t.prepare(p); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(content), mode)
	}
	err = func() error {
		for _, l := range launchers {
			if err := write(l, launcherScript(inst.FullName, inst.PreviousExec), 0o755); err != nil {
				return err
			}
		}
		if inst.DesktopPath != "" {
			content := in.desktopEntry(d, m, owner, repo, inst.PreviousExec, icon, inst.PreviousVersion)
			if err := write(inst.DesktopPath, content, 0o644); err != nil {
				return err
			}
		}
		back := *inst
		back.Version, back.PreviousVersion = inst.PreviousVersion, inst.Version
		back.ExecPath, back.PreviousExec = inst.PreviousExec, inst.ExecPath
		back.InstalledAt = time.Now()
		if err := in.Store.SaveInstall(ctx, back); err != nil {
			return err
		}
		*inst = back
		return nil
	}()
	if err != nil {
		if rbErr := t.rollback(); rbErr != nil {
			in.log().Error("incomplete rollback", "repo", inst.FullName, "err", rbErr)
		}
		return nil, err
	}
	if err := t.commit(); err != nil {
		in.log().Warn("could not remove backups", "repo", inst.FullName, "err", err)
	}
	in.runHooks(ctx)
	return inst, nil
}

// reservedCommands can never be created in ~/.local/bin by an app.
var reservedCommands = map[string]bool{"omastore": true, "omastored": true, "omastore-gui": true}

// systemBinDirs are checked so we never shadow system commands:
// ~/.local/bin usually comes before them in PATH.
var systemBinDirs = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin", "/usr/local/bin"}

// checkCommandName refuses launchers that would shadow system commands
// (e.g. an app whose executable is named "sudo" or "ls") or OmaStore's own.
func (in *Installer) checkCommandName(cmd string) error {
	if reservedCommands[cmd] {
		return fmt.Errorf("%w: the command %q is reserved for OmaStore", ErrConflict, cmd)
	}
	for _, d := range systemBinDirs {
		if _, err := os.Lstat(filepath.Join(d, cmd)); err == nil {
			return fmt.Errorf("%w: the command %q already exists in %s and would be shadowed", ErrConflict, cmd, d)
		}
	}
	return nil
}

// findExec uses the executable declared in the manifest for the architecture,
// if it exists in the package; otherwise, the FindExecutable heuristic.
func (in *Installer) findExec(staging, repo, format, tag string, m *manifest.Manifest) (string, error) {
	t, ok := m.Target(in.goarch())
	if ok && t.Exec != "" && format != asset.FormatBinary && format != asset.FormatAppImage {
		p, err := declaredExec(staging, manifest.Expand(t.Exec, tag))
		if err == nil {
			return p, nil
		}
		in.log().Warn("invalid manifest executable; using the heuristic", "exec", t.Exec, "err", err)
	}
	return FindExecutable(staging, repo)
}

// declaredExec validates a manifest path: it must be a regular file inside
// dir, without leaving it, not even through a symlink.
func declaredExec(dir, rel string) (string, error) {
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if !within(dir, p) {
		return "", fmt.Errorf("%w: %s", ErrUnsafePath, rel)
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if !within(realDir, resolved) {
		return "", fmt.Errorf("%w: %s points outside the package", ErrUnsafePath, rel)
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a file", rel)
	}
	return resolved, nil
}

func terminalFor(d *store.AppDetail, m *manifest.Manifest) bool {
	if m != nil && m.Terminal != nil {
		return *m.Terminal
	}
	return isTerminalApp(d.Repo.Topics)
}

func categoriesFor(d *store.AppDetail, m *manifest.Manifest) []string {
	if m != nil && len(m.Categories) > 0 && m.MainCategory() != "" {
		return m.Categories
	}
	return []string{d.Category}
}

// testHookBeforeSave lets tests simulate a failure in the last step.
var testHookBeforeSave func() error

// checkOwned refuses to overwrite a file that is not ours: it only accepts it
// if it does not exist, if it is registered in the previous installation or,
// for launchers, if it points inside ownDir.
func (in *Installer) checkOwned(p, ownDir string, prev *store.Install) error {
	_, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if prev != nil {
		for _, f := range prev.Files {
			if f == p {
				return nil
			}
		}
	}
	if ownDir != "" && isOurLink(p, ownDir) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrConflict, p)
}

// installIcon looks for the icon in the extracted content (e.g. a package's
// usr/share/icons) and, failing that, downloads iconURL. Returns "" if there is no icon.
func (in *Installer) installIcon(ctx context.Context, t *tx, id, versionDir, repo, iconURL string) (string, error) {
	var files []string
	filepath.WalkDir(versionDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			rel, _ := filepath.Rel(versionDir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if rel := gitrepo.FindIcon(files, repo); rel != "" {
		data, err := readSmall(filepath.Join(versionDir, filepath.FromSlash(rel)))
		if err == nil {
			if p, err := in.writeIcon(t, id, data); err == nil {
				return p, nil
			}
		}
	}
	if iconURL == "" {
		return "", nil
	}
	data, err := in.fetchSmall(ctx, iconURL)
	if err != nil {
		return "", err
	}
	return in.writeIcon(t, id, data)
}

func readSmall(p string) ([]byte, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxSmallBytes {
		return nil, fmt.Errorf("%s too large", p)
	}
	return os.ReadFile(p)
}

// Update installs the new version if there is one; otherwise returns ErrUpToDate.
func (in *Installer) Update(ctx context.Context, fullName string, opts Options) (*store.Install, error) {
	inst, err := in.Store.GetInstall(ctx, fullName)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%s: %w", fullName, ErrNotInstalled)
	}
	if err != nil {
		return nil, err
	}
	d, err := in.Store.GetApp(ctx, fullName)
	if err != nil {
		return nil, fmt.Errorf("app %s: %w", fullName, err)
	}
	if d.Repo.LatestTag == inst.Version {
		return inst, ErrUpToDate
	}
	return in.Install(ctx, fullName, opts)
}

// Uninstall removes only the paths registered in the database. If some of
// them cannot be removed, the record is kept with just those paths and the
// error wraps ErrIncomplete, so uninstalling again retries them. While the
// app runs it refuses with ErrInUse, unless force.
func (in *Installer) Uninstall(ctx context.Context, fullName string, force bool) error {
	unlock, err := in.lock(fullName)
	if err != nil {
		return err
	}
	defer unlock()
	inst, err := in.Store.GetInstall(ctx, fullName)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%s: %w", fullName, ErrNotInstalled)
	}
	if err != nil {
		return err
	}
	if !force {
		if owner, repo, err := splitName(inst.FullName); err == nil {
			if ps, err := procsUsing(filepath.Join(in.Paths.AppsDir, owner+"__"+repo)); err == nil && len(ps) > 0 {
				return fmt.Errorf("%s: %w: %s", inst.FullName, ErrInUse, describe(ps))
			}
		}
	}
	var pending []string
	var errs []error
	for i := len(inst.Files) - 1; i >= 0; i-- {
		if err := in.removeRegistered(inst.Files[i]); err != nil {
			pending = append(pending, inst.Files[i])
			errs = append(errs, err)
		}
	}
	defer in.runHooks(ctx)
	if len(pending) > 0 {
		inst.Files = pending
		if err := in.Store.SaveInstall(ctx, *inst); err != nil {
			return err
		}
		return fmt.Errorf("%s: %w: %w", inst.FullName, ErrIncomplete, errors.Join(errs...))
	}
	return in.Store.DeleteInstall(ctx, inst.FullName)
}

// removeRegistered removes a registered path, with safeguards: it must be in
// one of the managed directories; in ~/.local/bin only our launchers are removed;
// whole directories are only deleted inside apps/. It returns an error only
// when the path is still ours and could not be removed; a path that is gone,
// was replaced by someone else or is outside the managed directories is no
// longer OmaStore's to remove.
func (in *Installer) removeRegistered(p string) error {
	p = filepath.Clean(p)
	st, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	switch {
	case within(in.Paths.AppsDir, p) && p != filepath.Clean(in.Paths.AppsDir):
		err = os.RemoveAll(p)
		if err == nil {
			// The app directory goes too once its last version is gone
			// (os.Remove only removes empty directories).
			if dir := filepath.Dir(p); dir != filepath.Clean(in.Paths.AppsDir) {
				os.Remove(dir)
			}
		}
	case filepath.Dir(p) == filepath.Clean(in.Paths.BinDir):
		if !isOurLink(p, in.Paths.AppsDir) {
			in.log().Warn("no longer an OmaStore launcher; kept", "path", p)
			return nil
		}
		err = os.Remove(p)
	case (within(in.Paths.Applications, p) || within(in.Paths.Icons, p)) && st.Mode().IsRegular() &&
		strings.HasPrefix(path.Base(filepath.ToSlash(p)), "omastore-"):
		err = os.Remove(p)
	case within(in.Paths.Applications, p) || within(in.Paths.Icons, p):
		in.log().Warn("no longer an OmaStore file; kept", "path", p)
		return nil
	default:
		in.log().Warn("registered path outside the managed directories; ignored", "path", p)
		return nil
	}
	if err != nil {
		in.log().Warn("failed to remove", "path", p, "err", err)
	}
	return err
}

// System tools used by the hooks. Absolute paths: we never resolve them
// through PATH, which includes ~/.local/bin (where downloaded apps live).
var (
	updateDesktopDB = "/usr/bin/update-desktop-database"
	// omarchyTUI (not a hook) launches a TUI in Omarchy's terminal, or focuses it.
	omarchyTUI      = "/usr/bin/omarchy-launch-or-focus-tui"
	updateIconCache = "/usr/bin/gtk-update-icon-cache"
	hookTimeout     = 20 * time.Second
	errHookSkipped  = errors.New("tool not installed")
)

func runCommand(ctx context.Context, name string, args ...string) error {
	if _, err := os.Stat(name); err != nil {
		return errHookSkipped
	}
	ctx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}

func (in *Installer) runHooks(ctx context.Context) {
	if !in.Hooks {
		return
	}
	if err := runCommand(ctx, updateDesktopDB, "-q", in.Paths.Applications); err != nil && !errors.Is(err, errHookSkipped) {
		in.log().Debug("update-desktop-database failed", "err", err)
	}
	if err := runCommand(ctx, updateIconCache, "-q", "-t", "-f", in.Paths.Icons); err != nil && !errors.Is(err, errHookSkipped) {
		in.log().Debug("gtk-update-icon-cache failed", "err", err)
	}
}
