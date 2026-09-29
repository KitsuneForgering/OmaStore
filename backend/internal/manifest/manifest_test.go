package manifest

import (
	"strings"
	"testing"
)

const full = `
name = "  RAWmakase  "
summary = "Alternativa livre ao Lightroom"
categories = ["Graphics", "Photography", "Graphics"]
icon = "packaging/rawmakase.svg"
screenshots = ["docs/images/screenshot.png", "https://example.com/s.png"]
terminal = false

[linux.x86_64]
asset = "rawmakase-{version}-x86_64-linux.tar.gz"
exec = "usr/bin/rawmakase"

[linux.aarch64]
asset = "rawmakase-{version}-aarch64-linux.tar.gz"
exec = "usr/bin/rawmakase"
`

func TestParseFull(t *testing.T) {
	m, ps, err := Parse([]byte(full), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 0 {
		t.Errorf("problemas inesperados: %v", ps)
	}
	if m.Name != "RAWmakase" || m.MainCategory() != "Graphics" || len(m.Categories) != 2 {
		t.Errorf("m = %+v", m)
	}
	if m.Terminal == nil || *m.Terminal {
		t.Error("terminal = false não lido")
	}
	tg, ok := m.Target("amd64")
	if !ok || tg.Exec != "usr/bin/rawmakase" {
		t.Errorf("x86_64 = %+v", tg)
	}
	if _, ok := m.Target("arm64"); !ok {
		t.Error("aarch64 deveria virar arm64")
	}
	back := Decode(m.Encode())
	if back == nil || back.Name != m.Name || back.Linux["arm64"].Asset != m.Linux["arm64"].Asset {
		t.Errorf("Encode/Decode: %+v", back)
	}
}

func TestParseRejectsUnsafe(t *testing.T) {
	src := `
name = "` + strings.Repeat("x", 200) + `"
categories = ["Graphics;Evil", "Not A Category", "System"]
icon = "../../etc/passwd.png"
screenshots = ["/abs.png", "docs/readme.md", "ok.png", "file:///etc/shadow"]
[linux.x86_64]
asset = "app-{arch}.tar.gz"
exec = "../bin/sh"
[linux.riscv64]
asset = "x"
[linux.amd64]
asset = "*"
`
	m, ps, err := Parse([]byte(src), false)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(m.Name)) != maxName {
		t.Errorf("nome não cortado: %d", len(m.Name))
	}
	if len(m.Categories) != 1 || m.Categories[0] != "System" {
		t.Errorf("categorias = %v", m.Categories)
	}
	if m.Icon != "" {
		t.Errorf("ícone perigoso aceito: %q", m.Icon)
	}
	if len(m.Screenshots) != 1 || m.Screenshots[0] != "ok.png" {
		t.Errorf("screenshots = %v", m.Screenshots)
	}
	if m.Linux != nil {
		t.Errorf("alvos inválidos aceitos: %+v", m.Linux)
	}
	errs := 0
	for _, p := range ps {
		if !p.Warning {
			errs++
		}
	}
	if errs < 8 {
		t.Errorf("poucos erros reportados (%d): %v", errs, ps)
	}
}

func TestStrictUnknownFields(t *testing.T) {
	_, ps, err := Parse([]byte("name = \"x\"\nicone = \"typo.svg\"\n"), true)
	if err != nil || len(ps) != 1 || !strings.Contains(ps[0].Message, "icone") {
		t.Errorf("strict: %v %v", ps, err)
	}
	m, ps, err := Parse([]byte("name = \"x\"\nfuture_field = 1\n"), false)
	if err != nil || len(ps) != 0 || m.Name != "x" {
		t.Errorf("leniente: %v %v %v", m, ps, err)
	}
	if _, _, err := Parse([]byte("name = "), false); err == nil {
		t.Error("TOML inválido deveria dar erro")
	}
	if _, _, err := Parse(make([]byte, MaxSize+1), false); err == nil {
		t.Error("arquivo grande demais aceito")
	}
}

func TestMatchAsset(t *testing.T) {
	cases := []struct {
		pattern, tag, name string
		want               bool
	}{
		{"app-{version}-x86_64.tar.gz", "v1.2.0", "app-1.2.0-x86_64.tar.gz", true},
		{"app-{version}-x86_64.tar.gz", "1.2.0", "app-1.2.0-x86_64.tar.gz", true},
		{"app-{tag}-x86_64.tar.gz", "v1.2.0", "app-v1.2.0-x86_64.tar.gz", true},
		{"app-{version}-x86_64.tar.gz", "v1.2.0", "app-1.2.1-x86_64.tar.gz", false},
		{"app-*-linux-amd64", "v3", "app-3.0-linux-amd64", true},
		{"app-*-linux-amd64", "v3", "app-3.0-linux-amd64.sha256", false},
		{"*-x86_64.AppImage", "v1", "Tool-1-x86_64.AppImage", true},
		{"exact", "v1", "exact", true},
		{"exact", "v1", "exact2", false},
	}
	for _, c := range cases {
		if got := MatchAsset(c.pattern, c.tag, c.name); got != c.want {
			t.Errorf("%q %q %q = %v", c.pattern, c.tag, c.name, got)
		}
	}
}

func TestNoMainCategoryWarns(t *testing.T) {
	m, ps, _ := Parse([]byte(`categories = ["Photography"]`), true)
	if m.MainCategory() != "" || len(ps) != 1 || !ps[0].Warning {
		t.Errorf("m=%v ps=%v", m.Categories, ps)
	}
}

func TestEmpty(t *testing.T) {
	m, _, _ := Parse([]byte("# nada\n"), true)
	if !m.Empty() || !m.IsApp() || Decode("") != nil || Decode("{lixo") != nil {
		t.Error("manifesto vazio")
	}
	// Vazio continua sendo um manifesto (a presença do arquivo é o opt-in).
	if back := Decode(m.Encode()); back == nil || !back.IsApp() {
		t.Errorf("Encode de vazio perdeu a presença: %q", m.Encode())
	}
}

func TestKind(t *testing.T) {
	for src, app := range map[string]bool{
		``:                   true,
		`kind = "app"`:       true,
		`kind = "App"`:       true,
		`kind = "plugin"`:    false,
		`kind = "theme"`:     false,
		`kind = "extension"`: false,
	} {
		m, ps, err := Parse([]byte(src), true)
		if err != nil {
			t.Fatal(err)
		}
		if m.IsApp() != app {
			t.Errorf("%q: IsApp = %v", src, m.IsApp())
		}
		if !app && len(ps) == 0 {
			t.Errorf("%q: deveria reportar que não é indexado", src)
		}
	}
}

func TestExecPlaceholders(t *testing.T) {
	m, ps, _ := Parse([]byte("[linux.x86_64]\nexec = \"app-{version}-x86_64-linux/bin/app\"\n"), true)
	if len(ps) != 0 || m.Linux["amd64"].Exec != "app-{version}-x86_64-linux/bin/app" {
		t.Errorf("exec com {version}: %v %+v", ps, m.Linux)
	}
	if got := Expand(m.Linux["amd64"].Exec, "v1.2.0"); got != "app-1.2.0-x86_64-linux/bin/app" {
		t.Errorf("Expand = %q", got)
	}
	_, ps, _ = Parse([]byte("[linux.x86_64]\nexec = \"{arch}/bin/app\"\n"), true)
	if len(ps) != 1 || ps[0].Field != "linux.x86_64.exec" {
		t.Errorf("marcador inválido no exec: %v", ps)
	}
}
