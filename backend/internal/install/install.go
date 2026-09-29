// Package install baixa, verifica e instala o binário da última release de
// um app no $HOME do usuário, gerando o .desktop e o ícone. Nada é executado
// durante a instalação, nada é escrito fora do $HOME e não há sudo.
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

// Erros retornados pelo instalador.
var (
	ErrNotInstallable = errors.New("app sem binário para esta arquitetura")
	ErrNotInstalled   = errors.New("app não está instalado")
	ErrUpToDate       = errors.New("app já está na versão mais recente")
	ErrConflict       = errors.New("arquivo já existe e não pertence ao OmaStore")
	ErrOutsideHome    = errors.New("caminho fora do $HOME")
)

// Installer instala e remove apps.
type Installer struct {
	Store *store.Store
	Paths xdg.Paths
	HTTP  *http.Client // default: cliente com timeout de conexão
	// GOARCH decide o asset (default runtime.GOARCH).
	GOARCH string
	// Hooks roda update-desktop-database e gtk-update-icon-cache do sistema.
	Hooks bool
	Log   *slog.Logger

	locks sync.Map // full_name → *sync.Mutex
}

// Progress é o andamento de uma instalação.
type Progress struct {
	Stage string // download, verify, extract, integrate, done
	Done  int64
	Total int64
}

// Etapas de Progress.Stage.
const (
	StageDownload  = "download"
	StageVerify    = "verify"
	StageExtract   = "extract"
	StageIntegrate = "integrate"
	StageDone      = "done"
)

// New valida que todos os destinos estão dentro do $HOME.
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

// within diz se p está dentro de root (ou é root).
func within(root, p string) bool {
	if root == "" || !filepath.IsAbs(p) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// reName valida owner e repo com as regras de nome do GitHub, para que não
// possam virar componentes de caminho perigosos.
var reName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

func splitName(fullName string) (owner, repo string, err error) {
	owner, repo, ok := strings.Cut(fullName, "/")
	if !ok || !reName.MatchString(owner) || !reName.MatchString(repo) || repo == ".." || strings.Contains(repo, "..") {
		return "", "", fmt.Errorf("nome de repositório inválido: %q", fullName)
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

// appID é o identificador usado em nomes de arquivos (.desktop, ícone).
func appID(owner, repo string) string {
	return strings.ToLower("omastore-" + owner + "-" + repo)
}

// SelectAsset escolhe o asset para goarch: o declarado no manifesto (se
// houver e existir na release), senão arquitetura exata antes de genérica e
// depois o formato preferido.
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

// Install instala (ou reinstala/atualiza) a última release de fullName.
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

	// 1. Download para um diretório temporário.
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

	// 2. Verificação de checksum, quando a release publica um.
	report(Progress{Stage: StageVerify})
	want, err := in.expected(ctx, asset.Digest, asset.ChecksumURL, asset.Name)
	if err != nil {
		return nil, err
	}
	if want == "" {
		in.log().Warn("release sem checksum; instalando sem verificação", "repo", fullName, "asset", asset.Name)
	} else if err := verify(want, sum256, sum512); err != nil {
		return nil, fmt.Errorf("%s: %w", asset.Name, err)
	}

	// 3. Extração numa área de staging dentro do diretório do app.
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
		return nil, fmt.Errorf("extrair %s: %w", asset.Name, err)
	}
	execAbs, err := in.findExec(staging, repo, asset.Format, asset.Tag, m)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", asset.Name, err)
	}
	execRel, _ := filepath.Rel(staging, execAbs)
	if err := os.Chmod(execAbs, 0o755); err != nil {
		return nil, err
	}

	// 4. Integração: diretório da versão, link, ícone e .desktop. Tudo passa
	// pelo tx para poder ser desfeito.
	report(Progress{Stage: StageIntegrate})
	t := &tx{}
	inst, err := in.integrate(ctx, t, d, m, prev, owner, repo, staging, execRel)
	if err != nil {
		if rbErr := t.rollback(); rbErr != nil {
			in.log().Error("rollback incompleto", "repo", fullName, "err", rbErr)
		}
		return nil, err
	}
	if err := t.commit(); err != nil {
		in.log().Warn("não foi possível remover backups", "repo", fullName, "err", err)
	}

	// Remove o que a instalação anterior criou e a nova não usa mais (ex.:
	// o diretório da versão antiga).
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

	// Lançador em ~/.local/bin com o nome do executável (ver launcherScript).
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

	// Ícone: do pacote extraído ou baixado do repositório. Falhar aqui não
	// impede a instalação.
	id := appID(owner, repo)
	iconName := "application-x-executable"
	if iconPath, err := in.installIcon(ctx, t, id, versionDir, repo, d.IconURL); err != nil {
		in.log().Warn("ícone não instalado", "repo", d.FullName, "err", err)
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

// reservedCommands nunca podem ser criados em ~/.local/bin por um app.
var reservedCommands = map[string]bool{"omastore": true, "omastored": true, "omastore-gui": true}

// systemBinDirs são consultados para não esconder comandos do sistema:
// ~/.local/bin costuma vir antes deles no PATH.
var systemBinDirs = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin", "/usr/local/bin"}

// checkCommandName recusa lançadores que esconderiam comandos do sistema
// (ex.: um app cujo executável se chame "sudo" ou "ls") ou do OmaStore.
func (in *Installer) checkCommandName(cmd string) error {
	if reservedCommands[cmd] {
		return fmt.Errorf("%w: o comando %q é reservado ao OmaStore", ErrConflict, cmd)
	}
	for _, d := range systemBinDirs {
		if _, err := os.Lstat(filepath.Join(d, cmd)); err == nil {
			return fmt.Errorf("%w: o comando %q já existe em %s e seria encoberto", ErrConflict, cmd, d)
		}
	}
	return nil
}

// findExec usa o executável declarado no manifesto para a arquitetura, se
// existir dentro do pacote; senão, a heurística de FindExecutable.
func (in *Installer) findExec(staging, repo, format, tag string, m *manifest.Manifest) (string, error) {
	t, ok := m.Target(in.goarch())
	if ok && t.Exec != "" && format != index.FormatBinary && format != index.FormatAppImage {
		p, err := declaredExec(staging, manifest.Expand(t.Exec, tag))
		if err == nil {
			return p, nil
		}
		in.log().Warn("executável do manifesto inválido; usando heurística", "exec", t.Exec, "err", err)
	}
	return FindExecutable(staging, repo)
}

// declaredExec valida um caminho do manifesto: precisa ser um arquivo
// regular dentro de dir, sem sair dele nem por symlink.
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
		return "", fmt.Errorf("%w: %s aponta para fora do pacote", ErrUnsafePath, rel)
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("%s não é um arquivo", rel)
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

// testHookBeforeSave permite aos testes simular uma falha na última etapa.
var testHookBeforeSave func() error

// checkOwned recusa sobrescrever um arquivo que não seja nosso: só aceita se
// não existir, se estiver registrado na instalação anterior ou, para
// lançadores, se apontar para dentro de ownDir.
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

// installIcon procura o ícone no conteúdo extraído (ex.: usr/share/icons de
// um pacote) e, na falta, baixa iconURL. Retorna "" se não houver ícone.
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
		return nil, fmt.Errorf("%s grande demais", p)
	}
	return os.ReadFile(p)
}

// Update instala a nova versão se houver; senão retorna ErrUpToDate.
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

// Uninstall remove apenas os caminhos registrados no banco.
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
		os.Remove(filepath.Join(in.Paths.AppsDir, owner+"__"+repo)) // só se vazio
	}
	if err := in.Store.DeleteInstall(ctx, inst.FullName); err != nil {
		return err
	}
	in.runHooks(ctx)
	return nil
}

// removeRegistered remove um caminho registrado, com travas: precisa estar
// num dos diretórios gerenciados; em ~/.local/bin só remove lançadores nossos; só
// apaga diretórios inteiros dentro de apps/.
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
			in.log().Warn("não é mais um lançador do OmaStore; mantido", "path", p)
			return
		}
		err = os.Remove(p)
	case (within(in.Paths.Applications, p) || within(in.Paths.Icons, p)) && st.Mode().IsRegular() &&
		strings.HasPrefix(path.Base(filepath.ToSlash(p)), "omastore-"):
		err = os.Remove(p)
	default:
		in.log().Warn("caminho registrado fora dos diretórios gerenciados; ignorado", "path", p)
		return
	}
	if err != nil {
		in.log().Warn("falha ao remover", "path", p, "err", err)
	}
}

// Ferramentas do sistema usadas nos hooks. Caminhos absolutos: nunca
// resolvemos pelo PATH, que inclui ~/.local/bin (onde ficam apps baixados).
var (
	updateDesktopDB = "/usr/bin/update-desktop-database"
	updateIconCache = "/usr/bin/gtk-update-icon-cache"
	hookTimeout     = 20 * time.Second
	errHookSkipped  = errors.New("ferramenta ausente")
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
		in.log().Debug("update-desktop-database falhou", "err", err)
	}
	if err := runCommand(ctx, updateIconCache, "-q", "-t", "-f", in.Paths.Icons); err != nil && !errors.Is(err, errHookSkipped) {
		in.log().Debug("gtk-update-icon-cache falhou", "err", err)
	}
}
