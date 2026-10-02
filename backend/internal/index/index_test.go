package index

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/KitsuneForgering/OmaStore/backend/internal/asset"
	"github.com/KitsuneForgering/OmaStore/backend/internal/github"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

type fakeRepo struct {
	repo    github.Repo
	sha     string
	release *github.Release
	readme  string
	files   []string
	toml    string            // omastore.toml content (empty = empty file, which counts)
	noToml  bool              // repository without omastore.toml
	extra   map[string]string // other files readable through File
}

// fakeGH simulates the GitHub API in memory and counts the calls.
type fakeGH struct {
	mu     sync.Mutex
	repos  map[string]*fakeRepo
	search []string
	calls  map[string]int
	rate   bool // simulates a rate limit in GetRepo
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

// etagOf depends on the response body, which includes the full name (so a
// renamed repository never answers 304 to the old ETag).
func etagOf(r *fakeRepo) string {
	return `"` + r.repo.FullName + "|" + r.repo.PushedAt.String() + "|" + string(rune('0'+r.repo.Stars%10)) + `"`
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
	if c, ok := r.extra[path]; ok {
		return c, true, nil
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

// mustRun runs the indexer and fails the test on an error: an indexing
// failure must not show up only as an unexpected statistic.
func mustRun(t *testing.T, ix *Indexer, opts Options) Stats {
	t.Helper()
	stats, err := ix.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("index run: %v (stats %+v)", err, stats)
	}
	return stats
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
		readme: "# OmaPhoto\n\nFast photo editor for Omarchy.\n\n![shot](docs/shot.png)\n",
		files:  []string{"README.md", "assets/icon.svg", "screenshots/main.png"},
	}
}

func libRepo() *fakeRepo {
	return &fakeRepo{
		repo: github.Repo{FullName: "acme/omalib", Name: "omalib", Stars: 5, PushedAt: t0, DefaultBranch: "main"},
		sha:  "l1", readme: "# omalib\n\nA library with no binary at all here.\n",
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
	if d.Name != "OmaPhoto" || d.Summary != "Fast photo editor for Omarchy." || d.Category != CatGraphics || !d.Installable {
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
		case asset.ArchAMD64:
			if a.ChecksumURL != "https://dl/x86.sha256" || a.Digest != "sha256:aa" {
				t.Errorf("amd64: %+v", a)
			}
		case asset.ArchARM64:
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
		t.Errorf("installable catalog = %+v", list)
	}
	all, _ := st.ListApps(ctx, store.Filter{All: true})
	if len(all) != 2 {
		t.Errorf("with non-installable = %d", len(all))
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

	// Only stars changed: light update, without README/tree.
	gh.repos["acme/omaphoto"].repo.Stars = 43
	stats = mustRun(t, ix, Options{})
	if stats.Refreshed != 1 || gh.n("readme") != readmes {
		t.Errorf("stars: stats=%+v calls=%v", stats, gh.calls)
	}

	// Force reprocesses everything.
	stats = mustRun(t, ix, Options{Force: true})
	if stats.Updated != 2 || gh.n("readme") != readmes+2 {
		t.Errorf("force: stats=%+v", stats)
	}
}

func TestNewCommitOrReleaseReprocesses(t *testing.T) {
	ix, gh, st := setup(t)
	ctx := context.Background()
	mustRun(t, ix, Options{})

	r := gh.repos["acme/omaphoto"]
	r.repo.PushedAt = t0.Add(time.Hour)
	r.sha = "sha2"
	stats := mustRun(t, ix, Options{})
	if stats.Updated != 1 || stats.Unchanged != 1 {
		t.Errorf("new commit: %+v", stats)
	}

	// New release without a push (304 on the repo): the tag reveals the change.
	r.release = &github.Release{Tag: "v2.0.0", Body: "## v2\r\n\r\n- New export\r\n", Assets: r.release.Assets}
	stats = mustRun(t, ix, Options{})
	if stats.Updated != 1 {
		t.Errorf("new release: %+v", stats)
	}
	d, _ := st.GetApp(ctx, "acme/omaphoto")
	if d.Repo.LatestTag != "v2.0.0" || d.Repo.HeadSHA != "sha2" || d.Repo.ReleaseNotes != "## v2\n\n- New export" {
		t.Errorf("repo = %+v", d.Repo)
	}
}

func TestRemovedAndPrune(t *testing.T) {
	ix, gh, st := setup(t)
	ctx := context.Background()
	mustRun(t, ix, Options{})

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
	stats = mustRun(t, ix, Options{})
	if stats.Removed != 1 {
		t.Errorf("prune: %+v", stats)
	}
	names, _ := st.RepoNames(ctx)
	if len(names) != 0 {
		t.Errorf("left over %v", names)
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
	gh.repos["acme/omaphoto"].repo.Description = "  Photo\n  editor   for   Omarchy  "
	if _, err := ix.Run(context.Background(), Options{}); err != nil {
		t.Fatal(err)
	}
	d, _ := st.GetApp(context.Background(), "acme/omaphoto")
	if d.Summary != "Photo editor for Omarchy" {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestParseSeeds(t *testing.T) {
	got := parseSeeds("# c\n a/b \n\nc/d # comment\n")
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
	mustRun(t, ix, Options{})
	d, _ := st.GetApp(ctx, "acme/omaphoto")
	// Simulates a repo stored by an older indexer version.
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
		t.Errorf("stored version = %d", state.IndexVersion)
	}
}

func TestManifestOverridesHeuristics(t *testing.T) {
	ix, gh, st := setup(t)
	ctx := context.Background()
	r := gh.repos["acme/omaphoto"]
	r.files = append(r.files, "omastore.toml", "packaging/real-icon.svg")
	r.toml = `
name = "Oma Photo Pro"
summary = "Editor declared in the manifest"
categories = ["Office", "Graphics"]
icon = "packaging/real-icon.svg"
screenshots = ["docs/screen.png", "https://example.com/extra.png"]
terminal = true
[linux.x86_64]
asset = "omaphoto-portable-{version}"
exec = "bin/omaphoto"
`
	// An asset with a name the heuristic would reject (no arch nor "linux").
	r.release.Assets = append(r.release.Assets, github.ReleaseAsset{Name: "omaphoto-portable-1.0.0", URL: "https://dl/port"})
	if _, err := ix.Run(ctx, Options{}); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetApp(ctx, "acme/omaphoto")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Oma Photo Pro" || d.Summary != "Editor declared in the manifest" || d.Category != "Office" {
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
			found = a.Arch == asset.ArchAMD64 && a.Format == asset.FormatBinary
		}
	}
	if !found {
		t.Errorf("manifest asset missing or without arch: %+v", d.Assets)
	}
	if d.Manifest == "" {
		t.Error("manifest not stored")
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
			sha: "b1", toml: "name = \"missing quote\n"}
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
			t.Errorf("batched=%v: catalog = %v", batched, names)
		}

		// An app that removes its manifest leaves the catalog.
		r := gh.repos["acme/omaphoto"]
		r.noToml = true
		r.sha = "sha-without-manifest"
		r.repo.PushedAt = t0.Add(time.Hour)
		stats = mustRun(t, ix, Options{})
		if stats.Removed != 1 {
			t.Errorf("batched=%v: removal: %+v", batched, stats)
		}
		if _, err := st.GetApp(ctx, "acme/omaphoto"); err == nil {
			t.Errorf("batched=%v: app without manifest still in the catalog", batched)
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
		t.Errorf("an empty manifest should index with heuristics: %+v %v", d, err)
	}
}

// fakeBatchGH adds the batch query to fakeGH.
// Files replaced or added under the same tag (no push, same HEAD) are
// picked up: the stored digest must follow the new binary, or every install
// fails the checksum; an asset uploaded after a run caught the release half
// published must show up. Through REST (304 on the repository) and the batch.
func TestReleaseFilesChangedUnderSameTag(t *testing.T) {
	for _, batched := range []bool{false, true} {
		t.Run(map[bool]string{false: "rest", true: "batch"}[batched], func(t *testing.T) {
			ix, gh, st := setup(t)
			if batched {
				ix.GH = &fakeBatchGH{fakeGH: gh}
			}
			ctx := context.Background()
			r := gh.repos["acme/omaphoto"]
			// Half published: only the x86_64 tarball so far.
			full := r.release.Assets
			r.release = &github.Release{Tag: "v1.0.0", Assets: []github.ReleaseAsset{full[0]}}
			if _, err := ix.Run(ctx, Options{}); err != nil {
				t.Fatal(err)
			}
			d, err := st.GetApp(ctx, "acme/omaphoto")
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Assets) != 1 {
				t.Fatalf("half published: assets = %+v", d.Assets)
			}

			// The rest of the upload lands.
			r.release = &github.Release{Tag: "v1.0.0", Assets: full}
			stats, err := ix.Run(ctx, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if stats.Updated != 1 {
				t.Errorf("assets added: stats = %+v", stats)
			}
			if d, _ = st.GetApp(ctx, "acme/omaphoto"); len(d.Assets) != 2 {
				t.Errorf("assets added: %+v", d.Assets)
			}

			// The author replaces the x86_64 binary (gh release upload --clobber).
			replaced := append([]github.ReleaseAsset(nil), full...)
			replaced[0].Digest = "sha256:bb"
			replaced[0].Size = 11
			r.release = &github.Release{Tag: "v1.0.0", Assets: replaced}
			stats, err = ix.Run(ctx, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if stats.Updated != 1 {
				t.Errorf("asset replaced: stats = %+v", stats)
			}
			d, _ = st.GetApp(ctx, "acme/omaphoto")
			for _, a := range d.Assets {
				if a.Arch == asset.ArchAMD64 && a.Digest != "sha256:bb" {
					t.Errorf("stale digest: %+v", a)
				}
			}

			// And nothing changed: not reprocessed.
			stats, err = ix.Run(ctx, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if stats.Updated != 0 || stats.Unchanged != 2 {
				t.Errorf("unchanged: stats = %+v", stats)
			}
		})
	}
}

func TestReleaseSig(t *testing.T) {
	a := &github.Release{Tag: "v1", Assets: []github.ReleaseAsset{{Name: "a", Size: 1, Digest: "sha256:aa"}, {Name: "b", Size: 2}}}
	b := &github.Release{Tag: "v1", Assets: []github.ReleaseAsset{{Name: "b", Size: 2}, {Name: "a", Size: 1, Digest: "sha256:aa"}}}
	if releaseSig(a) != releaseSig(b) {
		t.Error("asset order changed the fingerprint")
	}
	if releaseSig(nil) != "" {
		t.Error("no release must give an empty fingerprint")
	}
	for _, changed := range []*github.Release{
		{Tag: "v2", Assets: a.Assets},
		{Tag: "v1", Assets: []github.ReleaseAsset{{Name: "a", Size: 1, Digest: "sha256:ab"}, {Name: "b", Size: 2}}},
		{Tag: "v1", Assets: []github.ReleaseAsset{{Name: "a", Size: 3, Digest: "sha256:aa"}, {Name: "b", Size: 2}}},
		{Tag: "v1", Assets: []github.ReleaseAsset{{Name: "a", Size: 1, Digest: "sha256:aa"}}},
	} {
		if releaseSig(changed) == releaseSig(a) {
			t.Errorf("%+v has the same fingerprint as %+v", changed, a)
		}
	}
}

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
		t.Errorf("calls = %v", gh.calls)
	}
	d, _ := st.GetApp(ctx, "acme/omaphoto")
	if !d.Installable || d.Repo.HeadSHA != "sha1" || d.Repo.LatestTag != "v1.0.0" {
		t.Errorf("app = %+v", d)
	}

	// Nothing changed: no request besides the batch.
	before := map[string]int{"readme": gh.n("readme"), "tree": gh.n("tree")}
	stats = mustRun(t, ix, Options{})
	if stats.Unchanged != 2 || gh.n("readme") != before["readme"] || gh.n("tree") != before["tree"] {
		t.Errorf("inalterado: stats=%+v calls=%v", stats, gh.calls)
	}

	// Only stars changed: light update.
	gh.repos["acme/omaphoto"].repo.Stars = 99
	stats = mustRun(t, ix, Options{})
	if stats.Refreshed != 1 || stats.Unchanged != 1 {
		t.Errorf("estrelas: %+v", stats)
	}

	// The repository disappeared: removed; on the next run, only skipped.
	delete(gh.repos, "acme/omalib")
	stats = mustRun(t, ix, Options{})
	if stats.Removed != 1 || stats.Skipped != 0 {
		t.Errorf("removed: %+v", stats)
	}
	stats = mustRun(t, ix, Options{})
	if stats.Removed != 0 || stats.Skipped != 1 {
		t.Errorf("skipped: %+v", stats)
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
		t.Errorf("without a release it should not fetch README/tree: %v", gh.calls)
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
		t.Errorf("seeds from the list = %v", names)
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
	ix.GH = &fakeManifestGH{fakeGH: gh, found: []string{"z/no-topic", "acme/omaphoto"}}
	got, err := ix.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "z/no-topic" {
		t.Errorf("descoberta = %v", got)
	}
}

// A repo discovered with different capitalization (or renamed) resolves to the
// stored name; if the record is from an older version, it must be
// reprocessed anyway.
func TestIndexVersionAppliesAfterNameResolution(t *testing.T) {
	for _, batched := range []bool{false, true} {
		ix, gh, st := setup(t)
		if batched {
			ix.GH = &fakeBatchGH{fakeGH: gh}
		}
		ctx := context.Background()
		mustRun(t, ix, Options{})
		d, _ := st.GetApp(ctx, "acme/omaphoto")
		d.Repo.IndexVersion = Version - 1
		st.SaveIndexed(ctx, d.Repo, d.App, d.Assets)

		// Discovery returns the name spelled differently; the API answers the canonical one.
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
			t.Errorf("batched=%v: version = %d", batched, state.IndexVersion)
		}
	}
}

func TestManifestOverride(t *testing.T) {
	for _, batched := range []bool{false, true} {
		ix, gh, st := setup(t)
		if batched {
			ix.GH = &fakeBatchGH{fakeGH: gh}
		}
		gh.repos["acme/omaphoto"].noToml = true // not published yet
		_, err := ix.Run(context.Background(), Options{
			Only:             []string{"acme/omaphoto"},
			ManifestOverride: map[string]string{"ACME/omaphoto": `name = "Preview"`},
		})
		if err != nil {
			t.Fatal(err)
		}
		d, err := st.GetApp(context.Background(), "acme/omaphoto")
		if err != nil || d.Name != "Preview" {
			t.Errorf("batched=%v: %+v %v", batched, d, err)
		}
	}
}

// A seed for a renamed/transferred repo is stored under the new name; prune
// must keep it, since it was discovered (through the old name) in this run.
func TestPruneKeepsRenamedRepo(t *testing.T) {
	for _, batched := range []bool{false, true} {
		ix, gh, st := setup(t)
		if batched {
			ix.GH = &fakeBatchGH{fakeGH: gh}
		}
		ctx := context.Background()
		gh.repos["old/omaphoto"] = gh.repos["acme/omaphoto"] // API redirects to acme/omaphoto
		gh.search = nil
		ix.Seeds = []string{"old/omaphoto"}
		ix.Prune = true
		if _, err := ix.Run(ctx, Options{}); err != nil {
			t.Fatal(err)
		}
		names, _ := st.RepoNames(ctx)
		if len(names) != 1 || names[0] != "acme/omaphoto" {
			t.Errorf("batched=%v: catalog = %v, want [acme/omaphoto]", batched, names)
		}
	}
}

// Without a token, a new repository without omastore.toml costs two requests
// (metadata and manifest): HEAD and the release are never fetched.
func TestRESTChecksManifestFirst(t *testing.T) {
	ix, gh, _ := setup(t)
	gh.repos = map[string]*fakeRepo{"acme/nothing": {
		repo: github.Repo{FullName: "acme/nothing", Name: "nothing", PushedAt: t0}, sha: "n1", noToml: true,
		release: libRepo().release}}
	gh.search = []string{"acme/nothing"}
	stats, err := ix.Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.NotApps != 1 {
		t.Errorf("stats = %+v", stats)
	}
	// One request: the manifest, on the default branch.
	if gh.n("head") != 0 || gh.n("release") != 0 || gh.n("repo") != 0 || gh.n("file") != 1 {
		t.Errorf("calls = %v", gh.calls)
	}

	// The next run skips the repository without requests...
	stats, err = ix.Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.NotApps != 1 || gh.n("file") != 1 || gh.n("repo") != 0 {
		t.Errorf("second run: stats = %+v, calls = %v", stats, gh.calls)
	}
	// ...unless it is asked for by name, or the mark expired.
	if _, err := ix.Run(context.Background(), Options{Only: []string{"acme/nothing"}}); err != nil {
		t.Fatal(err)
	}
	if gh.n("file") != 2 {
		t.Errorf("explicit run: calls = %v", gh.calls)
	}
	ix.Now = func() time.Time { return t0.Add(24*time.Hour + notAppTTL + time.Hour) }
	if _, err := ix.Run(context.Background(), Options{}); err != nil {
		t.Fatal(err)
	}
	if gh.n("file") != 3 {
		t.Errorf("expired mark: calls = %v", gh.calls)
	}
}

func TestSysDepsFromPKGBUILD(t *testing.T) {
	ix, gh, st := setup(t)
	ctx := context.Background()
	r := gh.repos["acme/omaphoto"]
	r.files = append(r.files, "packaging/arch/PKGBUILD", "packaging/arch-bin/.SRCINFO")
	r.extra = map[string]string{
		"packaging/arch-bin/.SRCINFO": "pkgbase = omaphoto-bin\n\tdepends = gtk4\n\toptdepends = ffmpeg: video\npkgname = omaphoto-bin\n",
		"packaging/arch/PKGBUILD":     "depends=(should-not-be-read)\n",
	}
	if _, err := ix.Run(ctx, Options{}); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetApp(ctx, "acme/omaphoto")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"source":"packaging/arch-bin/.SRCINFO","deps":[{"spec":"gtk4"},{"spec":"ffmpeg","reason":"video","optional":true}]}`
	if d.SysDeps != want {
		t.Errorf("sysdeps = %s", d.SysDeps)
	}
	if lib, _ := st.GetApp(ctx, "acme/omalib"); lib != nil && lib.SysDeps != "" {
		t.Errorf("repo without PKGBUILD: %q", lib.SysDeps)
	}
}

func TestReleaseNotesCap(t *testing.T) {
	if got := releaseNotes(nil); got != "" {
		t.Errorf("no release: %q", got)
	}
	// Cut on the last line break inside the cap.
	line := strings.Repeat("x", 99) + "\n"
	long := strings.Repeat(line, 2*maxReleaseNotes/len(line))
	got := releaseNotes(&github.Release{Body: long})
	if len(got) > maxReleaseNotes+8 || !strings.HasSuffix(got, strings.Repeat("x", 99)+"\n\n…") {
		t.Errorf("line cut: %d bytes, ends %q", len(got), got[len(got)-10:])
	}
	// One long line: cut on a rune boundary.
	got = releaseNotes(&github.Release{Body: strings.Repeat("é", maxReleaseNotes)})
	if !utf8.ValidString(got) || len(got) > maxReleaseNotes+8 {
		t.Errorf("rune cut: valid=%v len=%d", utf8.ValidString(got), len(got))
	}
}
