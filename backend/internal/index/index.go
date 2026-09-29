// Package index descobre repositórios de apps do Omarchy no GitHub, extrai
// seus dados de exibição e os grava no store, sem reprocessar o que não mudou.
package index

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/github"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/gitrepo"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/manifest"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
)

// Version é a versão da lógica de extração e classificação. Incremente-a ao
// mudar regras que afetam o que é gravado (assets, categorias, README...):
// repositórios gravados com versão menor são reprocessados mesmo sem mudança
// no GitHub.
const Version = 5

// GitHub é o subconjunto do cliente usado pelo indexador.
type GitHub interface {
	SearchByTopic(ctx context.Context, topic string, max int) ([]string, error)
	GetRepo(ctx context.Context, fullName, etag string) (*github.Repo, string, bool, error)
	HeadSHA(ctx context.Context, fullName, ref, lastSHA string) (string, error)
	LatestRelease(ctx context.Context, fullName string) (*github.Release, error)
	Readme(ctx context.Context, fullName string) (string, string, error)
	Tree(ctx context.Context, fullName, sha string) ([]string, bool, error)
	File(ctx context.Context, fullName, path, ref string, maxBytes int) (string, bool, error)
}

// ManifestSearcher é implementado por clientes que encontram repositórios
// pelo arquivo omastore.toml (busca de código; exige token).
type ManifestSearcher interface {
	SearchManifests(ctx context.Context, max int) ([]string, error)
}

// Batcher é implementado por clientes que buscam o estado de vários
// repositórios de uma vez (GraphQL). Se Snapshots retornar
// github.ErrNoToken, o indexador usa a API REST.
type Batcher interface {
	Snapshots(ctx context.Context, names []string) (map[string]*github.Snapshot, error)
}

// Indexer executa o pipeline de indexação.
type Indexer struct {
	GH     GitHub
	Store  *store.Store
	Repos  *gitrepo.Cache // opcional: usado quando a Trees API trunca
	Topics []string       // default: ["omarchy"]
	Seeds  []string       // default: Seeds()
	// MaxSearch limita os resultados por topic (default 300).
	MaxSearch int
	Workers   int // default 4
	// Prune remove do catálogo repositórios que não foram mais descobertos.
	Prune bool
	// NoBatch desliga a consulta em lote (GraphQL) e usa só a API REST.
	NoBatch bool
	Log     *slog.Logger
	Now     func() time.Time
	// GOARCH decide o que conta como instalável (default runtime.GOARCH).
	GOARCH string
}

// Options controla uma execução.
type Options struct {
	// Force reprocessa todos os repositórios, ignorando o cache.
	Force bool
	// Only restringe a execução a estes repositórios (sem descoberta).
	Only []string
	// ManifestOverride usa este conteúdo como omastore.toml de um repositório
	// (owner/repo → conteúdo), no lugar do arquivo publicado. Serve para o
	// autor ver o resultado antes de publicar o manifesto.
	ManifestOverride map[string]string
	// Progress, se definido, recebe o andamento após cada repositório. As
	// chamadas são serializadas; o callback não deve bloquear.
	Progress func(Progress)
}

// Progress é o andamento de uma indexação.
type Progress struct {
	Total   int
	Done    int
	Current string
	Stats
}

// Stats resume o resultado.
type Stats struct {
	Updated   int // reprocessados por completo
	Refreshed int // só stars/score atualizados
	Unchanged int // nada mudou
	Removed   int // saíram do catálogo
	Skipped   int // não existem ou estão arquivados, e não estavam no catálogo
	// NotApps não têm omastore.toml válido ou não declaram kind = "app"
	// (plugins, temas), e não estavam no catálogo.
	NotApps int
	Failed  int
}

// Resultado do processamento de um repositório.
type outcome int

const (
	outUnchanged outcome = iota
	outRefreshed
	outUpdated
	outRemoved
	outSkipped
	outNotApp
)

func (ix *Indexer) now() time.Time {
	if ix.Now != nil {
		return ix.Now()
	}
	return time.Now()
}

func (ix *Indexer) log() *slog.Logger {
	if ix.Log != nil {
		return ix.Log
	}
	return slog.Default()
}

func (ix *Indexer) goarch() string {
	if ix.GOARCH != "" {
		return ix.GOARCH
	}
	return runtime.GOARCH
}

// Discover retorna a união, sem duplicatas, das buscas por topic e das sementes.
func (ix *Indexer) Discover(ctx context.Context) ([]string, error) {
	topics := ix.Topics
	if len(topics) == 0 {
		topics = []string{"omarchy"}
	}
	seeds := ix.Seeds
	if seeds == nil {
		seeds = Seeds()
	}
	max := ix.MaxSearch
	if max <= 0 {
		max = 300
	}
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		k := strings.ToLower(n)
		if !seen[k] {
			seen[k] = true
			out = append(out, n)
		}
	}
	for _, s := range seeds {
		if list, ok := strings.CutPrefix(s, "list:"); ok {
			repos, err := ix.fromList(ctx, list)
			if err != nil {
				var rl *github.RateLimitError
				if errors.As(err, &rl) {
					return nil, err
				}
				ix.log().Warn("lista curada ilegível", "lista", list, "err", err)
			}
			for _, r := range repos {
				add(r)
			}
			continue
		}
		add(s)
	}
	// Repositórios com omastore.toml na raiz, mesmo sem o topic.
	if ms, ok := ix.GH.(ManifestSearcher); ok {
		names, err := ms.SearchManifests(ctx, max)
		var rl *github.RateLimitError
		switch {
		case errors.As(err, &rl):
			return nil, err
		case errors.Is(err, github.ErrNoToken):
		case err != nil:
			ix.log().Warn("busca por manifestos falhou", "err", err)
		}
		for _, n := range names {
			add(n)
		}
	}
	for _, t := range topics {
		names, err := ix.GH.SearchByTopic(ctx, t, max)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			add(n)
		}
	}
	return out, nil
}

// maxFromList limita quantos repositórios uma lista curada pode trazer.
const maxFromList = 300

var reRepoLink = regexp.MustCompile(`(?i)https?://github\.com/([A-Za-z0-9][A-Za-z0-9-]{0,38})/([A-Za-z0-9._-]{1,100})`)

// reservedOwners são caminhos do github.com que não são usuários.
var reservedOwners = map[string]bool{
	"topics": true, "orgs": true, "sponsors": true, "marketplace": true, "features": true,
	"settings": true, "apps": true, "collections": true, "about": true, "login": true,
}

// fromList extrai os repositórios citados no README de uma lista curada
// (ex.: uma lista "awesome"), na ordem em que aparecem.
func (ix *Indexer) fromList(ctx context.Context, list string) ([]string, error) {
	readme, _, err := ix.GH.Readme(ctx, list)
	if err != nil {
		return nil, err
	}
	return RepoLinks(readme, list, maxFromList), nil
}

// RepoLinks extrai links github.com/owner/repo de um texto, sem duplicatas
// e sem o próprio repositório da lista.
func RepoLinks(text, self string, max int) []string {
	seen := map[string]bool{strings.ToLower(self): true}
	var out []string
	for _, m := range reRepoLink.FindAllStringSubmatch(text, -1) {
		owner, repo := m[1], strings.TrimSuffix(m[2], ".git")
		if reservedOwners[strings.ToLower(owner)] || repo == "" || strings.HasPrefix(repo, ".") {
			continue
		}
		name := owner + "/" + repo
		if k := strings.ToLower(name); !seen[k] {
			seen[k] = true
			out = append(out, name)
			if len(out) == max {
				break
			}
		}
	}
	return out
}

// Run executa a indexação completa.
func (ix *Indexer) Run(ctx context.Context, opts Options) (Stats, error) {
	names := opts.Only
	discovered := false
	if len(names) == 0 {
		var err error
		names, err = ix.Discover(ctx)
		if err != nil {
			return Stats{}, fmt.Errorf("descoberta: %w", err)
		}
		discovered = true
	}

	ix.log().Debug("descoberta concluída", "repos", len(names))

	// Estado em lote: uma requisição GraphQL a cada 50 repositórios, em vez
	// de 3–4 requisições REST por repositório.
	var snaps map[string]*github.Snapshot
	if b, ok := ix.GH.(Batcher); ok && !ix.NoBatch {
		var err error
		snaps, err = b.Snapshots(ctx, names)
		ix.log().Debug("estado em lote obtido", "repos", len(snaps), "err", err)
		switch {
		case errors.Is(err, github.ErrNoToken):
			snaps = nil
		case err != nil:
			var rl *github.RateLimitError
			if errors.As(err, &rl) {
				return Stats{}, err
			}
			ix.log().Warn("consulta em lote falhou; usando a API REST", "err", err)
			snaps = nil
		}
	}

	workers := ix.Workers
	if workers <= 0 {
		workers = 4
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	var (
		mu    sync.Mutex
		prog  = Progress{Total: len(names)}
		fatal error
		jobs  = make(chan string)
		wg    sync.WaitGroup
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				snap, batched := snaps[name]
				// Com manifesto local, reprocessa: o publicado não mudou, mas o que vale mudou.
				out, err := ix.process(ctx, name, opts.Force || len(opts.ManifestOverride) > 0, snap, batched, opts.ManifestOverride)
				mu.Lock()
				prog.Done++
				prog.Current = name
				switch {
				case err != nil:
					prog.Failed++
					var rl *github.RateLimitError
					if errors.As(err, &rl) || errors.Is(err, context.Canceled) {
						if fatal == nil {
							fatal = err
							cancel(err)
						}
					} else {
						ix.log().Warn("falha ao indexar", "repo", name, "err", err)
					}
				case out == outUpdated:
					prog.Updated++
				case out == outRefreshed:
					prog.Refreshed++
				case out == outRemoved:
					prog.Removed++
				case out == outSkipped:
					prog.Skipped++
				case out == outNotApp:
					prog.NotApps++
				default:
					prog.Unchanged++
				}
				// Chamado sob o lock: callbacks em série e em ordem crescente de Done.
				if opts.Progress != nil {
					opts.Progress(prog)
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, n := range names {
		select {
		case jobs <- n:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()

	if fatal != nil {
		return prog.Stats, fatal
	}
	if err := ctx.Err(); err != nil {
		return prog.Stats, err
	}
	if ix.Prune && discovered {
		n, err := ix.prune(ctx, names)
		prog.Removed += n
		if err != nil {
			return prog.Stats, err
		}
	}
	return prog.Stats, nil
}

// prune remove do catálogo o que não foi descoberto nesta execução.
func (ix *Indexer) prune(ctx context.Context, found []string) (int, error) {
	keep := map[string]bool{}
	for _, n := range found {
		keep[strings.ToLower(n)] = true
	}
	all, err := ix.Store.RepoNames(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, name := range all {
		if keep[strings.ToLower(name)] {
			continue
		}
		if err := ix.Store.RemoveRepo(ctx, name); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// process aplica o pipeline a um repositório. snap, quando batched, é o
// estado obtido em lote pelo GraphQL (nil = repositório não existe); sem
// lote, o estado vem da API REST com requisições condicionais.
func (ix *Indexer) process(ctx context.Context, name string, force bool, snap *github.Snapshot, batched bool,
	overrides map[string]string) (outcome, error) {
	prev, err := ix.Store.RepoState(ctx, name)
	known := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return 0, err
	}
	now := ix.now()
	removed := func() (outcome, error) {
		if known {
			return outRemoved, ix.Store.RemoveRepo(ctx, name)
		}
		return outSkipped, nil
	}

	// Regras do indexador mudaram desde a última vez: reprocessar tudo.
	if known && prev.IndexVersion < Version {
		force = true
	}

	var (
		repo    *github.Repo
		newETag = prev.ETag
	)
	if batched {
		if snap == nil {
			return removed()
		}
		repo = &snap.Repo
	} else {
		etag := prev.ETag
		if force {
			etag = ""
		}
		var notModified bool
		repo, newETag, notModified, err = ix.GH.GetRepo(ctx, name, etag)
		if github.IsNotFound(err) {
			return removed()
		}
		if err != nil {
			return 0, err
		}
		if notModified {
			// Metadados iguais (logo pushed_at e HEAD também); uma release pode
			// ser publicada sobre uma tag existente, então conferimos a tag.
			rel, err := ix.GH.LatestRelease(ctx, name)
			if err != nil {
				return 0, err
			}
			if tagOf(rel) == prev.LatestTag {
				return outUnchanged, ix.Store.TouchRepo(ctx, name, now)
			}
			repo, newETag, _, err = ix.GH.GetRepo(ctx, name, "")
			if err != nil {
				return 0, err
			}
		}
	}

	if repo.Archived {
		return removed()
	}
	// A API pode devolver outro nome (repo renomeado/transferido).
	if repo.FullName != "" && repo.FullName != name {
		if known {
			if err := ix.Store.RemoveRepo(ctx, name); err != nil {
				return 0, err
			}
		}
		name = repo.FullName
		prev, err = ix.Store.RepoState(ctx, name)
		known = err == nil
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return 0, err
		}
		// O estado recarregado pode ser de uma versão antiga do indexador
		// (ex.: nome descoberto com outra capitalização).
		if known && prev.IndexVersion < Version {
			force = true
		}
	}

	var (
		sha string
		rel *github.Release
	)
	if batched {
		sha, rel = snap.HeadSHA, snap.Release
	} else {
		lastSHA := prev.HeadSHA
		if force {
			lastSHA = ""
		}
		if sha, err = ix.GH.HeadSHA(ctx, name, repo.DefaultBranch, lastSHA); err != nil {
			return 0, err
		}
		if rel, err = ix.GH.LatestRelease(ctx, name); err != nil {
			return 0, err
		}
	}
	score := Score(repo.Stars, repo.PushedAt, now)

	// Verificação de cache: se pushed_at, HEAD e tag não mudaram, não
	// reprocessar. Só os dados voláteis (stars etc.) são atualizados.
	if known && !force && prev.PushedAt.Equal(repo.PushedAt) && prev.HeadSHA == sha && prev.LatestTag == tagOf(rel) {
		if batched && prev.Stars == repo.Stars && prev.Description == repo.Description {
			return outUnchanged, ix.Store.TouchRepo(ctx, name, now)
		}
		return outRefreshed, ix.Store.UpdateStats(ctx, name, repo.Stars, repo.Description, repo.Topics, newETag, score, now)
	}

	// Só entram no catálogo repositórios com omastore.toml que declaram um
	// app (nada de plugins e temas). A presença do arquivo é o opt-in do autor.
	m, why, err := ix.manifestFor(ctx, name, sha, snap, batched, overrides)
	if err != nil {
		return 0, err
	}
	if m == nil {
		ix.log().Debug("fora do catálogo", "repo", name, "motivo", why)
		if known {
			return outRemoved, ix.Store.RemoveRepo(ctx, name)
		}
		return outNotApp, nil
	}

	app, assets, err := ix.extract(ctx, repo, sha, rel, m)
	if err != nil {
		return 0, err
	}
	app.Score = score
	r := store.Repo{
		FullName:      name,
		Description:   repo.Description,
		Stars:         repo.Stars,
		Topics:        repo.Topics,
		License:       repo.License,
		HTMLURL:       repo.HTMLURL,
		DefaultBranch: repo.DefaultBranch,
		PushedAt:      repo.PushedAt,
		HeadSHA:       sha,
		LatestTag:     tagOf(rel),
		ETag:          newETag,
		IndexedAt:     now,
		IndexVersion:  Version,
	}
	if err := ix.Store.SaveIndexed(ctx, r, app, assets); err != nil {
		return 0, err
	}
	ix.log().Debug("indexado", "repo", name, "tag", r.LatestTag, "installable", app.Installable)
	return outUpdated, nil
}

func tagOf(rel *github.Release) string {
	if rel == nil {
		return ""
	}
	return rel.Tag
}

// maxScreenshots limita as screenshots por app.
const maxScreenshots = 8

// extract monta os dados de exibição e os assets de um repositório.
func (ix *Indexer) extract(ctx context.Context, repo *github.Repo, sha string, rel *github.Release, m *manifest.Manifest) (store.App, []store.Asset, error) {
	name := repo.FullName
	// Descrições do GitHub às vezes têm espaços/quebras nas pontas.
	app := store.App{FullName: name, Name: repo.Name, Summary: strings.Join(strings.Fields(repo.Description), " ")}
	app.Category = Category(repo.Topics, repo.Name, repo.Description)
	app.Manifest = m.Encode()
	if m.Name != "" {
		app.Name = m.Name
	}
	if m.Summary != "" {
		app.Summary = m.Summary
	}
	if c := m.MainCategory(); c != "" {
		app.Category = c
	}

	// Sem release não há o que instalar: grava só o básico, sem gastar
	// requisições com README e árvore. Quando surgir uma release, a tag muda
	// e o repositório é reprocessado por completo.
	if rel == nil {
		return app, nil, nil
	}

	readme, readmePath, err := ix.GH.Readme(ctx, name)
	if err != nil {
		return app, nil, err
	}
	urls := repoURLs{FullName: name, Ref: sha}
	if d := path.Dir(readmePath); d != "." && d != "" {
		urls.BaseDir = d
	}
	var readmeImages []string
	if readme != "" {
		app.Readme = RewriteReadme(readme, urls)
		readmeImages = ReadmeImages(app.Readme)
		if t := Title(readme); m.Name == "" && t != "" && normalize(t) == normalize(repo.Name) {
			app.Name = t // preserva a grafia do autor, ex.: "OmaPhoto"
		}
		if app.Summary == "" {
			app.Summary = Summary(readme, 200)
		}
	}

	files, err := ix.listFiles(ctx, name, sha)
	filesKnown := err == nil
	if err != nil {
		// Sem a lista de arquivos ainda dá para indexar com o README.
		ix.log().Warn("sem lista de arquivos", "repo", name, "err", err)
	}
	if m.Icon != "" && (!filesKnown || contains(files, m.Icon)) {
		app.IconURL = urls.Raw(m.Icon)
	} else if icon := gitrepo.FindIcon(files, repo.Name); icon != "" {
		app.IconURL = urls.Raw(icon)
	} else {
		for _, img := range readmeImages {
			l := strings.ToLower(path.Base(img))
			if strings.Contains(l, "icon") || strings.Contains(l, "logo") {
				app.IconURL = img
				break
			}
		}
	}

	seen := map[string]bool{app.IconURL: true}
	for _, img := range readmeImages {
		l := strings.ToLower(path.Base(img))
		if seen[img] || strings.Contains(l, "logo") || strings.Contains(l, "icon") || strings.Contains(l, "banner") {
			continue
		}
		seen[img] = true
		app.Screenshots = append(app.Screenshots, img)
	}
	for _, f := range gitrepo.FindScreenshots(files, 0) {
		u := urls.Raw(f)
		if !seen[u] {
			seen[u] = true
			app.Screenshots = append(app.Screenshots, u)
		}
	}
	if len(m.Screenshots) > 0 {
		app.Screenshots = nil
		for _, sh := range m.Screenshots {
			if strings.HasPrefix(sh, "https://") {
				app.Screenshots = append(app.Screenshots, sh)
			} else {
				app.Screenshots = append(app.Screenshots, urls.Raw(sh))
			}
		}
	}
	if len(app.Screenshots) > maxScreenshots {
		app.Screenshots = app.Screenshots[:maxScreenshots]
	}

	assets := releaseAssets(rel, m)
	for _, a := range assets {
		if (AssetInfo{Format: a.Format, Arch: a.Arch}).Installable(ix.goarch()) {
			app.Installable = true
			break
		}
	}
	return app, assets, nil
}

// manifestFor obtém e valida o omastore.toml da raiz no commit sha: do lote
// GraphQL quando disponível, senão pela API REST. Retorna nil (com o
// motivo) se não houver manifesto utilizável ou se ele não declarar um app.
func (ix *Indexer) manifestFor(ctx context.Context, name, sha string, snap *github.Snapshot, batched bool,
	overrides map[string]string) (*manifest.Manifest, string, error) {
	var data string
	if content, ok := overrideFor(overrides, name); ok {
		data = content
	} else if batched {
		if snap.Manifest == nil {
			return nil, "sem " + manifest.FileName, nil
		}
		data = *snap.Manifest
	} else {
		content, found, err := ix.GH.File(ctx, name, manifest.FileName, sha, manifest.MaxSize)
		if err != nil {
			var rl *github.RateLimitError
			if errors.As(err, &rl) {
				return nil, "", err
			}
			return nil, "manifesto ilegível: " + err.Error(), nil
		}
		if !found {
			return nil, "sem " + manifest.FileName, nil
		}
		data = content
	}
	m, problems, err := manifest.Parse([]byte(data), false)
	if err != nil {
		return nil, "manifesto inválido: " + err.Error(), nil
	}
	for _, p := range problems {
		ix.log().Info("problema no manifesto", "repo", name, "problema", p.String())
	}
	if !m.IsApp() {
		return nil, "kind = " + m.Kind + " (só apps são indexados)", nil
	}
	return m, "", nil
}

func overrideFor(overrides map[string]string, name string) (string, bool) {
	for k, v := range overrides {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return "", false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// listFiles lista os arquivos do repo pela Trees API; se a listagem vier
// truncada e houver cache de clones, clona e lista localmente.
func (ix *Indexer) listFiles(ctx context.Context, name, sha string) ([]string, error) {
	files, truncated, err := ix.GH.Tree(ctx, name, sha)
	if err == nil && !truncated {
		return files, nil
	}
	if ix.Repos == nil {
		return files, err
	}
	dir, _, cerr := ix.Repos.Sync(ctx, name)
	if cerr != nil {
		if err == nil {
			return files, nil // truncada, mas melhor que nada
		}
		return nil, errors.Join(err, cerr)
	}
	return gitrepo.ListFiles(dir)
}

// releaseAssets classifica os assets da release e guarda os instaláveis
// (de qualquer arquitetura suportada), ligados ao checksum correspondente.
// Assets que casam com o padrão do manifesto entram com a arquitetura
// declarada, mesmo que o nome não permitisse deduzi-la.
func releaseAssets(rel *github.Release, m *manifest.Manifest) []store.Asset {
	if rel == nil {
		return nil
	}
	declared := map[string]string{} // nome do asset → arquitetura do manifesto
	if m != nil {
		for arch, t := range m.Linux {
			if t.Asset == "" {
				continue
			}
			for _, a := range rel.Assets {
				if manifest.MatchAsset(t.Asset, rel.Tag, a.Name) && !ClassifyAsset(a.Name).Checksum {
					declared[a.Name] = arch
				}
			}
		}
	}
	sums := map[string]string{} // nome do asset → URL do seu .sha256
	general := ""               // checksums.txt / SHA256SUMS
	for _, a := range rel.Assets {
		if !ClassifyAsset(a.Name).Checksum {
			continue
		}
		lower := strings.ToLower(a.Name)
		if ext := path.Ext(lower); ext == ".sha256" || ext == ".sha256sum" || ext == ".sha512" || ext == ".sha512sum" {
			sums[strings.TrimSuffix(a.Name, path.Ext(a.Name))] = a.URL
		} else if general == "" || strings.Contains(lower, "sha256") {
			general = a.URL
		}
	}
	var out []store.Asset
	for _, a := range rel.Assets {
		info := ClassifyAsset(a.Name)
		if arch, ok := declared[a.Name]; ok {
			info = AssetInfo{Format: FormatOf(a.Name), Arch: arch}
		}
		if info.Format == "" {
			continue
		}
		sum := sums[a.Name]
		if sum == "" {
			sum = general
		}
		out = append(out, store.Asset{
			Tag: rel.Tag, Name: a.Name, URL: a.URL, Size: a.Size,
			Arch: info.Arch, Format: info.Format, Digest: a.Digest, ChecksumURL: sum,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func normalize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == ' ' || r == '.' {
			return -1
		}
		return r
	}, strings.ToLower(s))
}
