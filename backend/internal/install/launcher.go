package install

import (
	"io"
	"io/fs"
	"os"
	"strings"
)

// launcherMarker identifica os lançadores gerados pelo OmaStore.
const launcherMarker = "# omastore-launcher"

// shellQuote põe s entre aspas simples para o sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// launcherScript gera o lançador de ~/.local/bin. Usamos um script em vez de
// symlink porque muitos apps localizam seus arquivos a partir de $0 / argv[0]
// (ex.: "$(dirname "$0")/../lib"), o que quebra quando $0 é o link.
func launcherScript(repo, target string) string {
	return "#!/bin/sh\n" + launcherMarker + " " + repo + "\n" +
		"exec " + shellQuote(target) + ` "$@"` + "\n"
}

// launcherTarget lê um lançador nosso e retorna o executável alvo; ok é false
// se p não for um lançador do OmaStore.
func launcherTarget(p string) (target string, ok bool) {
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 64<<10 {
		return "", false
	}
	f, err := os.Open(p)
	if err != nil {
		return "", false
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, 64<<10))
	lines := strings.Split(string(b), "\n")
	if len(lines) < 3 || lines[0] != "#!/bin/sh" || !strings.HasPrefix(lines[1], launcherMarker+" ") {
		return "", false
	}
	rest, found := strings.CutPrefix(lines[2], "exec '")
	if !found {
		return "", false
	}
	rest, found = strings.CutSuffix(rest, `' "$@"`)
	if !found {
		return "", false
	}
	return strings.ReplaceAll(rest, `'\''`, "'"), true
}

// isOurLink diz se p é um lançador (ou symlink, de versões antigas) que
// aponta para dentro de ownDir.
func isOurLink(p, ownDir string) bool {
	st, err := os.Lstat(p)
	if err != nil {
		return false
	}
	if st.Mode()&fs.ModeSymlink != 0 {
		t, err := os.Readlink(p)
		return err == nil && within(ownDir, t)
	}
	t, ok := launcherTarget(p)
	return ok && (ownDir == "" || within(ownDir, t))
}
