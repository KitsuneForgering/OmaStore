package index

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

// A stored repository that discovery no longer finds is still checked on
// every run: if it lost its manifest, it leaves the catalog without --prune.
func TestStoredRepoIsRecheckedWithoutDiscovery(t *testing.T) {
	for _, batched := range []bool{false, true} {
		ix, gh, st := setup(t)
		if batched {
			ix.GH = &fakeBatchGH{fakeGH: gh}
		}
		ctx := context.Background()
		if _, err := ix.Run(ctx, Options{}); err != nil {
			t.Fatal(err)
		}

		// The author removes the manifest; the repo was found only through it.
		r := gh.repos["acme/omaphoto"]
		r.noToml = true
		r.sha = "sha-without-manifest"
		r.repo.PushedAt = t0.Add(time.Hour)
		gh.search = []string{"acme/omalib"}

		stats, err := ix.Run(ctx, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if stats.Removed != 1 {
			t.Errorf("batched=%v: stats = %+v", batched, stats)
		}
		if _, err := st.GetApp(ctx, "acme/omaphoto"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("batched=%v: opted-out app still in the catalog: %v", batched, err)
		}
	}
}

// A repository renamed on GitHub, and discovered only under its new name,
// must not stay in the catalog twice; an installation recorded under the old
// name follows it, so it can still be updated.
func TestRenamedRepoLeavesNoDuplicateAndMovesInstall(t *testing.T) {
	for _, batched := range []bool{false, true} {
		ix, gh, st := setup(t)
		if batched {
			ix.GH = &fakeBatchGH{fakeGH: gh}
		}
		ctx := context.Background()

		before := photoRepo()
		before.repo.FullName, before.repo.Owner = "old/omaphoto", "old"
		gh.repos["old/omaphoto"] = before
		gh.search = []string{"old/omaphoto"}
		if _, err := ix.Run(ctx, Options{}); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveInstall(ctx, store.Install{FullName: "old/omaphoto", Version: "v1.0.0",
			InstalledAt: t0, Files: []string{"/x"}}); err != nil {
			t.Fatal(err)
		}

		// Renamed: the old name redirects, search only returns the new one.
		gh.repos["old/omaphoto"] = gh.repos["acme/omaphoto"]
		gh.search = []string{"acme/omaphoto"}
		if _, err := ix.Run(ctx, Options{}); err != nil {
			t.Fatal(err)
		}

		names, _ := st.RepoNames(ctx)
		if len(names) != 1 || names[0] != "acme/omaphoto" {
			t.Errorf("batched=%v: catalog = %v, want [acme/omaphoto]", batched, names)
		}
		if _, err := st.GetInstall(ctx, "acme/omaphoto"); err != nil {
			t.Errorf("batched=%v: installation not moved: %v", batched, err)
		}
		if _, err := st.GetInstall(ctx, "old/omaphoto"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("batched=%v: installation left under the old name: %v", batched, err)
		}
	}
}

type failingManifestGH struct{ *fakeGH }

func (f *failingManifestGH) SearchManifests(ctx context.Context, max int) ([]string, error) {
	return nil, errors.New("HTTP 502")
}

// If a discovery source fails, the list is partial: pruning with it would
// delete repositories that are fine.
func TestPartialDiscoveryDoesNotPrune(t *testing.T) {
	ix, gh, st := setup(t)
	ctx := context.Background()
	if _, err := ix.Run(ctx, Options{}); err != nil {
		t.Fatal(err)
	}

	ix.GH = &failingManifestGH{fakeGH: gh}
	gh.search = []string{"acme/omalib"} // omaphoto was found by the manifest search
	ix.Prune = true
	stats, err := ix.Run(ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 0 {
		t.Errorf("stats = %+v", stats)
	}
	if _, err := st.GetApp(ctx, "acme/omaphoto"); err != nil {
		t.Errorf("pruned after a partial discovery: %v", err)
	}
}

// Two indexers (daemon and CLI) on the same lock file do not run together.
func TestIndexLockIsExclusive(t *testing.T) {
	ix, _, _ := setup(t)
	ix.LockPath = filepath.Join(t.TempDir(), "index.lock")
	unlock, err := ix.lock()
	if err != nil {
		t.Fatal(err)
	}
	other := *ix
	if _, err := other.Run(context.Background(), Options{}); !errors.Is(err, ErrBusy) {
		t.Errorf("second run: %v, want ErrBusy", err)
	}
	unlock()
	if _, err := other.Run(context.Background(), Options{}); err != nil {
		t.Errorf("after unlock: %v", err)
	}
}

// slowManifestGH has a manifest search that answers only when its context ends.
type slowManifestGH struct{ *fakeGH }

func (f *slowManifestGH) SearchManifests(ctx context.Context, max int) ([]string, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// A discovery source that does not answer within DiscoveryTimeout is left
// out: the run goes on with the other sources, but does not prune.
func TestDiscoveryTimeoutIsPartial(t *testing.T) {
	ix, gh, st := setup(t)
	ctx := context.Background()
	if _, err := ix.Run(ctx, Options{}); err != nil {
		t.Fatal(err)
	}

	ix.GH = &slowManifestGH{fakeGH: gh}
	ix.DiscoveryTimeout = 50 * time.Millisecond
	ix.Prune = true
	gh.search = []string{"acme/omalib"}
	var stages []string
	stats, err := ix.Run(ctx, Options{Progress: func(p Progress) {
		if len(stages) == 0 || stages[len(stages)-1] != p.Stage {
			stages = append(stages, p.Stage)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 0 {
		t.Errorf("stats = %+v", stats)
	}
	if _, err := st.GetApp(ctx, "acme/omaphoto"); err != nil {
		t.Errorf("pruned after a timed-out discovery: %v", err)
	}
	if want := []string{StageDiscover, StageIndex}; !slices.Equal(stages, want) {
		t.Errorf("stages = %v, want %v", stages, want)
	}
}
