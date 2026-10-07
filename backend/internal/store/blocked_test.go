package store

import (
	"context"
	"testing"
)

// A blocked app leaves the catalog and the categories, stays listed for those
// who have it installed, and its detail says why. Names ignore case.
func TestBlocklist(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	seed(t, s, "acme/good", 10, "Graphics", true)
	seed(t, s, "acme/bad", 5, "Graphics", true)
	if err := s.SaveInstall(ctx, Install{FullName: "acme/bad", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBlocklist(ctx, map[string]string{"ACME/Bad": "ships malware"}); err != nil {
		t.Fatal(err)
	}
	items, _ := s.ListApps(ctx, Filter{})
	if len(items) != 1 || items[0].FullName != "acme/good" {
		t.Errorf("catalog = %+v, want only acme/good", items)
	}
	cats, _ := s.Categories(ctx)
	if len(cats) != 1 || cats[0].Count != 1 {
		t.Errorf("categories = %+v", cats)
	}
	inst, _ := s.ListApps(ctx, Filter{InstalledOnly: true})
	if len(inst) != 1 || !inst[0].Blocked || inst[0].BlockedReason != "ships malware" {
		t.Errorf("installed = %+v", inst)
	}
	d, err := s.GetApp(ctx, "acme/bad")
	if err != nil || !d.Blocked || d.BlockedReason != "ships malware" {
		t.Errorf("detail = %+v, %v", d, err)
	}
	// A new list replaces the old one.
	if err := s.SetBlocklist(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if items, _ := s.ListApps(ctx, Filter{}); len(items) != 2 {
		t.Errorf("after unblocking: %d apps", len(items))
	}
}
