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
			t.Fatalf("abertura %d: %v", i, err)
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
		t.Fatal("esperava erro")
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'b'`).Scan(&n)
	if n != 0 {
		t.Error("tabela b não deveria existir após rollback")
	}
}

func TestDuplicateMigrationNumber(t *testing.T) {
	fs := fstest.MapFS{
		"migrations/0001_a.sql": {Data: []byte("")},
		"migrations/0001_b.sql": {Data: []byte("")},
	}
	if _, err := loadMigrations(fs); err == nil {
		t.Fatal("esperava erro de número duplicado")
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
		t.Errorf("total de assets = %d, want 1", n)
	}
}

func TestSaveIndexedMismatch(t *testing.T) {
	s := openTest(t)
	err := s.SaveIndexed(context.Background(), Repo{FullName: "a/b"}, App{FullName: "c/d"}, nil)
	if err == nil {
		t.Fatal("esperava erro")
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
		t.Errorf("categoria: %+v", g)
	}
	q, _ := s.ListApps(ctx, Filter{Query: "VM"})
	if len(q) != 1 || q[0].FullName != "a/vm" {
		t.Errorf("busca: %+v", q)
	}
	pct, _ := s.ListApps(ctx, Filter{Query: "%"})
	if len(pct) != 0 {
		t.Errorf("%% deveria ser literal: %+v", pct)
	}
	lim, _ := s.ListApps(ctx, Filter{Limit: 1, Offset: 1})
	if len(lim) != 1 || lim[0].FullName != "a/photo" {
		t.Errorf("paginação: %+v", lim)
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
		t.Errorf("instalados: %+v", inst)
	}
	d, _ := s.GetApp(ctx, "a/vm")
	if d.Install == nil {
		t.Error("GetApp sem Install")
	}

	// A instalação sobrevive à remoção do repo do catálogo.
	if err := s.RemoveRepo(ctx, "a/vm"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetApp(ctx, "a/vm"); !errors.Is(err, ErrNotFound) {
		t.Errorf("app deveria ter sido removido: %v", err)
	}
	list, _ := s.ListInstalls(ctx)
	if len(list) != 1 {
		t.Errorf("instalações: %+v", list)
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
	if err := s.UpdateStats(ctx, "a/b", 99, "nova", []string{"x"}, `"e2"`, 99.5, time.Now()); err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetApp(ctx, "a/b")
	if d.Repo.Stars != 99 || d.Score != 99.5 || d.Repo.ETag != `"e2"` || d.Repo.HeadSHA != "abc" {
		t.Errorf("detalhe = %+v", d.Repo)
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
		t.Error("marca não mudou após gravar")
	}
	s.UpdateStats(ctx, "a/photo", 9, "", nil, "", 9, time.Now().Add(time.Hour))
	st2, _ := s.CatalogStamp(ctx)
	if st2 == st1 {
		t.Error("marca não mudou após atualizar stats")
	}
	s.RemoveRepo(ctx, "a/lib")
	st3, _ := s.CatalogStamp(ctx)
	if st3 == st2 {
		t.Error("marca não mudou após remover")
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
