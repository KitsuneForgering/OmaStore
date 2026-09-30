package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "sub", "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seed(t *testing.T, s *Store, name string, stars int, cat string, installable bool) {
	t.Helper()
	pushed := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	r := Repo{FullName: name, Stars: stars, Topics: []string{"omarchy"}, PushedAt: pushed,
		HeadSHA: "abc", LatestTag: "v1.0.0", IndexedAt: pushed}
	a := App{Name: filepath.Base(name), Summary: "App " + name, Category: cat,
		Score: float64(stars), Installable: installable, Screenshots: []string{"https://x/s.png"}}
	assets := []Asset{{Tag: "v1.0.0", Name: "app-linux-amd64.tar.gz", URL: "https://x/a", Arch: "amd64", Format: "tar.gz"}}
	if err := s.SaveIndexed(context.Background(), r, a, assets); err != nil {
		t.Fatal(err)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	for i := 0; i < 2; i++ {
		s, err := Open(context.Background(), path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		s.Close()
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	s := openTest(t)
	bad := fstest.MapFS{
		"migrations/9001_ok.sql":  {Data: []byte("CREATE TABLE a(x);")},
		"migrations/9002_bad.sql": {Data: []byte("CREATE TABLE b(x); SYNTAX ERROR;")},
	}
	if err := migrate(context.Background(), s.db, bad); err == nil {
		t.Fatal("expected an error")
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'b'`).Scan(&n)
	if n != 0 {
		t.Error("table b should not exist after rollback")
	}
}

func TestDuplicateMigrationNumber(t *testing.T) {
	fs := fstest.MapFS{
		"migrations/0001_a.sql": {Data: []byte("")},
		"migrations/0001_b.sql": {Data: []byte("")},
	}
	if _, err := loadMigrations(fs); err == nil {
		t.Fatal("expected a duplicate number error")
	}
}

func TestRepoState(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.RepoState(ctx, "a/b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	seed(t, s, "a/b", 10, "Graphics", true)
	st, err := s.RepoState(ctx, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if st.HeadSHA != "abc" || st.LatestTag != "v1.0.0" || !st.PushedAt.Equal(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("estado inesperado: %+v", st)
	}
}

func TestSaveIndexedReplacesAssets(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	seed(t, s, "a/b", 10, "Graphics", true)
	r := Repo{FullName: "a/b", LatestTag: "v2"}
	a := App{Name: "b", Category: "Graphics", Installable: true}
	if err := s.SaveIndexed(ctx, r, a, []Asset{{Tag: "v2", Name: "b-arm64", URL: "u", Arch: "arm64", Format: "binary"}}); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetApp(ctx, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Assets) != 1 || d.Assets[0].Name != "b-arm64" {
		t.Errorf("assets = %+v", d.Assets)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM assets`).Scan(&n)
	if n != 1 {
		t.Errorf("total assets = %d, want 1", n)
	}
}

func TestSaveIndexedMismatch(t *testing.T) {
	s := openTest(t)
	err := s.SaveIndexed(context.Background(), Repo{FullName: "a/b"}, App{FullName: "c/d"}, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestListApps(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	seed(t, s, "a/photo", 50, "Graphics", true)
	seed(t, s, "a/vm", 100, "System", true)
	seed(t, s, "a/lib", 500, "Development", false)

	all, err := s.ListApps(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].FullName != "a/vm" || all[1].FullName != "a/photo" {
		t.Errorf("ordem/filtro errados: %+v", all)
	}
	withLib, _ := s.ListApps(ctx, Filter{All: true})
	if len(withLib) != 3 {
		t.Errorf("All: %d itens", len(withLib))
	}
	g, _ := s.ListApps(ctx, Filter{Category: "Graphics"})
	if len(g) != 1 || g[0].FullName != "a/photo" {
		t.Errorf("category: %+v", g)
	}
	q, _ := s.ListApps(ctx, Filter{Query: "VM"})
	if len(q) != 1 || q[0].FullName != "a/vm" {
		t.Errorf("search: %+v", q)
	}
	pct, _ := s.ListApps(ctx, Filter{Query: "%"})
	if len(pct) != 0 {
		t.Errorf("%% should be literal: %+v", pct)
	}
	lim, _ := s.ListApps(ctx, Filter{Limit: 1, Offset: 1})
	if len(lim) != 1 || lim[0].FullName != "a/photo" {
		t.Errorf("pagination: %+v", lim)
	}

	cats, _ := s.Categories(ctx)
	if len(cats) != 2 || cats[0].Category != "Graphics" {
		t.Errorf("categorias: %+v", cats)
	}
}

func TestInstalls(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	seed(t, s, "a/vm", 100, "System", true)
	in := Install{FullName: "a/vm", Version: "v1.0.0", InstalledAt: time.Now(),
		ExecPath: "/h/.local/bin/vm", Files: []string{"/h/.local/bin/vm", "/h/x.desktop"}}
	if err := s.SaveInstall(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetInstall(ctx, "a/vm")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "v1.0.0" || len(got.Files) != 2 {
		t.Errorf("install = %+v", got)
	}
	inst, _ := s.ListApps(ctx, Filter{InstalledOnly: true})
	if len(inst) != 1 || inst[0].InstalledVersion != "v1.0.0" {
		t.Errorf("installed: %+v", inst)
	}
	d, _ := s.GetApp(ctx, "a/vm")
	if d.Install == nil {
		t.Error("GetApp without Install")
	}

	// The installation survives the repo being removed from the catalog.
	if err := s.RemoveRepo(ctx, "a/vm"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetApp(ctx, "a/vm"); !errors.Is(err, ErrNotFound) {
		t.Errorf("app should have been removed: %v", err)
	}
	list, _ := s.ListInstalls(ctx)
	if len(list) != 1 {
		t.Errorf("installations: %+v", list)
	}
	if err := s.DeleteInstall(ctx, "a/vm"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetInstall(ctx, "a/vm"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestUpdateStatsAndNames(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	seed(t, s, "a/b", 10, "Graphics", true)
	if err := s.UpdateStats(ctx, "a/b", 99, "new", []string{"x"}, `"e2"`, 99.5, time.Now()); err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetApp(ctx, "a/b")
	if d.Repo.Stars != 99 || d.Score != 99.5 || d.Repo.ETag != `"e2"` || d.Repo.HeadSHA != "abc" {
		t.Errorf("detail = %+v", d.Repo)
	}
	names, _ := s.RepoNames(ctx)
	if len(names) != 1 || names[0] != "a/b" {
		t.Errorf("names = %v", names)
	}
}

func TestIndexVersionRoundTrip(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if err := s.SaveIndexed(ctx, Repo{FullName: "a/b", IndexVersion: 7}, App{Name: "b"}, nil); err != nil {
		t.Fatal(err)
	}
	st, _ := s.RepoState(ctx, "a/b")
	if st.IndexVersion != 7 {
		t.Errorf("IndexVersion = %d", st.IndexVersion)
	}
}

func TestSearchDocsAndStamp(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	st0, _ := s.CatalogStamp(ctx)
	seed(t, s, "a/photo", 5, "Graphics", true)
	seed(t, s, "a/lib", 1, "Development", false)
	docs, err := s.SearchDocs(ctx)
	if err != nil || len(docs) != 2 {
		t.Fatalf("docs = %v, %v", docs, err)
	}
	st1, _ := s.CatalogStamp(ctx)
	if st1 == st0 {
		t.Error("stamp did not change after save")
	}
	s.UpdateStats(ctx, "a/photo", 9, "", nil, "", 9, time.Now().Add(time.Hour))
	st2, _ := s.CatalogStamp(ctx)
	if st2 == st1 {
		t.Error("stamp did not change after updating stats")
	}
	s.RemoveRepo(ctx, "a/lib")
	st3, _ := s.CatalogStamp(ctx)
	if st3 == st2 {
		t.Error("stamp did not change after remove")
	}
}

func TestManifestRoundTrip(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if err := s.SaveIndexed(ctx, Repo{FullName: "a/b"}, App{Name: "b", Manifest: `{"name":"B"}`}, nil); err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetApp(ctx, "a/b")
	if d.Manifest != `{"name":"B"}` {
		t.Errorf("manifest = %q", d.Manifest)
	}
}

func TestRenameInstall(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	s.SaveInstall(ctx, Install{FullName: "old/app", Version: "v1", InstalledAt: time.Now(), Files: []string{"/a"}})
	if err := s.RenameInstall(ctx, "old/app", "new/app"); err != nil {
		t.Fatal(err)
	}
	if in, err := s.GetInstall(ctx, "new/app"); err != nil || in.Files[0] != "/a" {
		t.Errorf("new/app: %+v %v", in, err)
	}
	if _, err := s.GetInstall(ctx, "old/app"); !errors.Is(err, ErrNotFound) {
		t.Errorf("old/app still recorded: %v", err)
	}
	// The target already has its own installation: nothing moves.
	s.SaveInstall(ctx, Install{FullName: "x/app", Version: "v9", InstalledAt: time.Now()})
	if err := s.RenameInstall(ctx, "x/app", "new/app"); err != nil {
		t.Fatal(err)
	}
	if in, _ := s.GetInstall(ctx, "new/app"); in.Version != "v1" {
		t.Errorf("existing installation overwritten: %+v", in)
	}
	// Nothing recorded under the old name: no-op.
	if err := s.RenameInstall(ctx, "none/app", "other/app"); err != nil {
		t.Fatal(err)
	}
}

func TestInstallsStampChanges(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a, _ := s.InstallsStamp(ctx)
	s.SaveInstall(ctx, Install{FullName: "a/b", Version: "v1", InstalledAt: time.Now()})
	b, _ := s.InstallsStamp(ctx)
	s.DeleteInstall(ctx, "a/b")
	c, _ := s.InstallsStamp(ctx)
	if a == b || b == c {
		t.Errorf("stamps %q %q %q", a, b, c)
	}
}

func TestLookupsIgnoreCase(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	seed(t, s, "pch/rawmakase", 10, "Graphics", true)
	if err := s.SaveInstall(ctx, Install{FullName: "pch/rawmakase", Version: "v1.0.0", InstalledAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetApp(ctx, "PCH/Rawmakase")
	if err != nil {
		t.Fatal(err)
	}
	if d.FullName != "pch/rawmakase" || len(d.Assets) != 1 || d.Install == nil {
		t.Errorf("detail = %q, %d assets, install %v", d.FullName, len(d.Assets), d.Install)
	}
	in, err := s.GetInstall(ctx, "Pch/RAWMAKASE")
	if err != nil || in.FullName != "pch/rawmakase" {
		t.Errorf("install = %+v, %v", in, err)
	}
	if _, err := s.GetApp(ctx, "pch/other"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestCatalogStampIgnoresTouch(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	seed(t, s, "a/b", 1, "Utility", true)
	before, err := s.CatalogStamp(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TouchRepo(ctx, "a/b", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if after, _ := s.CatalogStamp(ctx); after != before {
		t.Errorf("touch changed the stamp: %q → %q", before, after)
	}
	if err := s.UpdateStats(ctx, "a/b", 5, "d", nil, "", 5, time.Now().Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if after, _ := s.CatalogStamp(ctx); after == before {
		t.Error("new stars did not change the stamp")
	}
}

func TestNotApps(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	fresh := func(name string, version int, since time.Time) bool {
		t.Helper()
		ok, err := s.NotAppFresh(ctx, name, version, since)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if err := s.MarkNotApp(ctx, "x/theme", 6, now); err != nil {
		t.Fatal(err)
	}
	if !fresh("X/Theme", 6, now.Add(-time.Hour)) {
		t.Error("mark not found (case must not matter)")
	}
	if fresh("x/theme", 7, now.Add(-time.Hour)) || fresh("x/theme", 6, now.Add(time.Hour)) {
		t.Error("a mark from another indexer version or older than since must not count")
	}
	// Becoming an app clears the mark.
	seed(t, s, "x/theme", 1, "Utility", true)
	if fresh("x/theme", 6, now.Add(-time.Hour)) {
		t.Error("SaveIndexed did not clear the mark")
	}
	s.MarkNotApp(ctx, "y/old", 6, now.Add(-60*24*time.Hour))
	if err := s.PruneNotApps(ctx, now.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if fresh("y/old", 6, time.Time{}) {
		t.Error("old mark not pruned")
	}
}
