package install

import (
	"bytes"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopEscaping(t *testing.T) {
	d := Desktop{
		Name:       "Evil\nExec=rm -rf ~",
		Comment:    `back\slash	tab`,
		Exec:       `/home/u/my apps/$HOME/a"b%c`,
		Icon:       "omastore-a-b",
		Categories: []string{"Graphics", "Bad;Cat\n"},
		Repo:       "a/b",
		Version:    "v1\n[Desktop Action x]",
		Terminal:   true,
	}
	out := d.Render()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	keys := map[string]int{}
	for _, l := range lines[1:] {
		k, _, ok := strings.Cut(l, "=")
		if !ok {
			t.Errorf("linha sem chave: %q", l)
		}
		keys[k]++
	}
	groups := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "[") {
			groups++
		}
	}
	if keys["Exec"] != 1 || lines[0] != "[Desktop Entry]" || groups != 1 {
		t.Errorf("injeção:\n%s", out)
	}
	for _, want := range []string{
		"Name=Evil Exec=rm -rf ~\n",
		`Comment=back\\slash tab` + "\n",
		`Exec="/home/u/my apps/\\$HOME/a\\"b%%c"` + "\n",
		"Categories=Graphics;BadCat;\n",
		"Terminal=true\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q em:\n%s", want, out)
		}
	}
	if q := quoteExecArg("/simple/path"); q != "/simple/path" {
		t.Errorf("sem aspas: %q", q)
	}
}

func TestParseChecksums(t *testing.T) {
	h1, h2 := strings.Repeat("a", 64), strings.Repeat("B", 64)
	data := "# comentário\n" + h1 + "  app.tar.gz\n" + h2 + " *./dist/other.zip\nSHA256 (bsd.tgz) = " + h1 + "\n"
	cases := map[string]string{"app.tar.gz": h1, "other.zip": strings.ToLower(h2), "bsd.tgz": h1, "missing": ""}
	for name, want := range cases {
		if got := ParseChecksums([]byte(data), name); got != want {
			t.Errorf("%s: %q", name, got)
		}
	}
	if got := ParseChecksums([]byte(h1+"\n"), "whatever"); got != h1 {
		t.Errorf("arquivo .sha256 de um hash só: %q", got)
	}
	if got := ParseChecksums([]byte("nothex  app\n"), "app"); got != "" {
		t.Errorf("lixo: %q", got)
	}
}

func TestVerify(t *testing.T) {
	if err := verify(strings.Repeat("a", 64), strings.Repeat("a", 64), ""); err != nil {
		t.Error(err)
	}
	if err := verify(strings.Repeat("b", 128), "", strings.Repeat("b", 128)); err != nil {
		t.Error(err)
	}
	if err := verify(strings.Repeat("a", 64), strings.Repeat("c", 64), ""); err == nil {
		t.Error("deveria falhar")
	}
	if err := verify("abc", "", ""); err == nil {
		t.Error("hash inválido deveria falhar")
	}
}

func TestPrepareIcon(t *testing.T) {
	out, ext, dir, err := prepareIcon(pngBytes(256, 256))
	if err != nil || ext != ".png" || dir != "256x256" {
		t.Fatalf("256: %s %s %v", ext, dir, err)
	}
	if !bytes.Equal(out, pngBytes(256, 256)) {
		t.Error("PNG já no tamanho certo deveria ser copiado")
	}
	out, _, dir, err = prepareIcon(pngBytes(300, 150))
	if err != nil || dir != "256x256" {
		t.Fatalf("300x150: %s %v", dir, err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != 256 || cfg.Height != 256 {
		t.Errorf("redimensionado: %+v %v", cfg, err)
	}
	if _, ext, dir, _ := prepareIcon([]byte(`<?xml version="1.0"?><svg/>`)); ext != ".svg" || dir != "scalable" {
		t.Errorf("svg: %s %s", ext, dir)
	}
	if _, _, _, err := prepareIcon([]byte("<html>not an icon</html>")); err == nil {
		t.Error("lixo aceito")
	}
	if _, _, _, err := prepareIcon(pngBytes(8, 8)); err == nil {
		t.Error("ícone minúsculo aceito")
	}
}

func TestTxRollbackAndCommit(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old")
	os.WriteFile(old, []byte("antigo"), 0o644)
	fresh := filepath.Join(dir, "new")

	var tr tx
	tr.prepare(old)
	os.WriteFile(old, []byte("novo"), 0o644)
	tr.prepare(fresh)
	os.WriteFile(fresh, []byte("x"), 0o644)
	if err := tr.rollback(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(old); string(b) != "antigo" {
		t.Errorf("old = %q", b)
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Error("new deveria sumir")
	}

	tr = tx{}
	tr.prepare(old)
	os.WriteFile(old, []byte("novo"), 0o644)
	tr.commit()
	if b, _ := os.ReadFile(old); string(b) != "novo" {
		t.Errorf("commit: %q", b)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*bak*")); len(m) != 0 {
		t.Errorf("backups: %v", m)
	}
}

func TestLauncher(t *testing.T) {
	dir := t.TempDir()
	target := "/home/u/apps/it's here/bin/app"
	p := filepath.Join(dir, "app")
	os.WriteFile(p, []byte(launcherScript("a/b", target)), 0o755)
	got, ok := launcherTarget(p)
	if !ok || got != target {
		t.Errorf("target = %q %v", got, ok)
	}
	if !isOurLink(p, "/home/u/apps") || isOurLink(p, "/outro") {
		t.Error("isOurLink")
	}
	os.WriteFile(p, []byte("#!/bin/sh\nexec '/x' \"$@\"\n"), 0o755)
	if _, ok := launcherTarget(p); ok {
		t.Error("script alheio reconhecido como nosso")
	}
	// sh consegue interpretar o lançador (só checagem de sintaxe, nada roda).
	os.WriteFile(p, []byte(launcherScript("a/b", target)), 0o755)
	if out, err := exec.Command("sh", "-n", p).CombinedOutput(); err != nil {
		t.Errorf("sh -n: %v %s", err, out)
	}
}
