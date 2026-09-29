package install

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrNoExecutable indica que nada instalável como programa foi encontrado.
var ErrNoExecutable = errors.New("nenhum executável encontrado no pacote")

// fileKind lê só o cabeçalho do arquivo (nunca o executa).
func fileKind(p string) (elf, script bool) {
	f, err := os.Open(p)
	if err != nil {
		return false, false
	}
	defer f.Close()
	var b [4]byte
	n, _ := io.ReadFull(f, b[:])
	h := b[:n]
	return bytes.HasPrefix(h, []byte("\x7fELF")), bytes.HasPrefix(h, []byte("#!"))
}

func normalize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == ' ' || r == '.' {
			return -1
		}
		return r
	}, strings.ToLower(s))
}

// isLibrary descarta bibliotecas compartilhadas, que também são ELF.
func isLibrary(base string) bool {
	return strings.HasSuffix(base, ".so") || strings.Contains(base, ".so.")
}

// FindExecutable escolhe o executável principal em dir: prefere o nome do
// repositório, depois bin/ e usr/bin/, depois ELF sobre scripts. Retorna o
// caminho absoluto.
func FindExecutable(dir, repoName string) (string, error) {
	repo := normalize(repoName)
	best, bestScore := "", -1
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		base := path.Base(rel)
		if isLibrary(base) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		elf, script := fileKind(p)
		if !elf && !script {
			return nil
		}
		// Scripts só contam se forem marcados executáveis.
		if script && info.Mode().Perm()&0o111 == 0 {
			return nil
		}
		lrel := strings.ToLower(rel)
		if strings.Contains(lrel, "/lib/") || strings.HasPrefix(lrel, "lib/") ||
			strings.Contains(lrel, "/libexec/") || strings.Contains(lrel, "/share/") {
			return nil
		}
		nb := normalize(base)
		if strings.EqualFold(path.Ext(base), ".appimage") {
			nb = normalize(strings.TrimSuffix(base, path.Ext(base)))
		}
		score := 0
		switch {
		case nb == repo:
			score += 100
		case strings.Contains(nb, repo) || strings.Contains(repo, nb):
			score += 40
		}
		if strings.HasPrefix(lrel, "usr/bin/") || strings.HasPrefix(lrel, "bin/") || strings.Contains(lrel, "/bin/") {
			score += 30
		}
		if elf {
			score += 20
		} else {
			score += 5
		}
		if info.Mode().Perm()&0o111 != 0 {
			score += 10
		}
		score -= strings.Count(rel, "/")
		if score > bestScore || (score == bestScore && rel < best) {
			best, bestScore = rel, score
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if best == "" {
		return "", ErrNoExecutable
	}
	return filepath.Join(dir, filepath.FromSlash(best)), nil
}
