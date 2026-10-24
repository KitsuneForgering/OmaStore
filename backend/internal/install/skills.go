package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KitsuneForgering/OmaStore/backend/internal/flock"
)

// InstallSkills copies the bundled author skills into existing coding agents.
// An explicit request also works when automatic skill installation was disabled.
func (in *Installer) InstallSkills(ctx context.Context, src string) (int, error) {
	unlock, err := flock.TryLock(filepath.Join(in.SelfRoot(), ".update.lock"))
	if errors.Is(err, flock.ErrLocked) {
		return 0, ErrBusy
	}
	if err != nil {
		return 0, fmt.Errorf("lock skills: %w", err)
	}
	defer unlock()
	if err := regularTree(src); err != nil {
		return 0, fmt.Errorf("bundled skills unavailable: %w", err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return 0, err
	}
	dirs := in.skillDirs()
	if len(dirs) == 0 {
		return 0, fmt.Errorf("no coding agent directory found; open your coding agent once and try again")
	}
	count := 0
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return count, err
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), "omastore-") {
				continue
			}
			if err := ctx.Err(); err != nil {
				return count, err
			}
			if _, err := os.Stat(filepath.Join(src, e.Name(), "SKILL.md")); err != nil {
				return count, err
			}
			dest := filepath.Join(dir, e.Name())
			tmp := dest + ".omastore-new-" + randSuffix()
			if err := os.CopyFS(tmp, os.DirFS(filepath.Join(src, e.Name()))); err != nil {
				os.RemoveAll(tmp)
				return count, err
			}
			if err := os.WriteFile(filepath.Join(tmp, skillMarker), nil, 0o644); err != nil {
				os.RemoveAll(tmp)
				return count, err
			}
			// Keep existing copies as backups, including local edits to managed skills.
			backup := ""
			if _, err := os.Lstat(dest); err == nil {
				root := filepath.Join(in.Paths.DataDir, "skill-backups")
				if err := os.MkdirAll(root, 0o755); err != nil {
					os.RemoveAll(tmp)
					return count, err
				}
				dir, err := os.MkdirTemp(root, e.Name()+".")
				if err != nil {
					os.RemoveAll(tmp)
					return count, err
				}
				backup = filepath.Join(dir, e.Name())
				if err := os.Rename(dest, backup); err != nil {
					os.RemoveAll(tmp)
					os.Remove(dir)
					return count, err
				}
			} else if !os.IsNotExist(err) {
				os.RemoveAll(tmp)
				return count, err
			}
			if err := os.Rename(tmp, dest); err != nil {
				os.RemoveAll(tmp)
				if backup != "" {
					err = errors.Join(err, os.Rename(backup, dest))
				}
				return count, fmt.Errorf("install skill %s: %w", dest, err)
			}
			count++
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("no bundled author skills found")
	}
	return count, nil
}
