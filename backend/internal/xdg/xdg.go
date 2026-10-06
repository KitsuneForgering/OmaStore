// Package xdg resolves the XDG Base Directory Specification base directories
// used by OmaStore, applying the defaults from the specification.
package xdg

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const appName = "omastore"

// Home returns the user's home directory.
func Home() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return h, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve HOME: %w", err)
	}
	return h, nil
}

// envDir returns the value of env if it is an absolute path; otherwise,
// $HOME/<fallback>. The specification says relative paths must be ignored.
func envDir(env, fallback string) (string, error) {
	if v := os.Getenv(env); v != "" && filepath.IsAbs(v) {
		return v, nil
	}
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, fallback), nil
}

// DataHome returns $XDG_DATA_HOME (default ~/.local/share).
func DataHome() (string, error) { return envDir("XDG_DATA_HOME", ".local/share") }

// CacheHome returns $XDG_CACHE_HOME (default ~/.cache).
func CacheHome() (string, error) { return envDir("XDG_CACHE_HOME", ".cache") }

// StateHome returns $XDG_STATE_HOME (default ~/.local/state).
func StateHome() (string, error) { return envDir("XDG_STATE_HOME", ".local/state") }

// ConfigHome returns $XDG_CONFIG_HOME (default ~/.config).
func ConfigHome() (string, error) { return envDir("XDG_CONFIG_HOME", ".config") }

// RuntimeDir returns $XDG_RUNTIME_DIR. The specification defines no default;
// we use /run/user/<uid> when it exists and, as a last resort, the temp directory.
func RuntimeDir() string {
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" && filepath.IsAbs(v) {
		return v
	}
	d := filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	if st, err := os.Stat(d); err == nil && st.IsDir() {
		return d
	}
	return os.TempDir()
}

// Paths groups every path used by OmaStore.
type Paths struct {
	Home         string // $HOME
	DataDir      string // $XDG_DATA_HOME/omastore
	CacheDir     string // $XDG_CACHE_HOME/omastore
	StateDir     string // $XDG_STATE_HOME/omastore
	DB           string // $XDG_DATA_HOME/omastore/omastore.db
	AppsDir      string // $XDG_DATA_HOME/omastore/apps
	ReposDir     string // $XDG_CACHE_HOME/omastore/repos
	ImagesDir    string // $XDG_CACHE_HOME/omastore/images
	BinDir       string // ~/.local/bin
	Applications string // $XDG_DATA_HOME/applications
	Icons        string // $XDG_DATA_HOME/icons/hicolor
	UserUnits    string // $XDG_CONFIG_HOME/systemd/user
	Socket       string // $XDG_RUNTIME_DIR/omastore.sock
	OmarchyTheme string // $XDG_STATE_HOME/omarchy/current/theme
}

// Resolve computes the paths from the current environment.
func Resolve() (Paths, error) {
	home, err := Home()
	if err != nil {
		return Paths{}, err
	}
	data, err := DataHome()
	if err != nil {
		return Paths{}, err
	}
	cache, err := CacheHome()
	if err != nil {
		return Paths{}, err
	}
	state, err := StateHome()
	if err != nil {
		return Paths{}, err
	}
	config, err := ConfigHome()
	if err != nil {
		return Paths{}, err
	}
	p := Paths{
		Home:         home,
		DataDir:      filepath.Join(data, appName),
		CacheDir:     filepath.Join(cache, appName),
		StateDir:     filepath.Join(state, appName),
		BinDir:       filepath.Join(home, ".local", "bin"),
		Applications: filepath.Join(data, "applications"),
		Icons:        filepath.Join(data, "icons", "hicolor"),
		UserUnits:    filepath.Join(config, "systemd", "user"),
		Socket:       filepath.Join(RuntimeDir(), appName+".sock"),
		OmarchyTheme: filepath.Join(state, "omarchy", "current", "theme"),
	}
	p.DB = filepath.Join(p.DataDir, appName+".db")
	p.AppsDir = filepath.Join(p.DataDir, "apps")
	p.ReposDir = filepath.Join(p.CacheDir, "repos")
	p.ImagesDir = filepath.Join(p.CacheDir, "images")
	return p, nil
}

// Ensure creates OmaStore's own directories.
func (p Paths) Ensure() error {
	for _, d := range []string{p.DataDir, p.CacheDir, p.AppsDir, p.ReposDir, p.ImagesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}
