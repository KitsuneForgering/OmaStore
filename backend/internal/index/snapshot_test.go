package index

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

// releaseURLs points the fixtures' download URLs at the repositories' own
// releases, as real ones are.
func releaseURLs(snap *Snapshot) {
	for i, e := range snap.Repos {
		for j, a := range e.Assets {
			base := "https://github.com/" + e.Repo.FullName + "/releases/download/" + a.Tag + "/"
			snap.Repos[i].Assets[j].URL = base + a.Name
			if a.ChecksumURL != "" {
				snap.Repos[i].Assets[j].ChecksumURL = base + "checksums.txt"
			}
		}
	}
}

func emptyStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// What one OmaStore exports, another imports as the same catalog; an entry
// that points downloads outside its own releases is dropped.
func TestSnapshotRoundTrip(t *testing.T) {
	ix, _, _ := setup(t)
	ctx := context.Background()
	if _, err := ix.Run(ctx, Options{}); err != nil {
		t.Fatal(err)
	}
	snap, err := ix.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Repos) != 2 || snap.IndexVersion != Version || snap.Format != SnapshotFormat {
		t.Fatalf("snapshot = %d repos, version %d", len(snap.Repos), snap.IndexVersion)
	}
	releaseURLs(snap)
	evil := snap.Repos[0]
	evil.Repo.FullName, evil.App.FullName = "evil/app", "evil/app"
	evil.Assets = []store.Asset{{Tag: "v1", Name: "x.tar.gz", URL: "https://evil.example/x.tar.gz", Format: "tar.gz"}}
	snap.Repos = append(snap.Repos, evil)

	other := &Indexer{GH: &fakeGH{repos: map[string]*fakeRepo{}}, Store: emptyStore(t)}
	n, err := other.Import(ctx, snap)
	if err != nil || n != 2 {
		t.Fatalf("imported %d, %v; want 2", n, err)
	}
	d, err := other.Store.GetApp(ctx, "acme/omaphoto")
	if err != nil || d.Name != "OmaPhoto" || len(d.Assets) != 2 || d.Repo.ETag != "" {
		t.Errorf("imported app = %+v, %v", d, err)
	}
	if _, err := other.Store.GetApp(ctx, "evil/app"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the entry with a foreign download was imported: %v", err)
	}
}

// A full run on an empty catalog uses the snapshot and asks GitHub nothing;
// a catalog with repositories, or a failed snapshot, discovers as usual.
func TestRunImportsSnapshotWhenEmpty(t *testing.T) {
	src, _, _ := setup(t)
	ctx := context.Background()
	if _, err := src.Run(ctx, Options{}); err != nil {
		t.Fatal(err)
	}
	snap, _ := src.Export(ctx)
	releaseURLs(snap)

	gh := &fakeGH{repos: map[string]*fakeRepo{}, calls: map[string]int{}}
	ix := &Indexer{GH: gh, Store: emptyStore(t), Seeds: []string{},
		Snapshot: func(context.Context) (*Snapshot, error) { return snap, nil }}
	st, err := ix.Run(ctx, Options{})
	if err != nil || st.Updated != 2 {
		t.Fatalf("stats = %+v, %v", st, err)
	}
	if len(gh.calls) != 0 {
		t.Errorf("GitHub was asked %v on a snapshot run", gh.calls)
	}

	calls := 0
	ix.Snapshot = func(context.Context) (*Snapshot, error) { calls++; return snap, nil }
	ix.Run(ctx, Options{})
	if calls != 0 {
		t.Error("a catalog with repositories fetched the snapshot again")
	}

	failing := &Indexer{GH: gh, Store: emptyStore(t), Seeds: []string{},
		Snapshot: func(context.Context) (*Snapshot, error) { return nil, errors.New("offline") }}
	failing.Run(ctx, Options{})
	if gh.n("search") == 0 {
		t.Error("a failed snapshot did not fall back to discovery")
	}
}
