// Comando omastore: CLI de depuração do backend do OmaStore. Usa os mesmos
// serviços do daemon, sem precisar da GUI.
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

const usage = `uso: omastore [-v] <comando> [opções]

comandos:
  index [--force] [--prune] [--max N] [--no-batch] [--manifest arquivo] [owner/repo...]
                                    indexa o catálogo (ou só os repos dados)
  list [--category C] [--query Q] [--installed] [--all] [--json]
  categories                        lista as categorias
  show [--json] owner/repo          detalhes de um app
  similar [--json] [--limit N] owner/repo
                                    apps parecidos (busca: list --query)
  lint-manifest [dir|arquivo]       valida um omastore.toml (padrão: diretório atual)
  install owner/repo...             instala a última release
  uninstall owner/repo...           remove um app instalado
  update [owner/repo...]            atualiza os apps dados (ou todos os instalados)
  update --check [--notify]         lista atualizações; --notify avisa pela área de trabalho

variáveis: GITHUB_TOKEN (ou gh auth token), OMASTORE_LOG=debug|info|warn|error
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// errUsage sinaliza erro de uso (código de saída 2).
var errUsage = errors.New("uso inválido")

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
	// Comandos que não precisam do banco nem da rede.
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
		fmt.Fprintf(stderr, "comando desconhecido: %q\n\n%s", cmd, usage)
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
	force := fs.Bool("force", false, "reprocessa tudo, ignorando o cache")
	prune := fs.Bool("prune", false, "remove do catálogo repos não mais encontrados")
	maxSearch := fs.Int("max", 0, "máximo de resultados da busca por topic (default 300)")
	noBatch := fs.Bool("no-batch", false, "não usar a consulta em lote (GraphQL), só a API REST")
	manifestFile := fs.String("manifest", "", "usa este omastore.toml local no lugar do publicado (exige um único owner/repo)")
	if err := parse(fs, args); err != nil {
		return err
	}
	a.Indexer.Prune = *prune
	a.Indexer.MaxSearch = *maxSearch
	a.Indexer.NoBatch = *noBatch
	var overrides map[string]string
	if *manifestFile != "" {
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "--manifest exige exatamente um owner/repo")
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
	fmt.Fprintf(stdout, "atualizados: %d, só stats: %d, inalterados: %d, removidos: %d, sem manifesto de app: %d, ignorados: %d, falhas: %d (%s, %d requisições)\n",
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
	fs.StringVar(&f.Category, "category", "", "filtra por categoria")
	fs.StringVar(&f.Query, "query", "", "busca por texto")
	fs.BoolVar(&f.InstalledOnly, "installed", false, "só instalados")
	fs.BoolVar(&f.All, "all", false, "inclui apps sem binário instalável")
	asJSON := fs.Bool("json", false, "saída em JSON")
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
	fmt.Fprintln(tw, "REPO\tNOME\tCATEGORIA\t★\tVERSÃO\tINSTALADO\tRESUMO")
	for _, it := range items {
		inst := ""
		if it.InstalledVersion != "" {
			inst = it.InstalledVersion
			if it.InstalledVersion != it.LatestTag {
				inst += " (desatualizado)"
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
	asJSON := fs.Bool("json", false, "saída em JSON")
	limit := fs.Int("limit", 8, "máximo de resultados")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "uso: omastore similar [--json] [--limit N] owner/repo")
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
		fmt.Fprintln(stderr, "uso: omastore lint-manifest [dir|arquivo]")
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
		return fmt.Errorf("%w: 1 erro(s)", errLint)
	}
	// Com o diretório do repositório, confere se os arquivos existem.
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
				problems = append(problems, manifest.Problem{Field: p, Message: "arquivo não existe no repositório"})
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
		return fmt.Errorf("%w: %d erro(s)", errLint, errs)
	}
	if !m.IsApp() {
		return nil // o aviso sobre kind já foi impresso entre os problemas
	}
	if m.Empty() {
		fmt.Fprintf(stdout, "ok: %s (empty: the repository joins the catalog and the rest is inferred)\n", file)
		return nil
	}
	fmt.Fprintf(stdout, "ok: %s\n", file)
	if m.Name != "" {
		fmt.Fprintf(stdout, "  nome:        %s\n", m.Name)
	}
	if c := m.MainCategory(); c != "" {
		fmt.Fprintf(stdout, "  categoria:   %s\n", c)
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
	asJSON := fs.Bool("json", false, "saída em JSON")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "uso: omastore show [--json] owner/repo")
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
	fmt.Fprintf(stdout, "categoria:  %s\n", d.Category)
	fmt.Fprintf(stdout, "stars:      %d\n", d.Repo.Stars)
	fmt.Fprintf(stdout, "licença:    %s\n", d.Repo.License)
	fmt.Fprintf(stdout, "topics:     %s\n", strings.Join(d.Repo.Topics, ", "))
	fmt.Fprintf(stdout, "release:    %s\n", d.Repo.LatestTag)
	fmt.Fprintf(stdout, "instalável: %v\n", d.Installable)
	fmt.Fprintf(stdout, "ícone:      %s\n", d.IconURL)
	if d.Manifest != "" {
		fmt.Fprintf(stdout, "manifesto:  omastore.toml\n")
	}
	for _, s := range d.Screenshots {
		fmt.Fprintf(stdout, "screenshot: %s\n", s)
	}
	for _, as := range d.Assets {
		sum := "sem checksum"
		switch {
		case as.Digest != "":
			sum = as.Digest
		case as.ChecksumURL != "":
			sum = "checksum: " + as.ChecksumURL
		}
		fmt.Fprintf(stdout, "asset:      %s [%s %s] %s\n", as.Name, as.Format, orDash(as.Arch), sum)
	}
	if d.Install != nil {
		fmt.Fprintf(stdout, "instalado:  %s em %s\n", d.Install.Version, d.Install.InstalledAt.Local().Format(time.DateTime))
		fmt.Fprintf(stdout, "executável: %s\n", d.Install.ExecPath)
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// isTerminal diz se w é um terminal (para decidir entre \r e linhas novas).
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// progressPrinter mostra o andamento de uma instalação: numa linha só em
// terminais, ou uma linha por etapa quando a saída é redirecionada.
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

// endLine termina a linha de progresso em terminais.
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
		fmt.Fprintf(stderr, "uso: omastore %s owner/repo...\n", name)
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
		fmt.Fprintf(stdout, "%s %s instalado: %s\n", name, inst.Version, inst.ExecPath)
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
		fmt.Fprintf(stdout, "%s removido\n", name)
	}
	return errors.Join(errs...)
}

// newNotifier cria o notificador de desktop (substituído nos testes).
var newNotifier = func() notify.Notifier { return notify.DBus{AppName: "OmaStore", Icon: "omastore"} }

func cmdUpdate(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) error {
	fs := newFlags("update", stderr)
	check := fs.Bool("check", false, "só lista as atualizações disponíveis, sem instalar")
	notifyFlag := fs.Bool("notify", false, "com --check: notificação desktop se houver atualizações novas")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *notifyFlag && !*check {
		fmt.Fprintln(stderr, "--notify só vale com --check")
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
			fmt.Fprintln(stdout, "nenhum app instalado")
			return nil
		}
	}
	var errs []error
	for _, name := range names {
		inst, err := a.Installer.Update(ctx, name, progressPrinter(stderr, name))
		switch {
		case errors.Is(err, install.ErrUpToDate):
			fmt.Fprintf(stdout, "%s já está em %s\n", name, inst.Version)
		case err != nil:
			endLine(stderr)
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		default:
			endLine(stderr)
			fmt.Fprintf(stdout, "%s atualizado para %s\n", name, inst.Version)
		}
	}
	return errors.Join(errs...)
}

// checkUpdates lista os apps instalados com versão nova no catálogo e, com
// notify, avisa pela área de trabalho (uma vez por conjunto de versões).
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
		fmt.Fprintln(stdout, "tudo atualizado")
		if notifyUser {
			// Zera o estado: a próxima atualização volta a ser avisada.
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
		fmt.Fprintln(stdout, "(já notificado)")
	}
	return nil
}
