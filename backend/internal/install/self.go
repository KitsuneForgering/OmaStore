package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/KitsuneForgering/OmaStore/backend/internal/asset"
	"github.com/KitsuneForgering/OmaStore/backend/internal/flock"
)

// OmaStore updating itself. Only the per-user installation made by
// packaging/install.sh is updated here: it lives in
// $XDG_DATA_HOME/omastore/self/<version>/ with symlinks in ~/.local/bin. A
// package installation (/usr/bin) belongs to pacman and a development build
// to whoever built it.

// SelfRepo is OmaStore's own repository.
const SelfRepo = "KitsuneForgering/OmaStore"

// How OmaStore was installed (SelfInstall.Mode).
const (
	SelfManaged = "self"    // install.sh in the user's account: updatable here
	SelfPackage = "package" // /usr: updated by the system package manager
	SelfDev     = "dev"     // anything else (a development build)
)

// Errors of the self-update.
var (
	ErrSelfNotManaged = errors.New("this OmaStore was not installed by install.sh; update it the way it was installed")
	ErrSelfNoAsset    = errors.New("the release has no OmaStore build for this architecture")
	ErrSelfUnverified = errors.New("the release publishes no checksum for the OmaStore build")
)

// selfBinaries are the files a release must contain, all in usr/bin.
var selfBinaries = []string{"omastore", "omastored", "omastore-gui"}

const (
	selfDesktop = "usr/share/applications/omastore.desktop"
	selfIcon    = "usr/share/icons/hicolor/scalable/apps/omastore.svg"
	selfSkills  = "usr/share/omastore/skills"
	// selfMarker in the self directory means install.sh also created the menu
	// entry and the icon, which the update may then replace.
	selfMarker = ".managed"
	// skillMarker marks the agent skill copies install.sh made.
	skillMarker = ".omastore-managed"
	// selfNoSkills in the self directory: install.sh ran with --no-skills.
	selfNoSkills = ".no-skills"
)

// SelfInstall describes the running OmaStore.
type SelfInstall struct {
	Mode    string // SelfManaged, SelfPackage or SelfDev
	Version string // only for SelfManaged, without the "v"
	Root    string // $XDG_DATA_HOME/omastore/self
}

// SelfRoot is where install.sh keeps OmaStore's versions.
func (in *Installer) SelfRoot() string { return filepath.Join(in.Paths.DataDir, "self") }

// DetectSelf classifies the executable exe (normally os.Executable()).
func (in *Installer) DetectSelf(exe string) SelfInstall {
	root := in.SelfRoot()
	s := SelfInstall{Mode: SelfDev, Root: root}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	exe = filepath.Clean(exe)
	if rootReal, err := filepath.EvalSymlinks(root); err == nil {
		root = rootReal
	}
	if rel, err := filepath.Rel(root, exe); err == nil && !strings.HasPrefix(rel, "..") {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) == 3 && parts[1] == "bin" && reSelfVersion.MatchString(parts[0]) {
			if _, err := os.Stat(filepath.Join(root, selfMarker)); err == nil {
				s.Mode, s.Version = SelfManaged, parts[0]
			}
		}
		return s
	}
	if strings.HasPrefix(exe, "/usr/") {
		s.Mode = SelfPackage
	}
	return s
}

// reSelfVersion is a release version without the "v" (as in the tarball name).
var reSelfVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// SelfTag validates a release tag ("v1.2.3") and returns the version ("1.2.3").
func SelfTag(tag string) (string, bool) {
	v, ok := strings.CutPrefix(tag, "v")
	return v, ok && reSelfVersion.MatchString(v)
}

// SelfAssetName is the release tarball of version for goarch.
func SelfAssetName(version, goarch string) string {
	arch := map[string]string{asset.ArchAMD64: "x86_64", asset.ArchARM64: "aarch64"}[goarch]
	if arch == "" {
		arch = goarch
	}
	return "omastore-" + version + "-" + arch + "-linux.tar.gz"
}

// SelfAssetName is the release tarball of version for this installer's architecture.
func (in *Installer) SelfAssetName(version string) string { return SelfAssetName(version, in.goarch()) }

// NewerVersion reports whether version a is newer than b (semver precedence:
// a pre-release is older than its release; identifiers compare numerically
// when both are numbers).
func NewerVersion(a, b string) bool { return compareVersions(a, b) > 0 }

func compareVersions(a, b string) int {
	coreA, preA, _ := strings.Cut(a, "-")
	coreB, preB, _ := strings.Cut(b, "-")
	pa, pb := strings.Split(coreA, "."), strings.Split(coreB, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		if c := compareIdent(at(pa, i, "0"), at(pb, i, "0")); c != 0 {
			return c
		}
	}
	switch {
	case preA == preB:
		return 0
	case preA == "":
		return 1
	case preB == "":
		return -1
	}
	ia, ib := strings.Split(preA, "."), strings.Split(preB, ".")
	for i := 0; i < len(ia) && i < len(ib); i++ {
		if c := compareIdent(ia[i], ib[i]); c != 0 {
			return c
		}
	}
	return compareInt(len(ia), len(ib))
}

func at(s []string, i int, def string) string {
	if i < len(s) {
		return s[i]
	}
	return def
}

func compareIdent(a, b string) int {
	na, errA := strconv.Atoi(a)
	nb, errB := strconv.Atoi(b)
	switch {
	case errA == nil && errB == nil:
		return compareInt(na, nb)
	case errA == nil:
		return -1 // numeric identifiers sort before alphanumeric ones
	case errB == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// SelfRelease is the release to update to, as found on GitHub.
type SelfRelease struct {
	Tag         string
	AssetURL    string
	Digest      string // "sha256:<hex>" from the API, if any
	ChecksumURL string // the tarball's .sha256, if published
}

// SelfStatus says whether OmaStore itself has an update.
type SelfStatus struct {
	Mode            string // SelfManaged, SelfPackage or SelfDev
	Version         string // running version (only for SelfManaged)
	Latest          string // latest release version, "" if unknown
	UpdateAvailable bool   // Latest is newer and this installation can update itself
	Notes           string // release notes of Latest (markdown)
}

// SelfResult is a finished self-update.
type SelfResult struct {
	From string // previous version
	To   string // installed version
	GUI  string // launcher of the new interface (~/.local/bin/omastore-gui)
}

// SelfUpdate installs rel over the running OmaStore cur, the way install.sh
// would: verified tarball, new version directory, then the ~/.local/bin links,
// the menu entry, the icon and the agents' skill copies switched over. Nothing from
// the tarball is run. The previous version stays on disk (running processes
// use it) until the next update.
func (in *Installer) SelfUpdate(ctx context.Context, cur SelfInstall, rel SelfRelease, progress func(Progress)) (*SelfResult, error) {
	if progress == nil {
		progress = func(Progress) {}
	}
	if cur.Mode != SelfManaged {
		return nil, ErrSelfNotManaged
	}
	version, ok := SelfTag(rel.Tag)
	if !ok {
		return nil, fmt.Errorf("invalid release tag %q", rel.Tag)
	}
	if !NewerVersion(version, cur.Version) {
		return &SelfResult{From: cur.Version, To: cur.Version, GUI: filepath.Join(in.Paths.BinDir, "omastore-gui")}, ErrUpToDate
	}
	if !within(in.Paths.Home, cur.Root) {
		return nil, fmt.Errorf("%w: %s", ErrOutsideHome, cur.Root)
	}
	unlock, err := flock.TryLock(filepath.Join(cur.Root, ".update.lock"))
	if errors.Is(err, flock.ErrLocked) {
		return nil, fmt.Errorf("OmaStore: %w", ErrBusy)
	}
	if err != nil {
		return nil, err
	}
	defer unlock()

	name := in.SelfAssetName(version)
	if path := strings.TrimSuffix(rel.AssetURL, "/"); filepath.Base(path) != name {
		return nil, fmt.Errorf("%w (%s)", ErrSelfNoAsset, name)
	}
	want, err := in.expected(ctx, rel.Digest, rel.ChecksumURL, name)
	if err != nil {
		return nil, err
	}
	if want == "" {
		return nil, ErrSelfUnverified
	}

	staging, err := os.MkdirTemp(cur.Root, ".staging-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	archive := filepath.Join(staging, name)
	progress(Progress{Stage: StageDownload})
	sum256, sum512, err := in.download(ctx, rel.AssetURL, archive, func(done, total int64) {
		progress(Progress{Stage: StageDownload, Done: done, Total: total})
	})
	if err != nil {
		return nil, err
	}
	progress(Progress{Stage: StageVerify})
	if err := verify(want, sum256, sum512); err != nil {
		return nil, err
	}

	progress(Progress{Stage: StageExtract})
	tree := filepath.Join(staging, "tree")
	if err := Extract(archive, asset.FormatTarGz, tree, ""); err != nil {
		return nil, fmt.Errorf("extract %s: %w", name, err)
	}
	for _, b := range selfBinaries {
		if err := regularFile(filepath.Join(tree, "usr", "bin", b), true); err != nil {
			return nil, fmt.Errorf("release %s: %w", rel.Tag, err)
		}
	}
	// The version directory is assembled aside and renamed into place whole.
	build := filepath.Join(staging, "version")
	if err := os.MkdirAll(build, 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(filepath.Join(tree, "usr", "bin"), filepath.Join(build, "bin")); err != nil {
		return nil, err
	}
	skills := filepath.Join(tree, filepath.FromSlash(selfSkills))
	hasSkills := regularTree(skills) == nil
	if hasSkills {
		if err := os.MkdirAll(filepath.Join(build, "share"), 0o755); err != nil {
			return nil, err
		}
		if err := os.Rename(skills, filepath.Join(build, "share", "skills")); err != nil {
			return nil, err
		}
	}
	dest := filepath.Join(cur.Root, version)
	if err := os.RemoveAll(dest); err != nil { // a leftover of an interrupted update
		return nil, err
	}
	if err := os.Rename(build, dest); err != nil {
		return nil, err
	}

	progress(Progress{Stage: StageIntegrate})
	t := &tx{}
	fail := func(err error) (*SelfResult, error) {
		if rerr := t.rollback(); rerr != nil {
			in.log().Error("self-update rollback incomplete", "err", rerr)
		}
		os.RemoveAll(dest)
		return nil, err
	}
	for _, b := range selfBinaries {
		link := filepath.Join(in.Paths.BinDir, b)
		if target, err := os.Readlink(link); err == nil {
			if !in.isSelfLink(cur.Root, target, b) {
				return fail(fmt.Errorf("%w: %s", ErrConflict, link))
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fail(fmt.Errorf("%w: %s", ErrConflict, link))
		}
		if err := t.prepare(link); err != nil {
			return fail(err)
		}
		if err := os.Symlink(filepath.Join(dest, "bin", b), link); err != nil {
			return fail(err)
		}
	}
	if _, err := os.Stat(filepath.Join(cur.Root, selfMarker)); err == nil {
		if err := in.selfMenuEntry(t, tree); err != nil {
			return fail(err)
		}
	}
	if err := t.commit(); err != nil {
		in.log().Warn("self-update backups not removed", "err", err)
	}
	if hasSkills {
		in.refreshSkills(cur.Root, filepath.Join(dest, "share", "skills"))
	}
	in.refreshOmarchyHook()
	in.runHooks(ctx)
	in.pruneSelf(cur.Root, version, cur.Version)
	progress(Progress{Stage: StageDone})
	return &SelfResult{From: cur.Version, To: version, GUI: filepath.Join(in.Paths.BinDir, "omastore-gui")}, nil
}

// isSelfLink reports whether a ~/.local/bin link target belongs to install.sh.
func (in *Installer) isSelfLink(root, target, name string) bool {
	if !filepath.IsAbs(target) {
		return false
	}
	rel, err := filepath.Rel(root, filepath.Clean(target))
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return len(parts) == 3 && reSelfVersion.MatchString(parts[0]) && parts[1] == "bin" && parts[2] == name
}

// selfMenuEntry replaces the menu entry and the icon, pointing Exec at the
// ~/.local/bin launcher (as install.sh does).
func (in *Installer) selfMenuEntry(t *tx, tree string) error {
	desktop, err := readSmall(filepath.Join(tree, filepath.FromSlash(selfDesktop)))
	if err != nil {
		return fmt.Errorf("release menu entry: %w", err)
	}
	icon, err := readSmall(filepath.Join(tree, filepath.FromSlash(selfIcon)))
	if err != nil {
		return fmt.Errorf("release icon: %w", err)
	}
	execLine := []byte("Exec=" + escapeValue(quoteExecArg(filepath.Join(in.Paths.BinDir, "omastore-gui"))))
	lines := bytes.Split(desktop, []byte("\n"))
	for i, l := range lines {
		l = bytes.TrimRight(l, "\r")
		// The field codes after the command (%u for omastore:// links) stay.
		if bytes.Equal(l, []byte("Exec=omastore-gui")) {
			lines[i] = execLine
		} else if args, ok := bytes.CutPrefix(l, []byte("Exec=omastore-gui ")); ok {
			lines[i] = append(append(append([]byte{}, execLine...), ' '), args...)
		}
	}
	files := []struct {
		path string
		data []byte
	}{
		{filepath.Join(in.Paths.Applications, "omastore.desktop"), bytes.Join(lines, []byte("\n"))},
		{filepath.Join(in.Paths.Icons, "scalable", "apps", "omastore.svg"), icon},
	}
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return err
		}
		if err := t.prepare(f.path); err != nil {
			return err
		}
		if err := os.WriteFile(f.path, f.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// skillDirs are the coding agents' skill directories install.sh copies the
// author skills into, for the agents installed here (whose home exists): the
// shared ~/.agents/skills (OpenCode, Copilot, Gemini, Cursor, Crush, Oh My
// Pi, Grok, Muse, OpenClaw...), Claude Code, Codex, Pi and Hermes with its
// profiles. The same directories Omarchy links its own skills into.
func (in *Installer) skillDirs() []string {
	envOr := func(env, rel string) string {
		if v := os.Getenv(env); v != "" {
			return v
		}
		return filepath.Join(in.Paths.Home, rel)
	}
	homes := []string{envOr("CLAUDE_CONFIG_DIR", ".claude"), envOr("CODEX_HOME", ".codex"),
		filepath.Join(in.Paths.Home, ".agents"), filepath.Join(in.Paths.Home, ".pi", "agent"),
		filepath.Join(in.Paths.Home, ".hermes")}
	profiles, _ := filepath.Glob(filepath.Join(in.Paths.Home, ".hermes", "profiles", "*"))
	homes = append(homes, profiles...)
	var dirs []string
	for _, h := range homes {
		if st, err := os.Stat(h); err != nil || !st.IsDir() || !within(in.Paths.Home, h) {
			continue
		}
		dirs = append(dirs, filepath.Join(h, "skills"))
	}
	return dirs
}

// Omarchy post-update hook written by install.sh (always under ~/.config).
const (
	omarchyHookRel    = ".config/omarchy/hooks/post-update.d/omastore.hook"
	omarchyHookMarker = "# omastore-managed"
	// What hooks from before automatic updates ran, and what they run now
	// (the same lines install.sh writes).
	omarchyHookOld = `[[ -x $cli ]] && timeout 20 "$cli" update --check --notify >/dev/null 2>&1`
	omarchyHookNew = "if [[ -x $cli ]]; then\n" +
		"  echo \"Updating OmaStore apps…\"\n" +
		"  timeout 600 \"$cli\" update --auto --notify 2>&1 | sed 's/^/  /'\n" +
		"fi"
)

// refreshOmarchyHook brings a hook install.sh wrote before automatic updates
// to the current command, so omarchy-update updates the apps. A hook without
// the marker is the user's and stays as it is.
func (in *Installer) refreshOmarchyHook() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	p := filepath.Join(home, filepath.FromSlash(omarchyHookRel))
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() {
		return
	}
	b, err := readSmall(p)
	if err != nil || !bytes.Contains(b, []byte("\n"+omarchyHookMarker+"\n")) || !bytes.Contains(b, []byte(omarchyHookOld)) {
		return
	}
	b = bytes.Replace(b, []byte(omarchyHookOld), []byte(omarchyHookNew), 1)
	tmp := p + ".omastore-new-" + randSuffix()
	if err := os.WriteFile(tmp, b, st.Mode().Perm()); err != nil {
		in.log().Warn("Omarchy hook not updated", "err", err)
		return
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		in.log().Warn("Omarchy hook not updated", "err", err)
	}
}

// backupSkill moves a skill directory OmaStore did not install into
// $XDG_DATA_HOME/omastore/skill-backups, which install.sh --uninstall keeps.
func (in *Installer) backupSkill(dest string) error {
	root := filepath.Join(in.Paths.DataDir, "skill-backups")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(root, filepath.Base(dest)+".")
	if err != nil {
		return err
	}
	if err := os.Rename(dest, filepath.Join(dir, filepath.Base(dest))); err != nil {
		os.Remove(dir)
		return err
	}
	in.log().Info("replaced a skill OmaStore did not install", "skill", dest, "backup", dir)
	return nil
}

// refreshSkills brings the agents' skill copies to the release's set, as
// install.sh does: copies are added or replaced, the ones a release dropped
// are removed, and a directory of the same name not made by OmaStore (no
// skillMarker) is moved to skill-backups so the new one is installed. Nothing happens when install.sh ran with
// --no-skills (selfNoSkills in the self directory).
func (in *Installer) refreshSkills(root, src string) {
	if _, err := os.Stat(filepath.Join(root, selfNoSkills)); err == nil {
		return
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return
	}
	want := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "omastore-") {
			want[e.Name()] = true
		}
	}
	ours := func(p string) bool {
		st, err := os.Lstat(p)
		if err != nil || !st.IsDir() {
			return false
		}
		_, err = os.Stat(filepath.Join(p, skillMarker))
		return err == nil
	}
	for _, dir := range in.skillDirs() {
		old, _ := filepath.Glob(filepath.Join(dir, "omastore-*"))
		for _, p := range old {
			if !want[filepath.Base(p)] && ours(p) {
				os.RemoveAll(p)
			}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			in.log().Warn("skills not installed", "dir", dir, "err", err)
			continue
		}
		for name := range want {
			dest := filepath.Join(dir, name)
			if _, err := os.Lstat(dest); err == nil && !ours(dest) {
				// Another copy (an older OmaStore's, or one copied by hand):
				// the new one wins, the old one is kept in skill-backups.
				if err := in.backupSkill(dest); err != nil {
					in.log().Warn("skill not replaced", "skill", name, "dir", dir, "err", err)
					continue
				}
			}
			tmp := dest + ".omastore-new-" + randSuffix()
			if err := os.CopyFS(tmp, os.DirFS(filepath.Join(src, name))); err != nil {
				os.RemoveAll(tmp)
				in.log().Warn("skill not installed", "skill", name, "dir", dir, "err", err)
				continue
			}
			os.WriteFile(filepath.Join(tmp, skillMarker), nil, 0o644)
			if err := os.RemoveAll(dest); err != nil || os.Rename(tmp, dest) != nil {
				os.RemoveAll(tmp)
				in.log().Warn("skill not installed", "skill", name, "dir", dir)
			}
		}
	}
}

// pruneSelf removes the version directories other than keep... and leftovers
// of interrupted updates.
func (in *Installer) pruneSelf(root string, keep ...string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() || (!reSelfVersion.MatchString(n) && !strings.HasPrefix(n, ".staging-")) {
			continue
		}
		kept := false
		for _, k := range keep {
			kept = kept || n == k
		}
		if !kept {
			if err := os.RemoveAll(filepath.Join(root, n)); err != nil {
				in.log().Warn("old OmaStore version not removed", "dir", n, "err", err)
			}
		}
	}
}

// regularFile checks that p is a regular file (and executable, if asked).
func regularFile(p string, executable bool) error {
	st, err := os.Lstat(p)
	if err != nil {
		return fmt.Errorf("missing %s", filepath.Base(p))
	}
	if !st.Mode().IsRegular() || (executable && st.Mode()&0o111 == 0) {
		return fmt.Errorf("%s is not a regular executable file", filepath.Base(p))
	}
	return nil
}

// regularTree checks that dir exists and holds only directories and regular files.
func regularTree(dir string) error {
	if st, err := os.Lstat(dir); err != nil || !st.IsDir() {
		return fmt.Errorf("missing %s", dir)
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return fmt.Errorf("unexpected link or special file: %s", p)
		}
		return nil
	})
}
