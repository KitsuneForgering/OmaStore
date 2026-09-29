// Package gitrepo mantém clones rasos (Depth 1) de repositórios em cache e
// localiza neles arquivos que a API não entrega bem (ícone, screenshots).
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

// Cache guarda clones em Dir/<owner>__<repo>.
type Cache struct {
	Dir string
	// URL monta a URL de clone; o default é https://github.com/<full>.git.
	// Os testes apontam para repositórios locais.
	URL func(fullName string) string
}

// Path retorna o diretório do clone de fullName.
func (c *Cache) Path(fullName string) string {
	return filepath.Join(c.Dir, strings.ReplaceAll(fullName, "/", "__"))
}

func (c *Cache) url(fullName string) string {
	if c.URL != nil {
		return c.URL(fullName)
	}
	return "https://github.com/" + fullName + ".git"
}

// Sync garante um clone raso atualizado de fullName e retorna o diretório
// e o SHA do HEAD. Se já houver clone, faz fetch raso e reset; se isso falhar,
// apaga e clona de novo.
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
		return "", "", fmt.Errorf("limpar clone de %s: %w", fullName, err)
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", "", fmt.Errorf("criar cache de repos: %w", err)
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
		return "", "", fmt.Errorf("HEAD de %s: %w", fullName, err)
	}
	return dir, head.Hash().String(), nil
}

// update faz fetch raso do branch atual e reseta a worktree para ele.
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

// skipDirs não são percorridos ao listar arquivos.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "target": true,
	"build": true, "dist": true, ".venv": true, "__pycache__": true,
}

// maxDepth limita a profundidade da listagem.
const maxDepth = 6

// ListFiles lista os arquivos regulares de dir como caminhos relativos com "/".
// Symlinks não são seguidos nem listados.
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
		return nil, fmt.Errorf("listar %s: %w", dir, err)
	}
	return out, nil
}
