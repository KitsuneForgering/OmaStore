package index

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/github"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
)

type fakeRepo struct {
	repo    github.Repo
	sha     string
	release *github.Release
	readme  string
	files   []string
	toml    string // conteúdo do omastore.toml (vazio = arquivo vazio, que vale)
	noToml  bool   // repositório sem omastore.toml
}

// fakeGH simula a API do GitHub em memória e conta as chamadas.
type fakeGH struct {
	mu     sync.Mutex
	repos  map[string]*fakeRepo
	search []string
	calls  map[string]int
	rate   bool // simula rate limit em GetRepo
}

func (f *fakeGH) count(k string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[k]++
}

func (f *fakeGH) n(k string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[k]
}

func (f *fakeGH) get(name string) (*fakeRepo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.repos[name]
	if !ok {
		return nil, github.ErrNotFound
	}
	return r, nil
}

func (f *fakeGH) SearchByTopic(ctx context.Context, topic string, max int) ([]string, error) {
	f.count("search")
	return f.search, nil
}

func etagOf(r *fakeRepo) string {
	return `"` + r.repo.PushedAt.String() + "|" + string(rune('0'+r.repo.Stars%10)) + `"`
}

func (f *fakeGH) GetRepo(ctx context.Context, name, etag string) (*github.Repo, string, bool, error) {
	f.count("repo")
	if f.rate {
		return nil, "", false, &github.RateLimitError{Reset: time.Now()}
	}
	r, err := f.get(name)
	if err != nil {
		return nil, "", false, err
	}
	if etag != "" && etag == etagOf(r) {
		return nil, etag, true, nil
	}
	cp := r.repo
	return &cp, etagOf(r), false, nil
}

func (f *fakeGH) HeadSHA(ctx context.Context, name, ref, last string) (string, error) {
	f.count("head")
	r, err := f.get(name)
	if err != nil {
		return "", err
	}
	return r.sha, nil
}

func (f *fakeGH) LatestRelease(ctx context.Context, name string) (*github.Release, error) {
	f.count("release")
	r, err := f.get(name)
	if err != nil {
		return nil, err
	}
	return r.release, nil
}

func (f *fakeGH) Readme(ctx context.Context, name string) (string, string, error) {
	f.count("readme")
	r, err := f.get(name)
	if err != nil {
		return "", "", err
	}
	return r.readme, "README.md", nil
}

func (f *fakeGH) File(ctx context.Context, name, path, ref string, max int) (string, bool, error) {
	f.count("file")
	r, err := f.get(name)
	if err != nil {
		return "", false, err
	}
	if path != "omastore.toml" || r.noToml {
		return "", false, nil
	}
	return r.toml, true, nil
}

func (f *fakeGH) Tree(ctx context.Context, name, sha string) ([]string, bool, error) {
	f.count("tree")
	r, err := f.get(name)
	if err != nil {
		return nil, false, err
	}
	return r.files, false, nil
}

var t0 = time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

func photoRepo() *fakeRepo {
	return &fakeRepo{
		repo: github.Repo{FullName: "acme/omaphoto", Owner: "acme", Name: "omaphoto", Stars: 42,
			Topics: []string{"omarchy", "photo"}, DefaultBranch: "main", PushedAt: t0, License: "MIT"},
		sha: "sha1",
		release: &github.Release{Tag: "v1.0.0", Assets: []github.ReleaseAsset{
			{Name: "omaphoto-1.0.0-x86_64-linux.tar.gz", URL: "https://dl/x86.tgz", Size: 10, Digest: "sha256:aa"},
			{Name: "omaphoto-1.0.0-aarch64-linux.tar.gz", URL: "https://dl/arm.tgz", Size: 10},
			{Name: "omaphoto-1.0.0-x86_64-linux.tar.gz.sha256", URL: "https://dl/x86.sha256"},
			{Name: "checksums.txt", URL: "https://dl/checksums.txt"},
			{Name: "omaphoto-source.tar.gz", URL: "https://dl/src.tgz"},
		}},
		readme: "# OmaPhoto\n\nEditor de fotos rápido para o Omarchy.\n\n![shot](docs/shot.png)\n",
		files:  []string{"README.md", "assets/icon.svg", "screenshots/main.png"},
	}
}

func libRepo() *fakeRepo {
	return &fakeRepo{
		repo: github.Repo{FullName: "acme/omalib", Name: "omalib", Stars: 5, PushedAt: t0, DefaultBranch: "main"},
		sha:  "l1", readme: "# omalib\n\nBiblioteca sem binário nenhum aqui.\n",
		release: &github.Release{Tag: "v0.1", Assets: []github.ReleaseAsset{{Name: "omalib-0.1.deb", URL: "u"}}},
	}
}

func setup(t *testing.T) (*Indexer, *fakeGH, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	gh := &fakeGH{
		repos:  map[string]*fakeRepo{"acme/omaphoto": photoRepo(), "acme/omalib": libRepo()},
		search: []string{"acme/omaphoto", "acme/omalib"},
	}
	ix := &Indexer{
		GH: gh, Store: st, Seeds: []string{}, Workers: 2, GOARCH: "amd64",
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return t0.Add(24 * time.Hour) },
	}
	return ix, gh, st
}

func TestIndexNewRepo(t *testing.T) {
	ix, _, st := setup(t)
	ctx := context.Background()
	var last Progress
	stats, err := ix.Run(ctx, Options{Progress: func(p Progress) { last = p }})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Updated != 2 || last.Done != 2 || last.Total != 2 {
		t.Errorf("stats=%+v last=%+v", stats, last)
	}
	d, err := st.GetApp(ctx, "acme/omaphoto")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "OmaPhoto" || d.Summary != "Editor de fotos rápido para o Omarchy." || d.Category != CatGraphics || !d.Installable {
		t.Errorf("app = %+v", d.App)
	}
	if d.IconURL != "https://raw.githubusercontent.com/acme/omaphoto/sha1/assets/icon.svg" {
		t.Errorf("icon = %q", d.IconURL)
	}
	if len(d.Screenshots) != 2 || d.Screenshots[0] != "https://raw.githubusercontent.com/acme/omaphoto/sha1/docs/shot.png" {
		t.Errorf("screenshots = %v", d.Screenshots)
	}
	if len(d.Assets) != 2 {
		t.Fatalf("assets = %+v", d.Assets)
	}
	for _, a := range d.Assets {
		switch a.Arch {
		case ArchAMD64:
			if a.ChecksumURL != "https://dl/x86.sha256" || a.Digest != "sha256:aa" {
				t.Errorf("amd64: %+v", a)
			}
		case ArchARM64:
			if a.ChecksumURL != "https://dl/checksums.txt" {
				t.Errorf("arm64: %+v", a)
			}
		}
	}
	if d.Repo.HeadSHA != "sha1" || d.Repo.LatestTag != "v1.0.0" {
		t.Errorf("repo = %+v", d.Repo)
	}
}

func TestRepoWithoutBinaryIsExcluded(t *testing.T) {
	ix, _, st := setup(t)
	ctx := context.Background()
	if _, err := ix.Run(ctx, Options{}); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListApps(ctx, store.Filter{})
	if len(list) != 1 || list[0].FullName != "acme/omaphoto" {
		t.Errorf("catálogo instalável = %+v", list)
	}
	all, _ := st.ListApps(ctx, store.Filter{All: true})
	if len(all) != 2 {
		t.Errorf("com não instaláveis = %d", len(all))
	}
}

func TestUnchangedRepoIsNotReprocessed(t *testing.T) {
	ix, gh, _ := setup(t)
	ctx := context.Background()
	if _, err := ix.Run(ctx, Options{}); err != nil {
		t.Fatal(err)
	}
	readmes, trees := gh.n("readme"), gh.n("tree")

	stats, err := ix.Run(ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Unchanged != 2 || stats.Updated != 0 {
		t.Errorf("stats = %+v", stats)
	}
	if gh.n("readme") != readmes || gh.n("tree") != trees || gh.n("head") != 2 {
		t.Errorf("reprocessou: calls = %v", gh.calls)
	}

	// Só stars mudaram: atualização leve, sem README/árvore.
	gh.repos["acme/omaphoto"].repo.Stars = 43
	stats, _ = ix.Run(ctx, Options{})
	if stats.Refreshed != 1 || gh.n("readme") != readmes {
		t.Errorf("stars: stats=%+v calls=%v", stats, gh.calls)
	}

	// Force reprocessa tudo.
	stats, _ = ix.Run(ctx, Options{Force: true})
	if stats.Updated != 2 || gh.n("readme") != readmes+2 {
		t.Errorf("force: stats=%+v", stats)
	}
}

func TestNewCommitOrReleaseReprocesses(t *testing.T) {
	ix, gh, st := setup(t)
	ctx := context.Background()
	ix.Run(ctx, Options{})

	r := gh.repos["acme/omaphoto"]
	r.repo.PushedAt = t0.Add(time.Hour)
	r.sha = "sha2"
	stats, _ := ix.Run(ctx, Options{})
	if stats.Updated != 1 || stats.Unchanged != 1 {
		t.Errorf("novo commit: %+v", stats)
	}

	// Release nova sem push (304 no repo): a tag denuncia a mudança.
	r.release = &github.Release{Tag: "v2.0.0", Assets: r.release.Assets}
	stats, _ = ix.Run(ctx, Options{})
	if stats.Updated != 1 {
		t.Errorf("nova release: %+v", stats)
	}
	d, _ := st.GetApp(ctx, "acme/omaphoto")
	if d.Repo.LatestTag != "v2.0.0" || d.Repo.HeadSHA != "sha2" {
		t.Errorf("repo = %+v", d.Repo)
	}
}

func TestRemovedAndPrune(t *testing.T) {
	ix, gh, st := setup(t)
	ctx := context.Background()
	ix.Run(ctx, Options{})

	delete(gh.repos, "acme/omalib")
	stats, err := ix.Run(ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 1 {
		t.Errorf("stats = %+v", stats)
	}

	gh.search = nil
	ix.Prune = true
	stats, _ = ix.Run(ctx, Options{})
	if stats.Removed != 1 {
		t.Errorf("prune: %+v", stats)
	}
	names, _ := st.RepoNames(ctx)
	if len(names) != 0 {
		t.Errorf("sobrou %v", names)
	}
}

func TestRateLimitAborts(t *testing.T) {
	ix, gh, _ := setup(t)
	gh.rate = true
	_, err := ix.Run(context.Background(), Options{})
	var rl *github.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscoverDedup(t *testing.T) {
	ix, _, _ := setup(t)
	ix.Seeds = []string{"ACME/omaphoto", "x/y"}
	got, err := ix.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "ACME/omaphoto" || got[1] != "x/y" || got[2] != "acme/omalib" {
		t.Errorf("got %v", got)
	}
}

func TestSummaryFromDescriptionIsTrimmed(t *testing.T) {
	ix, gh, st := setup(t)
	gh.repos["acme/omaphoto"].repo.Description = "  Editor\n de   fotos  "
	if _, err := ix.Run(context.Background(), Options{}); err != nil {
		t.Fatal(err)
	}
	d, _ := st.GetApp(context.Background(), "acme/omaphoto")
	if d.Summary != "Editor de fotos" {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestParseSeeds(t *testing.T) {
	got := parseSeeds("# c\n a/b \n\nc/d # comentário\n")
	if len(got) != 2 || got[0] != "a/b" || got[1] != "c/d" {
		t.Errorf("got %v", got)
	}
	if len(Seeds()) == 0 {
		t.Error("seeds embutidas vazias")
	}
}

func TestIndexVersionBumpReprocesses(t *testing.T) {
	ix, gh, st := setup(t)
	ctx := context.Background()
	ix.Run(ctx, Options{})
	d, _ := st.GetApp(ctx, "acme/omaphoto")
	// Simula um repo gravado por uma versão antiga do indexador.
	d.Repo.IndexVersion = Version - 1
	st.SaveIndexed(ctx, d.Repo, d.App, d.Assets)
	readmes := gh.n("readme")

	stats, err := ix.Run(ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Updated != 1 || stats.Unchanged != 1 || gh.n("readme") != readmes+1 {
		t.Errorf("stats = %+v", stats)
	}
	state, _ := st.RepoState(ctx, "acme/omaphoto")
	if state.IndexVersion != Version {
		t.Errorf("versão gravada = %d", state.IndexVersion)
	}
}

func TestManifestOverridesHeuristics(t *testing.T) {
	ix, gh, st := setup(t)
	ctx := context.Background()
	r := gh.repos["acme/omaphoto"]
	r.files = append(r.files, "omastore.toml", "packaging/real-icon.svg")
	r.toml = `
name = "Oma Photo Pro"
summary = "Editor declarado no manifesto"
categories = ["Office", "Graphics"]
icon = "packaging/real-icon.svg"
screenshots = ["docs/tela.png", "https://example.com/extra.png"]
terminal = true
[linux.x86_64]
asset = "omaphoto-portable-{version}"
exec = "bin/omaphoto"
`
	// Um asset com nome que a heurística rejeitaria (sem arch nem "linux").
	r.release.Assets = append(r.release.Assets, github.ReleaseAsset{Name: "omaphoto-portable-1.0.0", URL: "https://dl/port"})
	if _, err := ix.Run(ctx, Options{}); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetApp(ctx, "acme/omaphoto")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Oma Photo Pro" || d.Summary != "Editor declarado no manifesto" || d.Category != "Office" {
		t.Errorf("app = %+v", d.App)
	}
	if d.IconURL != "https://raw.githubusercontent.com/acme/omaphoto/sha1/packaging/real-icon.svg" {
		t.Errorf("icon = %s", d.IconURL)
	}
	if len(d.Screenshots) != 2 || d.Screenshots[1] != "https://example.com/extra.png" {
		t.Errorf("screenshots = %v", d.Screenshots)
	}
	var found bool
	for _, a := range d.Assets {
		if a.Name == "omaphoto-portable-1.0.0" {
			found = a.Arch == ArchAMD64 && a.Format == FormatBinary
		}
	}
	if !found {
		t.Errorf("asset do manifesto ausente ou sem arch: %+v", d.Assets)
	}
	if d.Manifest == "" {
		t.Error("manifesto não gravado")
	}
}

func TestOnlyAppsWithManifestAreIndexed(t *testing.T) {
	for _, batched := range []bool{false, true} {
		ix, gh, st := setup(t)
		if batched {
			ix.GH = &fakeBatchGH{fakeGH: gh}
		}
		ctx := context.Background()
		gh.repos["acme/theme"] = &fakeRepo{repo: github.Repo{FullName: "acme/theme", Name: "theme", PushedAt: t0},
			sha: "t1", toml: `kind = "theme"`, release: libRepo().release}
		gh.repos["acme/plugin"] = &fakeRepo{repo: github.Repo{FullName: "acme/plugin", Name: "plugin", PushedAt: t0},
			sha: "p1", toml: `kind = "plugin"`, release: libRepo().release}
		gh.repos["acme/broken"] = &fakeRepo{repo: github.Repo{FullName: "acme/broken", Name: "broken", PushedAt: t0},
			sha: "b1", toml: "name = \"sem aspas\n"}
		gh.repos["acme/nothing"] = &fakeRepo{repo: github.Repo{FullName: "acme/nothing", Name: "nothing", PushedAt: t0},
			sha: "n1", noToml: true}
		gh.search = append(gh.search, "acme/theme", "acme/plugin", "acme/broken", "acme/nothing")

		stats, err := ix.Run(ctx, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if stats.Updated != 2 || stats.NotApps != 4 {
			t.Errorf("batched=%v: stats = %+v", batched, stats)
		}
		names, _ := st.RepoNames(ctx)
		if len(names) != 2 {
			t.Errorf("batched=%v: catálogo = %v", batched, names)
		}

		// Um app que remove o manifesto sai do catálogo.
		r := gh.repos["acme/omaphoto"]
		r.noToml = true
		r.sha = "sha-sem-manifesto"
		r.repo.PushedAt = t0.Add(time.Hour)
		stats, _ = ix.Run(ctx, Options{})
		if stats.Removed != 1 {
			t.Errorf("batched=%v: remoção: %+v", batched, stats)
		}
		if _, err := st.GetApp(ctx, "acme/omaphoto"); err == nil {
			t.Errorf("batched=%v: app sem manifesto continua no catálogo", batched)
		}
	}
}

func TestEmptyManifestIsEnough(t *testing.T) {
	ix, gh, st := setup(t)
	gh.repos["acme/omaphoto"].toml = ""
	if _, err := ix.Run(context.Background(), Options{}); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetApp(context.Background(), "acme/omaphoto")
	if err != nil || !d.Installable || d.Name != "OmaPhoto" {
		t.Errorf("manifesto vazio deveria indexar com heurísticas: %+v %v", d, err)
	}
}

// fakeBatchGH acrescenta a consulta em lote ao fakeGH.
type fakeBatchGH struct {
	*fakeGH
	noToken bool
}

func (f *fakeBatchGH) Snapshots(ctx context.Context, names []string) (map[string]*github.Snapshot, error) {
	f.count("snapshots")
	if f.noToken {
		return nil, github.ErrNoToken
	}
	out := map[string]*github.Snapshot{}
	for _, n := range names {
		r, err := f.get(n)
		if err != nil {
			out[n] = nil
			continue
		}
		snap := &github.Snapshot{Repo: r.repo, HeadSHA: r.sha, Release: r.release}
		if !r.noToml {
			toml := r.toml
			snap.Manifest = &toml
		}
		out[n] = snap
	}
	return out, nil
}

func batchSetup(t *testing.T) (*Indexer, *fakeBatchGH, *store.Store) {
	ix, gh, st := setup(t)
	b := &fakeBatchGH{fakeGH: gh}
	ix.GH = b
	return ix, b, st
}

func TestBatchedIndexingSkipsRESTMetadata(t *testing.T) {
	ix, gh, st := batchSetup(t)
	ctx := context.Background()
	stats, err := ix.Run(ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Updated != 2 {
		t.Errorf("stats = %+v", stats)
	}
	if gh.n("snapshots") != 1 || gh.n("repo") != 0 || gh.n("head") != 0 || gh.n("release") != 0 {
		t.Errorf("chamadas = %v", gh.calls)
	}
	d, _ := st.GetApp(ctx, "acme/omaphoto")
	if !d.Installable || d.Repo.HeadSHA != "sha1" || d.Repo.LatestTag != "v1.0.0" {
		t.Errorf("app = %+v", d)
	}

	// Nada mudou: nenhuma requisição além do lote.
	before := map[string]int{"readme": gh.n("readme"), "tree": gh.n("tree")}
	stats, _ = ix.Run(ctx, Options{})
	if stats.Unchanged != 2 || gh.n("readme") != before["readme"] || gh.n("tree") != before["tree"] {
		t.Errorf("inalterado: stats=%+v calls=%v", stats, gh.calls)
	}

	// Só estrelas mudaram: atualização leve.
	gh.repos["acme/omaphoto"].repo.Stars = 99
	stats, _ = ix.Run(ctx, Options{})
	if stats.Refreshed != 1 || stats.Unchanged != 1 {
		t.Errorf("estrelas: %+v", stats)
	}

	// Repositório sumiu: removido; na execução seguinte, só ignorado.
	delete(gh.repos, "acme/omalib")
	stats, _ = ix.Run(ctx, Options{})
	if stats.Removed != 1 || stats.Skipped != 0 {
		t.Errorf("removido: %+v", stats)
	}
	stats, _ = ix.Run(ctx, Options{})
	if stats.Removed != 0 || stats.Skipped != 1 {
		t.Errorf("ignorado: %+v", stats)
	}
}

func TestBatchedFallsBackWithoutToken(t *testing.T) {
	ix, gh, _ := batchSetup(t)
	gh.noToken = true
	stats, err := ix.Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Updated != 2 || gh.n("repo") == 0 {
		t.Errorf("fallback REST: stats=%+v calls=%v", stats, gh.calls)
	}
}

func TestNoReleaseSkipsReadmeAndTree(t *testing.T) {
	ix, gh, st := setup(t)
	gh.repos["acme/omalib"].release = nil
	if _, err := ix.Run(context.Background(), Options{Only: []string{"acme/omalib"}}); err != nil {
		t.Fatal(err)
	}
	if gh.n("readme") != 0 || gh.n("tree") != 0 {
		t.Errorf("sem release não deveria buscar README/árvore: %v", gh.calls)
	}
	d, err := st.GetApp(context.Background(), "acme/omalib")
	if err != nil || d.Installable {
		t.Errorf("app = %+v, %v", d, err)
	}
}

func TestRepoLinksAndListSeeds(t *testing.T) {
	text := `# Awesome Omarchy
- [Aether](https://github.com/omacom/aether) - theming
- [dup](https://github.com/OMACOM/Aether/issues/3)
- https://github.com/pch/rawmakase.git
- [topic](https://github.com/topics/omarchy)
- [self](https://github.com/me/awesome)
- [site](https://omarchy.org)`
	got := RepoLinks(text, "me/awesome", 0)
	want := []string{"omacom/aether", "pch/rawmakase"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("links = %v", got)
	}
	if n := len(RepoLinks(text, "me/awesome", 1)); n != 1 {
		t.Errorf("max: %d", n)
	}

	ix, gh, _ := setup(t)
	gh.repos["me/awesome"] = &fakeRepo{readme: "- https://github.com/acme/omaphoto\n- https://github.com/x/y\n"}
	ix.Seeds = []string{"list:me/awesome"}
	gh.search = nil
	names, err := ix.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "acme/omaphoto" || names[1] != "x/y" {
		t.Errorf("sementes da lista = %v", names)
	}
}

type fakeManifestGH struct {
	*fakeGH
	found []string
}

func (f *fakeManifestGH) SearchManifests(ctx context.Context, max int) ([]string, error) {
	return f.found, nil
}

func TestDiscoverByManifest(t *testing.T) {
	ix, gh, _ := setup(t)
	ix.GH = &fakeManifestGH{fakeGH: gh, found: []string{"z/sem-topic", "acme/omaphoto"}}
	got, err := ix.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "z/sem-topic" {
		t.Errorf("descoberta = %v", got)
	}
}

// Um repo descoberto com outra capitalização (ou renomeado) resolve para o
// nome gravado; se o registro for de uma versão antiga, precisa ser
// reprocessado mesmo assim.
func TestIndexVersionAppliesAfterNameResolution(t *testing.T) {
	for _, batched := range []bool{false, true} {
		ix, gh, st := setup(t)
		if batched {
			ix.GH = &fakeBatchGH{fakeGH: gh}
		}
		ctx := context.Background()
		ix.Run(ctx, Options{})
		d, _ := st.GetApp(ctx, "acme/omaphoto")
		d.Repo.IndexVersion = Version - 1
		st.SaveIndexed(ctx, d.Repo, d.App, d.Assets)

		// A descoberta devolve o nome com outra grafia; a API responde o canônico.
		gh.repos["ACME/OmaPhoto"] = gh.repos["acme/omaphoto"]
		gh.search = []string{"ACME/OmaPhoto"}
		stats, err := ix.Run(ctx, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if stats.Updated != 1 {
			t.Errorf("batched=%v: stats = %+v", batched, stats)
		}
		state, _ := st.RepoState(ctx, "acme/omaphoto")
		if state.IndexVersion != Version {
			t.Errorf("batched=%v: versão = %d", batched, state.IndexVersion)
		}
	}
}

func TestManifestOverride(t *testing.T) {
	for _, batched := range []bool{false, true} {
		ix, gh, st := setup(t)
		if batched {
			ix.GH = &fakeBatchGH{fakeGH: gh}
		}
		gh.repos["acme/omaphoto"].noToml = true // ainda não publicado
		_, err := ix.Run(context.Background(), Options{
			Only:             []string{"acme/omaphoto"},
			ManifestOverride: map[string]string{"ACME/omaphoto": `name = "Prévia"`},
		})
		if err != nil {
			t.Fatal(err)
		}
		d, err := st.GetApp(context.Background(), "acme/omaphoto")
		if err != nil || d.Name != "Prévia" {
			t.Errorf("batched=%v: %+v %v", batched, d, err)
		}
	}
}
