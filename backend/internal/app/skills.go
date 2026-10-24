package app

import (
	"context"
	"os"
	"path/filepath"
)

// InstallSkills uses only the skills bundled alongside the running daemon.
func (a *App) InstallSkills(ctx context.Context) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return 0, err
	}
	base := filepath.Dir(filepath.Dir(exe))
	candidates := []string{filepath.Join(base, "share", "skills"), filepath.Join(base, "share", "omastore", "skills"), filepath.Join(base, "skills")}
	for _, src := range candidates {
		if st, err := os.Stat(src); err == nil && st.IsDir() {
			return a.Installer.InstallSkills(ctx, src)
		}
	}
	return a.Installer.InstallSkills(ctx, candidates[1])
}
