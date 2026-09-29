package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/notify"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/xdg"
)

// isolate aponta HOME/XDG para um diretório temporário e desliga a rede.
func isolate(t *testing.T) xdg.Paths {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GITHUB_TOKEN", "test")
	p, err := xdg.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func seedStore(t *testing.T, p xdg.Paths) {
	t.Helper()
	p.Ensure()
	st, err := store.Open(context.Background(), p.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	err = st.SaveIndexed(context.Background(),
		store.Repo{FullName: "acme/omaphoto", Stars: 7, LatestTag: "v1", Topics: []string{"photo"}},
		store.App{Name: "OmaPhoto", Summary: "Editor de fotos", Category: "Graphics", Installable: true, Score: 7},
		[]store.Asset{{Tag: "v1", Name: "omaphoto-linux-amd64", URL: "https://x", Format: "binary", Arch: "amd64"}})
	if err != nil {
		t.Fatal(err)
	}
}

func runCLI(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = run(context.Background(), args, &o, &e)
	return code, o.String(), e.String()
}

func TestUsage(t *testing.T) {
	isolate(t)
	if code, _, errOut := runCLI(); code != 2 || !strings.Contains(errOut, "uso:") {
		t.Errorf("sem args: %d %q", code, errOut)
	}
	if code, _, _ := runCLI("nope"); code != 2 {
		t.Errorf("comando desconhecido: %d", code)
	}
	if code, out, _ := runCLI("help"); code != 0 || !strings.Contains(out, "comandos:") {
		t.Errorf("help: %d", code)
	}
	if code, _, _ := runCLI("show"); code != 2 {
		t.Errorf("show sem repo: %d", code)
	}
	if code, _, _ := runCLI("install"); code != 2 {
		t.Errorf("install sem repo: %d", code)
	}
}

func TestListAndShow(t *testing.T) {
	p := isolate(t)
	seedStore(t, p)

	code, out, errOut := runCLI("list")
	if code != 0 || !strings.Contains(out, "acme/omaphoto") || !strings.Contains(out, "Graphics") {
		t.Errorf("list: %d %q %q", code, out, errOut)
	}
	code, out, _ = runCLI("list", "--json", "--category", "System")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Errorf("list vazia: %d %q", code, out)
	}
	code, out, _ = runCLI("show", "--json", "acme/omaphoto")
	var d store.AppDetail
	if code != 0 || json.Unmarshal([]byte(out), &d) != nil || d.Name != "OmaPhoto" || len(d.Assets) != 1 {
		t.Errorf("show json: %d %q", code, out)
	}
	code, out, _ = runCLI("show", "acme/omaphoto")
	if code != 0 || !strings.Contains(out, "OmaPhoto (acme/omaphoto)") || !strings.Contains(out, "sem checksum") {
		t.Errorf("show: %q", out)
	}
	if code, _, errOut := runCLI("show", "x/y"); code != 1 || !strings.Contains(errOut, "não encontrado") {
		t.Errorf("show inexistente: %d %q", code, errOut)
	}
	if code, out, _ := runCLI("categories"); code != 0 || !strings.Contains(out, "Graphics") {
		t.Errorf("categories: %q", out)
	}
	if code, out, _ := runCLI("update"); code != 0 || !strings.Contains(out, "nenhum app instalado") {
		t.Errorf("update: %q", out)
	}
	if code, _, errOut := runCLI("uninstall", "acme/omaphoto"); code != 1 || !strings.Contains(errOut, "não está instalado") {
		t.Errorf("uninstall: %d %q", code, errOut)
	}
}

func TestLintManifest(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", "icon.svg"), []byte("<svg/>"), 0o644)
	os.WriteFile(filepath.Join(dir, "omastore.toml"), []byte(`
name = "Meu App"
categories = ["Graphics"]
icon = "assets/icon.svg"
[linux.x86_64]
asset = "meuapp-{version}-x86_64-linux.tar.gz"
exec = "bin/meuapp"
`), 0o644)
	code, out, errOut := runCLI("lint-manifest", dir)
	if code != 0 || !strings.Contains(out, "ok:") || !strings.Contains(out, "amd64") {
		t.Errorf("válido: %d %q %q", code, out, errOut)
	}

	os.WriteFile(filepath.Join(dir, "omastore.toml"), []byte(`
icone = "typo.svg"
`), 0o644)
	if code, out, _ := runCLI("lint-manifest", dir); code != 1 || !strings.Contains(out, "icone") {
		t.Errorf("campo desconhecido: %d %q", code, out)
	}

	os.WriteFile(filepath.Join(dir, "omastore.toml"), []byte(`
icon = "falta.svg"
screenshots = ["../fora.png"]
`), 0o644)
	code, out, _ = runCLI("lint-manifest", filepath.Join(dir, "omastore.toml"))
	if code != 1 || !strings.Contains(out, "fora.png") {
		t.Errorf("caminho inválido: %d %q", code, out)
	}
	// Com o diretório, detecta arquivo inexistente.
	code, out, _ = runCLI("lint-manifest", dir)
	if code != 1 || !strings.Contains(out, "falta.svg") {
		t.Errorf("arquivo inexistente: %d %q", code, out)
	}
	if code, _, _ := runCLI("lint-manifest", filepath.Join(dir, "nao-existe")); code != 1 {
		t.Errorf("inexistente: %d", code)
	}
}

type fakeNotifier struct{ sent *[]string }

func (f fakeNotifier) Notify(ctx context.Context, summary, body string) error {
	*f.sent = append(*f.sent, summary+"|"+body)
	return nil
}

func TestUpdateCheck(t *testing.T) {
	p := isolate(t)
	t.Setenv("XDG_STATE_HOME", "")
	seedStore(t, p)
	var sent []string
	old := newNotifier
	newNotifier = func() notify.Notifier { return fakeNotifier{&sent} }
	defer func() { newNotifier = old }()

	if code, out, _ := runCLI("update", "--check"); code != 0 || !strings.Contains(out, "tudo atualizado") {
		t.Errorf("sem instalados: %d %q", code, out)
	}
	st, _ := store.Open(context.Background(), p.DB)
	st.SaveInstall(context.Background(), store.Install{FullName: "acme/omaphoto", Version: "v0", InstalledAt: time.Now()})
	st.Close()

	code, out, _ := runCLI("update", "--check", "--notify")
	if code != 0 || !strings.Contains(out, "acme/omaphoto: v0 → v1") || len(sent) != 1 ||
		!strings.Contains(sent[0], "1 atualização") {
		t.Errorf("com atualização: %d %q %v", code, out, sent)
	}
	// Mesmo conjunto: não notifica de novo.
	if _, out, _ := runCLI("update", "--check", "--notify"); len(sent) != 1 || !strings.Contains(out, "já notificado") {
		t.Errorf("repetiu: %q %v", out, sent)
	}
	if code, _, _ := runCLI("update", "--notify"); code != 2 {
		t.Errorf("--notify sem --check: %d", code)
	}
}
