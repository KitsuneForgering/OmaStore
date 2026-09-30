package xdg

import (
	"path/filepath"
	"testing"
)

func TestResolveDefaults(t *testing.T) {
	t.Setenv("HOME", "/home/test")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "relative/ignored")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/42")

	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		p.DB:           "/home/test/.local/share/omastore/omastore.db",
		p.ReposDir:     "/home/test/.cache/omastore/repos",
		p.BinDir:       "/home/test/.local/bin",
		p.Applications: "/home/test/.local/share/applications",
		p.Socket:       "/run/user/42/omastore.sock",
		p.OmarchyTheme: "/home/test/.local/state/omarchy/current/theme",
	}
	for got, w := range want {
		if got != w {
			t.Errorf("got %q, want %q", got, w)
		}
	}
}

func TestResolveEnv(t *testing.T) {
	d := t.TempDir()
	t.Setenv("HOME", d)
	t.Setenv("XDG_DATA_HOME", filepath.Join(d, "data"))
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.AppsDir != filepath.Join(d, "data", "omastore", "apps") {
		t.Errorf("AppsDir = %q", p.AppsDir)
	}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
}
