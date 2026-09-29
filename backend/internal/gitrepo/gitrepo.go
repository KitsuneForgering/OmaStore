// Package gitrepo keeps shallow clones (Depth 1) of repositories in a cache and
// finds files in them that the API does not serve well (icon, screenshots).
package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

// Cache keeps clones in Dir/<owner>__<repo>.
type Cache struct {
	Dir string
	// URL builds the clone URL; the default is https://github.com/<full>.git.
	// Tests point it at local repositories.
	URL func(fullName string) string
}

// Path returns the clone directory of fullName.
func (c *Cache) Path(fullName string) string {
	return filepath.Join(c.Dir, strings.ReplaceAll(fullName, "/", "__"))
}

func (c *Cache) url(fullName string) string {
	if c.URL != nil {
		return c.URL(fullName)
	}
	return "https://github.com/" + fullName + ".git"
}

// Sync ensures an up-to-date shallow clone of fullName and returns the directory
// and the HEAD SHA. If a clone exists, it does a shallow fetch and reset; if that
// fails, it deletes it and clones again.
func (c *Cache) Sync(ctx context.Context, fullName string) (dir, sha string, err error) {
	dir = c.Path(fullName)
	if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
		sha, err = update(ctx, dir)
		if err == nil {
			return dir, sha, nil
		}
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", "", fmt.Errorf("clean clone of %s: %w", fullName, err)
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create repo cache: %w", err)
	}
	r, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:          c.url(fullName),
		Depth:        1,
		SingleBranch: true,
		Tags:         git.NoTags,
	})
	if err != nil {
		os.RemoveAll(dir)
		return "", "", fmt.Errorf("clonar %s: %w", fullName, err)
	}
	head, err := r.Head()
	if err != nil {
		return "", "", fmt.Errorf("HEAD of %s: %w", fullName, err)
	}
	return dir, head.Hash().String(), nil
}

// update does a shallow fetch of the current branch and resets the worktree to it.
func update(ctx context.Context, dir string) (string, error) {
	r, err := git.PlainOpen(dir)
	if err != nil {
		return "", err
	}
	head, err := r.Head()
	if err != nil {
		return "", err
	}
	if !head.Name().IsBranch() {
		return "", errors.New("HEAD destacado")
	}
	branch := head.Name().Short()
	remoteRef := plumbing.NewRemoteReferenceName("origin", branch)
	spec := config.RefSpec(fmt.Sprintf("+%s:%s", head.Name(), remoteRef))
	err = r.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{spec},
		Depth:      1,
		Tags:       git.NoTags,
		Force:      true,
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return "", err
	}
	ref, err := r.Reference(remoteRef, true)
	if err != nil {
		return "", err
	}
	wt, err := r.Worktree()
	if err != nil {
		return "", err
	}
	if err := wt.Reset(&git.ResetOptions{Commit: ref.Hash(), Mode: git.HardReset}); err != nil {
		return "", err
	}
	return ref.Hash().String(), nil
}

// skipDirs are not walked when listing files.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "target": true,
	"build": true, "dist": true, ".venv": true, "__pycache__": true,
}

// maxDepth limits the listing depth.
const maxDepth = 6

// ListFiles lists the regular files of dir as relative paths with "/".
// Symlinks are neither followed nor listed.
func ListFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			if rel != "." && (skipDirs[d.Name()] || strings.Count(rel, string(filepath.Separator)) >= maxDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	return out, nil
}
