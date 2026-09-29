// Command omastore: OmaStore's debugging CLI. Uses the same services as
// the daemon, without needing the GUI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/app"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/install"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/logging"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/manifest"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/notify"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
)

const usage = `usage: omastore [-v] <command> [options]

commands:
  index [--force] [--prune] [--max N] [--no-batch] [--manifest file] [owner/repo...]
                                    index the catalog (or only the given repos)
  list [--category C] [--query Q] [--installed] [--all] [--json]
  categories                        list the categories
  show [--json] owner/repo          details of an app
  similar [--json] [--limit N] owner/repo
                                    similar apps (search: list --query)
  lint-manifest [dir|file]          validate an omastore.toml (default: current directory)
  install owner/repo...             install the latest release
  uninstall owner/repo...           remove an installed app
  update [owner/repo...]            update the given apps (or every installed one)
  update --check [--notify]         list updates; --notify shows a desktop notification

environment: GITHUB_TOKEN (or gh auth token), OMASTORE_LOG=debug|info|warn|error
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// errUsage signals a usage error (exit code 2).
var errUsage = errors.New("invalid usage")

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	global := flag.NewFlagSet("omastore", flag.ContinueOnError)
	global.SetOutput(stderr)
	verbose := global.Bool("v", false, "log detalhado")
	global.Usage = func() { fmt.Fprint(stderr, usage) }
	if err := global.Parse(args); err != nil {
		return 2
	}
	rest := global.Args()
	if len(rest) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	level := ""
	if *verbose {
		level = "debug"
	}
	log := logging.Setup(stderr, level)

	cmd, cmdArgs := rest[0], rest[1:]
	commands := map[string]func(context.Context, *app.App, []string, io.Writer, io.Writer) error{
		"index":      cmdIndex,
		"list":       cmdList,
		"categories": cmdCategories,
		"show":       cmdShow,
		"similar":    cmdSimilar,
		"install":    cmdInstall,
		"uninstall":  cmdUninstall,
		"update":     cmdUpdate,
	}
	// Commands that need neither the database nor the network.
	if cmd == "lint-manifest" {
		if err := cmdLintManifest(cmdArgs, stdout, stderr); err != nil {
			if errors.Is(err, errUsage) {
				return 2
			}
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}

	fn, ok := commands[cmd]
	if !ok {
		if cmd == "help" || cmd == "-h" || cmd == "--help" {
			fmt.Fprint(stdout, usage)
			return 0
		}
		fmt.Fprintf(stderr, "unknown command: %q\n\n%s", cmd, usage)
		return 2
	}

	a, err := app.Open(ctx, log)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	defer a.Close()

	if err := fn(ctx, a, cmdArgs, stdout, stderr); err != nil {
		if errors.Is(err, errUsage) || errors.Is(err, flag.ErrHelp) {
			return 2
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	return nil
}

func cmdIndex(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) error {
	fs := newFlags("index", stderr)
	force := fs.Bool("force", false, "reprocess everything, ignoring the cache")
	prune := fs.Bool("prune", false, "remove repos that are no longer found from the catalog")
	maxSearch := fs.Int("max", 0, "maximum topic search results (default 300)")
	noBatch := fs.Bool("no-batch", false, "do not use the batch query (GraphQL), only the REST API")
	manifestFile := fs.String("manifest", "", "use this local omastore.toml instead of the published one (requires a single owner/repo)")
	if err := parse(fs, args); err != nil {
		return err
	}
	a.Indexer.Prune = *prune
	a.Indexer.MaxSearch = *maxSearch
	a.Indexer.NoBatch = *noBatch
	var overrides map[string]string
	if *manifestFile != "" {
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "--manifest requires exactly one owner/repo")
			return errUsage
		}
		data, err := os.ReadFile(*manifestFile)
		if err != nil {
			return err
		}
		overrides = map[string]string{fs.Arg(0): string(data)}
	}
	reqBefore := a.GitHub.Requests()
	tty := isTerminal(stderr)
	start := time.Now()
	stats, err := a.Indexer.Run(ctx, index.Options{
		Force:            *force,
		Only:             fs.Args(),
		ManifestOverride: overrides,
		Progress: func(p index.Progress) {
			if tty {
				fmt.Fprintf(stderr, "\r[%d/%d] %-50.50s", p.Done, p.Total, p.Current)
			}
		},
	})
	if tty {
		fmt.Fprintln(stderr)
	}
	fmt.Fprintf(stdout, "updated: %d, stats only: %d, unchanged: %d, removed: %d, no app manifest: %d, skipped: %d, failed: %d (%s, %d requests)\n",
		stats.Updated, stats.Refreshed, stats.Unchanged, stats.Removed, stats.NotApps, stats.Skipped, stats.Failed,
		time.Since(start).Round(time.Millisecond), a.GitHub.Requests()-reqBefore)
	return err
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func cmdList(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) error {
	fs := newFlags("list", stderr)
	var f store.Filter
	fs.StringVar(&f.Category, "category", "", "filter by category")
	fs.StringVar(&f.Query, "query", "", "busca por texto")
	fs.BoolVar(&f.InstalledOnly, "installed", false, "installed only")
	fs.BoolVar(&f.All, "all", false, "include apps without an installable binary")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parse(fs, args); err != nil {
		return err
	}
	items, err := a.ListApps(ctx, f)
	if err != nil {
		return err
	}
	return printItems(stdout, items, *asJSON)
}

func printItems(stdout io.Writer, items []store.ListItem, asJSON bool) error {
	if asJSON {
		return writeJSON(stdout, items)
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "REPO\tNAME\tCATEGORY\t★\tVERSION\tINSTALLED\tSUMMARY")
	for _, it := range items {
		inst := ""
		if it.InstalledVersion != "" {
			inst = it.InstalledVersion
			if it.InstalledVersion != it.LatestTag {
				inst += " (outdated)"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n", it.FullName, it.Name, it.Category, it.Stars,
			it.LatestTag, inst, clip(it.Summary, 60))
	}
	return tw.Flush()
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func cmdSimilar(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) error {
	fs := newFlags("similar", stderr)
	asJSON := fs.Bool("json", false, "JSON output")
	limit := fs.Int("limit", 8, "maximum results")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: omastore similar [--json] [--limit N] owner/repo")
		return errUsage
	}
	items, err := a.Similar(ctx, fs.Arg(0), *limit)
	if err != nil {
		return fmt.Errorf("%s: %w", fs.Arg(0), err)
	}
	return printItems(stdout, items, *asJSON)
}

var errLint = errors.New("manifest has errors")

func cmdLintManifest(args []string, stdout, stderr io.Writer) error {
	target := "."
	if len(args) > 1 {
		fmt.Fprintln(stderr, "usage: omastore lint-manifest [dir|file]")
		return errUsage
	}
	if len(args) == 1 {
		target = args[0]
	}
	file, dir := target, ""
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		dir = target
		file = filepath.Join(target, manifest.FileName)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	m, problems, err := manifest.Parse(data, true)
	if err != nil {
		fmt.Fprintf(stdout, "error: (file): %v\n", err)
		return fmt.Errorf("%w: 1 error(s)", errLint)
	}
	// With the repository directory, check that the files exist.
	if dir != "" && m != nil {
		paths := append([]string{}, m.Screenshots...)
		if m.Icon != "" {
			paths = append(paths, m.Icon)
		}
		for _, p := range paths {
			if strings.HasPrefix(p, "https://") {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
				problems = append(problems, manifest.Problem{Field: p, Message: "file does not exist in the repository"})
			}
		}
	}
	errs := 0
	for _, p := range problems {
		fmt.Fprintln(stdout, p.String())
		if !p.Warning {
			errs++
		}
	}
	if errs > 0 {
		return fmt.Errorf("%w: %d error(s)", errLint, errs)
	}
	if !m.IsApp() {
		return nil // the kind warning was already printed with the problems
	}
	if m.Empty() {
		fmt.Fprintf(stdout, "ok: %s (empty: the repository joins the catalog and the rest is inferred)\n", file)
		return nil
	}
	fmt.Fprintf(stdout, "ok: %s\n", file)
	if m.Name != "" {
		fmt.Fprintf(stdout, "  name:        %s\n", m.Name)
	}
	if c := m.MainCategory(); c != "" {
		fmt.Fprintf(stdout, "  category:    %s\n", c)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		if t, ok := m.Target(arch); ok {
			fmt.Fprintf(stdout, "  %-6s       asset=%q exec=%q\n", arch, t.Asset, t.Exec)
		}
	}
	return nil
}

func cmdCategories(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) error {
	cats, err := a.Store.Categories(ctx)
	if err != nil {
		return err
	}
	for _, c := range cats {
		fmt.Fprintf(stdout, "%-14s %d\n", c.Category, c.Count)
	}
	return nil
}

func cmdShow(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) error {
	fs := newFlags("show", stderr)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: omastore show [--json] owner/repo")
		return errUsage
	}
	d, err := a.Store.GetApp(ctx, fs.Arg(0))
	if err != nil {
		return fmt.Errorf("%s: %w", fs.Arg(0), err)
	}
	if *asJSON {
		return writeJSON(stdout, d)
	}
	fmt.Fprintf(stdout, "%s (%s)\n", d.Name, d.FullName)
	fmt.Fprintf(stdout, "  %s\n\n", d.Summary)
	fmt.Fprintf(stdout, "category:    %s\n", d.Category)
	fmt.Fprintf(stdout, "stars:       %d\n", d.Repo.Stars)
	fmt.Fprintf(stdout, "license:     %s\n", d.Repo.License)
	fmt.Fprintf(stdout, "topics:      %s\n", strings.Join(d.Repo.Topics, ", "))
	fmt.Fprintf(stdout, "release:     %s\n", d.Repo.LatestTag)
	fmt.Fprintf(stdout, "installable: %v\n", d.Installable)
	fmt.Fprintf(stdout, "icon:        %s\n", d.IconURL)
	if d.Manifest != "" {
		fmt.Fprintf(stdout, "manifest:    omastore.toml\n")
	}
	for _, s := range d.Screenshots {
		fmt.Fprintf(stdout, "screenshot:  %s\n", s)
	}
	for _, as := range d.Assets {
		sum := "no checksum"
		switch {
		case as.Digest != "":
			sum = as.Digest
		case as.ChecksumURL != "":
			sum = "checksum: " + as.ChecksumURL
		}
		fmt.Fprintf(stdout, "asset:       %s [%s %s] %s\n", as.Name, as.Format, orDash(as.Arch), sum)
	}
	if d.Install != nil {
		fmt.Fprintf(stdout, "installed:   %s on %s\n", d.Install.Version, d.Install.InstalledAt.Local().Format(time.DateTime))
		fmt.Fprintf(stdout, "executable:  %s\n", d.Install.ExecPath)
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// isTerminal reports whether w is a terminal (to choose between \r and new lines).
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// progressPrinter shows an installation's progress: on a single line in
// terminals, or one line per stage when the output is redirected.
func progressPrinter(w io.Writer, name string) func(install.Progress) {
	tty := isTerminal(w)
	last := ""
	return func(p install.Progress) {
		if !tty {
			if p.Stage != last {
				fmt.Fprintf(w, "%s: %s\n", name, p.Stage)
				last = p.Stage
			}
			return
		}
		line := p.Stage
		if p.Stage == install.StageDownload && p.Total > 0 {
			line = fmt.Sprintf("download %3d%% (%s)", p.Done*100/p.Total, humanBytes(p.Total))
		}
		if line != last {
			fmt.Fprintf(w, "\r%s: %-40s", name, line)
			last = line
		}
	}
}

// endLine ends the progress line in terminals.
func endLine(w io.Writer) {
	if isTerminal(w) {
		fmt.Fprintln(w)
	}
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func needRepos(name string, args []string, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "usage: omastore %s owner/repo...\n", name)
		return errUsage
	}
	return nil
}

func cmdInstall(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) error {
	if err := needRepos("install", args, stderr); err != nil {
		return err
	}
	var errs []error
	for _, name := range args {
		inst, err := a.Installer.Install(ctx, name, progressPrinter(stderr, name))
		endLine(stderr)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		fmt.Fprintf(stdout, "%s %s installed: %s\n", name, inst.Version, inst.ExecPath)
	}
	return errors.Join(errs...)
}

func cmdUninstall(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) error {
	if err := needRepos("uninstall", args, stderr); err != nil {
		return err
	}
	var errs []error
	for _, name := range args {
		if err := a.Installer.Uninstall(ctx, name); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		fmt.Fprintf(stdout, "%s removed\n", name)
	}
	return errors.Join(errs...)
}

// newNotifier creates the desktop notifier (replaced in tests).
var newNotifier = func() notify.Notifier { return notify.DBus{AppName: "OmaStore", Icon: "omastore"} }

func cmdUpdate(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) error {
	fs := newFlags("update", stderr)
	check := fs.Bool("check", false, "only list the available updates, without installing")
	notifyFlag := fs.Bool("notify", false, "with --check: desktop notification if there are new updates")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *notifyFlag && !*check {
		fmt.Fprintln(stderr, "--notify only works with --check")
		return errUsage
	}
	if *check {
		return checkUpdates(ctx, a, *notifyFlag, stdout)
	}
	names := fs.Args()
	if len(names) == 0 {
		insts, err := a.Store.ListInstalls(ctx)
		if err != nil {
			return err
		}
		for _, in := range insts {
			names = append(names, in.FullName)
		}
		if len(names) == 0 {
			fmt.Fprintln(stdout, "no apps installed")
			return nil
		}
	}
	var errs []error
	for _, name := range names {
		inst, err := a.Installer.Update(ctx, name, progressPrinter(stderr, name))
		switch {
		case errors.Is(err, install.ErrUpToDate):
			fmt.Fprintf(stdout, "%s is already at %s\n", name, inst.Version)
		case err != nil:
			endLine(stderr)
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		default:
			endLine(stderr)
			fmt.Fprintf(stdout, "%s updated to %s\n", name, inst.Version)
		}
	}
	return errors.Join(errs...)
}

// checkUpdates lists the installed apps with a newer version in the catalog and,
// with notify, shows a desktop notification (once per set of versions).
func checkUpdates(ctx context.Context, a *app.App, notifyUser bool, stdout io.Writer) error {
	items, err := a.Store.ListApps(ctx, store.Filter{InstalledOnly: true, All: true})
	if err != nil {
		return err
	}
	var ups []notify.Update
	for _, it := range items {
		if it.LatestTag != "" && it.InstalledVersion != it.LatestTag {
			ups = append(ups, notify.Update{Repo: it.FullName, Name: it.Name, From: it.InstalledVersion, To: it.LatestTag})
		}
	}
	if len(ups) == 0 {
		fmt.Fprintln(stdout, "everything is up to date")
		if notifyUser {
			// Reset the state: the next update will be announced again.
			os.Remove(filepath.Join(a.Paths.StateDir, "notified-updates"))
		}
		return nil
	}
	for _, u := range ups {
		fmt.Fprintf(stdout, "%s: %s → %s\n", u.Repo, u.From, u.To)
	}
	if !notifyUser {
		return nil
	}
	key, summary, body := notify.UpdatesMessage(ups)
	sent, err := notify.Once(ctx, newNotifier(), filepath.Join(a.Paths.StateDir, "notified-updates"), key, summary, body)
	if err != nil {
		return err
	}
	if !sent {
		fmt.Fprintln(stdout, "(already notified)")
	}
	return nil
}
