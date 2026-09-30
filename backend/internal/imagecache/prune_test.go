package imagecache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrune(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{Dir: dir}
	now := time.Now()
	write := func(name string, size int, age time.Duration) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, make([]byte, size), 0o644)
		os.Chtimes(p, now.Add(-age), now.Add(-age))
		return p
	}
	stale := write("stale.png", 10, 40*24*time.Hour)
	oldTmp := write("x.tmp-1", 10, 40*24*time.Hour)
	newTmp := write("y.tmp-2", 500, 0)
	older := write("older.png", 100, 2*time.Hour)
	recent := write("recent.png", 100, time.Hour)

	n, err := c.Prune(30*24*time.Hour, 150)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("removed %d, want 3", n)
	}
	for p, want := range map[string]bool{stale: false, oldTmp: false, older: false, recent: true, newTmp: true} {
		_, err := os.Stat(p)
		if (err == nil) != want {
			t.Errorf("%s: exists=%v, want %v", filepath.Base(p), err == nil, want)
		}
	}
}

// A cache hit refreshes the mtime, so images in use are not pruned as old.
func TestCachedHitRefreshesMtime(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{Dir: dir}
	k := key("https://x/a.png")
	p := filepath.Join(dir, k+".png")
	os.WriteFile(p, pngHeader, 0o644)
	old := time.Now().Add(-40 * 24 * time.Hour)
	os.Chtimes(p, old, old)
	if got := c.cached(k); got != p {
		t.Fatalf("cached = %q", got)
	}
	if st, _ := os.Stat(p); st.ModTime().Before(time.Now().Add(-time.Minute)) {
		t.Error("mtime not refreshed")
	}
}
