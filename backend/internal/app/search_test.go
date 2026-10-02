package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

func testApp(t *testing.T) *App {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &App{Store: st}
}

func put(t *testing.T, a *App, repo, name, summary, category string, topics []string, stars int, installable bool) {
	t.Helper()
	err := a.Store.SaveIndexed(context.Background(),
		store.Repo{FullName: repo, Stars: stars, Topics: topics, IndexedAt: time.Now()},
		store.App{Name: name, Summary: summary, Category: category, Score: float64(stars), Installable: installable},
		nil)
	if err != nil {
		t.Fatal(err)
	}
}

func names(items []store.ListItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.FullName)
	}
	return out
}

func TestListAppsRankedSearch(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	put(t, a, "a/popular", "Popular", "Generic tool with many stars", "Utility", nil, 5000, true)
	put(t, a, "a/cal", "Cal", "Terminal calendar", "Office", []string{"calendar"}, 3, true)
	put(t, a, "a/calweb", "CalWeb", "Calendar sync server", "Network", []string{"calendar"}, 10, false)

	// The most relevant comes first, even with fewer stars; non-installable ones are left out.
	got, err := a.ListApps(ctx, store.Filter{Query: "calendar"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FullName != "a/cal" {
		t.Errorf("search = %v", names(got))
	}
	all, _ := a.ListApps(ctx, store.Filter{Query: "calendar", All: true})
	if len(all) != 2 {
		t.Errorf("All = %v", names(all))
	}
	office, _ := a.ListApps(ctx, store.Filter{Query: "calendar", All: true, Category: "Network"})
	if len(office) != 1 || office[0].FullName != "a/calweb" {
		t.Errorf("category = %v", names(office))
	}
	page, _ := a.ListApps(ctx, store.Filter{Query: "calendar", All: true, Offset: 1, Limit: 1})
	if len(page) != 1 {
		t.Errorf("pagination = %v", names(page))
	}
	none, _ := a.ListApps(ctx, store.Filter{Query: "xyzzy"})
	if none == nil || len(none) != 0 {
		t.Errorf("no results must be an empty list, not nil: %v", none)
	}
	// Without a query, the score order still applies.
	plain, _ := a.ListApps(ctx, store.Filter{})
	if plain[0].FullName != "a/popular" {
		t.Errorf("without query = %v", names(plain))
	}

	// The index is rebuilt when the catalog changes.
	put(t, a, "a/newcal", "NewCal", "Calendar for Omarchy", "Office", []string{"calendar"}, 1, true)
	got, _ = a.ListApps(ctx, store.Filter{Query: "newcal"})
	if len(got) != 1 || got[0].FullName != "a/newcal" {
		t.Errorf("after change = %v", names(got))
	}
}

func TestSimilarApps(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	put(t, a, "a/mon1", "Mon1", "Monitor layout for Hyprland displays", "System", []string{"monitor", "display"}, 5, true)
	put(t, a, "a/mon2", "Mon2", "Arrange monitors and displays", "System", []string{"monitor", "display"}, 3, true)
	put(t, a, "a/cal", "Cal", "Calendar", "Office", []string{"calendar"}, 3, true)
	got, err := a.Similar(ctx, "a/mon1", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FullName != "a/mon2" {
		t.Errorf("parecidos = %v", names(got))
	}
	if _, err := a.Similar(ctx, "x/y", 5); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}
