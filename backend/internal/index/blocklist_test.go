package index

import (
	"context"
	"errors"
	"testing"

	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

func TestParseBlocklist(t *testing.T) {
	got := ParseBlocklist("# header\n\nacme/bad  # ships malware\nacme/quiet\nnot a repo # x\n")
	if len(got) != 2 || got["acme/bad"] != "ships malware" || got["acme/quiet"] != "" {
		t.Errorf("parsed = %v", got)
	}
}

// Every run reads the curated list; when it cannot, the previous one stays.
func TestRunRefreshesBlocklist(t *testing.T) {
	ix, gh, st := setup(t)
	ctx := context.Background()
	gh.repos["store/meta"] = &fakeRepo{extra: map[string]string{"blocklist.txt": "acme/omalib # abandoned, broken\n"}}
	ix.Blocklist = "store/meta:blocklist.txt"
	if _, err := ix.Run(ctx, Options{}); err != nil {
		t.Fatal(err)
	}
	if reason, blocked, _ := st.Blocked(ctx, "acme/omalib"); !blocked || reason != "abandoned, broken" {
		t.Fatalf("blocked = %v %q", blocked, reason)
	}
	items, _ := st.ListApps(ctx, store.Filter{All: true})
	for _, it := range items {
		if it.FullName == "acme/omalib" {
			t.Error("a blocked app is still in the catalog")
		}
	}
	// Unreadable list: keep the last one.
	ix.Blocklist = "store/missing:blocklist.txt"
	if _, err := ix.Run(ctx, Options{Only: []string{"acme/omaphoto"}}); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, blocked, _ := st.Blocked(ctx, "acme/omalib"); !blocked {
		t.Error("a failed read dropped the blocklist")
	}
}
