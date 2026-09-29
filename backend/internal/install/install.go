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

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/gitrepo"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/manifest"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/xdg"
)

// Errors returned by the installer.
var (
	ErrNotInstallable = errors.New("app has no binary for this architecture")
	ErrNotInstalled   = errors.New("app is not installed")
	ErrUpToDate       = errors.New("app is already at the latest version")
	ErrConflict       = errors.New("file already exists and does not belong to OmaStore")
	ErrOutsideHome    = errors.New("path outside $HOME")
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
	Log   *slog.Logger

	locks sync.Map // full_name → *sync.Mutex
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
	return &Installer{Store: st, Paths: p, Hooks: true}, nil
}

func (in *Installer) http() *http.Client {
	if in.HTTP != nil {
		return in.HTTP
	}
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 60 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
	}}
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

func (in *Installer) lock(fullName string) func() {
	m, _ := in.locks.LoadOrStore(strings.ToLower(fullName), &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// within reports whether p is inside root (or is root).
func within(root, p string) bool {
	if root == "" || !filepath.IsAbs(p) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// reName validates owner and repo with GitHub's naming rules, so they cannot
// turn into dangerous path components.
var reName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

func splitName(fullName string) (owner, repo string, err error) {
	owner, repo, ok := strings.Cut(fullName, "/")
	if !ok || !reName.MatchString(owner) || !reName.MatchString(repo) || repo == ".." || strings.Contains(repo, "..") {
		return "", "", fmt.Errorf("invalid repository name: %q", fullName)
	}
	return owner, repo, nil
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
		if (index.AssetInfo{Format: a.Format, Arch: a.Arch}).Installable(goarch) {
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
		ri, rj := index.FormatRank(cands[i].Format), index.FormatRank(cands[j].Format)
		if ri != rj {
			return ri < rj
		}
		return cands[i].Name < cands[j].Name
	})
	return cands[0], true
}

// Install installs (or reinstalls/updates) the latest release of fullName.
func (in *Installer) Install(ctx context.Context, fullName string, progress func(Progress)) (*store.Install, error) {
	defer in.lock(fullName)()
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
	asset, ok := SelectAsset(d.Assets, in.goarch(), m)
	if !d.Installable || !ok {
		return nil, fmt.Errorf("%s: %w", fullName, ErrNotInstallable)
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
	report(Progress{Stage: StageDownload, Total: asset.Size})
	sum256, sum512, err := in.download(ctx, asset.URL, archive, func(done, total int64) {
		report(Progress{Stage: StageDownload, Done: done, Total: total})
	})
	if err != nil {
		return nil, err
	}

	// 2. Checksum verification, when the release publishes one.
	report(Progress{Stage: StageVerify})
	want, err := in.expected(ctx, asset.Digest, asset.ChecksumURL, asset.Name)
	if err != nil {
		return nil, err
	}
	if want == "" {
		in.log().Warn("release without checksum; installing without verification", "repo", fullName, "asset", asset.Name)
	} else if err := verify(want, sum256, sum512); err != nil {
		return nil, fmt.Errorf("%s: %w", asset.Name, err)
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
	if asset.Format == index.FormatAppImage {
		binName += ".AppImage"
	}
	if err := Extract(archive, asset.Format, staging, binName); err != nil {
		return nil, fmt.Errorf("extract %s: %w", asset.Name, err)
	}
	execAbs, err := in.findExec(staging, repo, asset.Format, asset.Tag, m)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", asset.Name, err)
	}
	execRel, _ := filepath.Rel(staging, execAbs)
	if err := os.Chmod(execAbs, 0o755); err != nil {
		return nil, err
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
	// uses (e.g. the old version directory).
	if prev != nil {
		keep := map[string]bool{}
		for _, f := range inst.Files {
			keep[f] = true
		}
		for _, f := range prev.Files {
			if !keep[f] {
				in.removeRegistered(f)
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
	content := Desktop{
		Name:       d.Name,
		Comment:    d.Summary,
		Exec:       execPath,
		Icon:       iconName,
		Terminal:   terminalFor(d, m),
		Categories: categoriesFor(d, m),
		Repo:       d.FullName,
		Version:    d.Repo.LatestTag,
	}.Render()
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
	if ok && t.Exec != "" && format != index.FormatBinary && format != index.FormatAppImage {
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
func (in *Installer) Update(ctx context.Context, fullName string, progress func(Progress)) (*store.Install, error) {
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
	return in.Install(ctx, fullName, progress)
}

// Uninstall removes only the paths registered in the database.
func (in *Installer) Uninstall(ctx context.Context, fullName string) error {
	defer in.lock(fullName)()
	inst, err := in.Store.GetInstall(ctx, fullName)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%s: %w", fullName, ErrNotInstalled)
	}
	if err != nil {
		return err
	}
	for i := len(inst.Files) - 1; i >= 0; i-- {
		in.removeRegistered(inst.Files[i])
	}
	if owner, repo, err := splitName(inst.FullName); err == nil {
		os.Remove(filepath.Join(in.Paths.AppsDir, owner+"__"+repo)) // only if empty
	}
	if err := in.Store.DeleteInstall(ctx, inst.FullName); err != nil {
		return err
	}
	in.runHooks(ctx)
	return nil
}

// removeRegistered removes a registered path, with safeguards: it must be in
// one of the managed directories; in ~/.local/bin only our launchers are removed;
// whole directories are only deleted inside apps/.
func (in *Installer) removeRegistered(p string) {
	p = filepath.Clean(p)
	st, err := os.Lstat(p)
	if err != nil {
		return
	}
	switch {
	case within(in.Paths.AppsDir, p) && p != filepath.Clean(in.Paths.AppsDir):
		err = os.RemoveAll(p)
	case filepath.Dir(p) == filepath.Clean(in.Paths.BinDir):
		if !isOurLink(p, in.Paths.AppsDir) {
			in.log().Warn("no longer an OmaStore launcher; kept", "path", p)
			return
		}
		err = os.Remove(p)
	case (within(in.Paths.Applications, p) || within(in.Paths.Icons, p)) && st.Mode().IsRegular() &&
		strings.HasPrefix(path.Base(filepath.ToSlash(p)), "omastore-"):
		err = os.Remove(p)
	default:
		in.log().Warn("registered path outside the managed directories; ignored", "path", p)
		return
	}
	if err != nil {
		in.log().Warn("failed to remove", "path", p, "err", err)
	}
}

// System tools used by the hooks. Absolute paths: we never resolve them
// through PATH, which includes ~/.local/bin (where downloaded apps live).
var (
	updateDesktopDB = "/usr/bin/update-desktop-database"
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
