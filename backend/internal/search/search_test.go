package search

import (
	"math/rand"
	"reflect"
	"testing"
)

// catalog imita o catálogo real indexado do GitHub.
func catalog() []Doc {
	return []Doc{
		{Repo: "goodroot/hyprwhspr", Name: "hyprwhspr", Stars: 1222, Category: "AudioVideo",
			Topics:  []string{"omarchy", "speech-to-text", "whisper", "dictation", "voice"},
			Summary: "Native speech-to-text for Linux - Fast, accurate, private system-wide dictation"},
		{Repo: "kristoferlund/ostt", Name: "ostt", Stars: 300, Category: "AudioVideo",
			Topics:  []string{"omarchy", "voice", "transcription", "cli"},
			Summary: "Open source voice-to-text for the terminal. Record from a hotkey, transcribe with any provider"},
		{Repo: "jankeesvw/omarchy-meeting-recorder", Name: "omarchy-meeting-recorder", Stars: 293, Category: "AudioVideo",
			Topics:  []string{"omarchy", "audio", "recording", "meetings"},
			Summary: "Record meetings on Omarchy: mic and computer audio as two tracks"},
		{Repo: "pch/rawmakase", Name: "RAWmakase", Stars: 106, Category: "Graphics",
			Summary: "Free Lightroom alternative for Linux and macOS",
			Readme:  "RAWmakase is a fast, non-destructive RAW photo developer. Develop, export JPEG, import a Lightroom catalog."},
		{Repo: "ZacharyZhang-NY/OmaPhoto", Name: "OmaPhoto", Stars: 174, Category: "Graphics",
			Topics:  []string{"omarchy", "photo", "image-editor", "layers"},
			Summary: "Layered image editor for Linux and Omarchy, a port of Photopea"},
		{Repo: "michaelmonetized/omadesign", Name: "omadesign", Stars: 22, Category: "Graphics",
			Topics:  []string{"omarchy", "design", "illustration", "photography", "raster", "vector"},
			Summary: "Native Linux creative suite for design, paint, and motion"},
		{Repo: "erans/hyprmon", Name: "HyprMon", Stars: 518, Category: "System",
			Topics:  []string{"hyprland", "monitor", "tui", "display"},
			Summary: "TUI monitor configuration tool for Hyprland with visual layout"},
		{Repo: "crmne/hyprmoncfg", Name: "hyprmoncfg", Stars: 437, Category: "System",
			Topics:  []string{"hyprland", "monitor", "display"},
			Summary: "Arrange Hyprland monitors without doing coordinate math"},
		{Repo: "huacnlee/omamail", Name: "Omamail", Stars: 263, Category: "Network",
			Topics:  []string{"omarchy", "email", "imap", "gmail"},
			Summary: "Omarchy mail plugin with Gmail, HEY and IMAP supports"},
		{Repo: "t4t5/rencal", Name: "renCal", Stars: 150, Category: "Office",
			Topics:  []string{"calendar", "omarchy"},
			Summary: "A modern desktop calendar. Built for Omarchy."},
	}
}

func repos(rs []Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Repo
	}
	return out
}

func TestTokens(t *testing.T) {
	got := Tokens("Edições de FOTOS, câmera & áudio — the Photo-Editors v2")
	want := []string{"edicao", "foto", "camera", "audio", "photo", "editor", "v2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if Tokens("the a de o") != nil {
		t.Error("só stopwords deveria dar vazio")
	}
}

func TestStemIngEd(t *testing.T) {
	cases := map[string]string{
		"theming": "theme", "recording": "record", "running": "run", "annotated": "annotate",
		"editing": "edit", "string": "string", "dictation": "dictation", "installed": "install",
		"indexed": "index", "feed": "feed", "embedded": "embed", "themes": "theme",
	}
	for in, want := range cases {
		if got := stem(in); got != want {
			t.Errorf("stem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEditDistance1(t *testing.T) {
	yes := [][2]string{{"monitor", "monitor"}, {"monitr", "monitor"}, {"monitor", "monitors"},
		{"moniotr", "monitor"}, {"monitoz", "monitor"}}
	no := [][2]string{{"monitor", "mentor"}, {"abc", "abcde"}, {"moinotr", "monitor"}}
	for _, p := range yes {
		if !editDistance1(p[0], p[1]) {
			t.Errorf("%v deveria casar", p)
		}
	}
	for _, p := range no {
		if editDistance1(p[0], p[1]) {
			t.Errorf("%v não deveria casar", p)
		}
	}
}

func TestSearchRanking(t *testing.T) {
	ix := Build(catalog())
	cases := []struct {
		query string
		first string
	}{
		{"speech to text", "goodroot/hyprwhspr"},
		{"lightroom", "pch/rawmakase"},
		{"calendar", "t4t5/rencal"},
		{"calendário", "t4t5/rencal"}, // pt → en
		{"gravador de reunião", "jankeesvw/omarchy-meeting-recorder"},
		{"editor de fotos", "ZacharyZhang-NY/OmaPhoto"},
		{"zzzqqq", ""}, // sem correspondência: não inventa
		{"email", "huacnlee/omamail"},
		{"photo editor", "ZacharyZhang-NY/OmaPhoto"},
		{"omaphoto", "ZacharyZhang-NY/OmaPhoto"}, // pelo nome
		{"rawmak", "pch/rawmakase"},              // prefixo
		{"lightrom", "pch/rawmakase"},            // erro de digitação
		{"transcripton", "kristoferlund/ostt"},   // erro de digitação
		{"meetings", "jankeesvw/omarchy-meeting-recorder"},
	}
	for _, c := range cases {
		res := ix.Search(c.query, 0)
		if c.first == "" {
			if len(res) != 0 {
				t.Errorf("%q: esperava nada, veio %v", c.query, repos(res))
			}
			continue
		}
		if len(res) == 0 || res[0].Repo != c.first {
			t.Errorf("%q: primeiro = %v, want %s", c.query, repos(res), c.first)
		}
	}
}

// Uma variante rara (prefixo ou erro de digitação) não pode passar à
// frente do documento que contém exatamente o termo buscado.
func TestExactTermBeatsVariants(t *testing.T) {
	ix := Build([]Doc{
		{Repo: "a/aether", Name: "Aether", Summary: "Native theming made easy", Topics: []string{"omarchy-theme"}},
		{Repo: "b/themed", Name: "Bar", Summary: "A themed status bar with themeable widgets"},
		{Repo: "c/there", Name: "There", Summary: "Get there faster"},
		{Repo: "d/other", Name: "Other", Summary: "Theme theme theme"},
		{Repo: "e/x", Name: "X", Summary: "unrelated"},
		{Repo: "f/y", Name: "Y", Summary: "unrelated too"},
	})
	got := repos(ix.Search("theme", 0))
	if len(got) == 0 || (got[0] != "a/aether" && got[0] != "d/other") {
		t.Errorf("theme = %v", got)
	}
	for _, r := range got {
		if r == "c/there" {
			t.Errorf("\"theme\" casou com \"there\" por erro de digitação: %v", got)
		}
	}
	// Termo inexistente continua sendo corrigido.
	if got := repos(ix.Search("aehter", 0)); len(got) == 0 || got[0] != "a/aether" {
		t.Errorf("aehter = %v", got)
	}
}

func TestSearchTopTwo(t *testing.T) {
	ix := Build(catalog())
	// As duas ferramentas de monitor ficam à frente de todo o resto.
	got := repos(ix.Search("monitor hyprland", 2))
	want := map[string]bool{"erans/hyprmon": true, "crmne/hyprmoncfg": true}
	if len(got) != 2 || !want[got[0]] || !want[got[1]] {
		t.Errorf("top 2 = %v", got)
	}
}

func TestSearchAndThenOr(t *testing.T) {
	ix := Build(catalog())
	// "voice" e "calendar" nunca aparecem juntos: cai para OU e traz os dois lados.
	res := repos(ix.Search("voice calendar", 0))
	has := map[string]bool{}
	for _, r := range res {
		has[r] = true
	}
	if !has["t4t5/rencal"] || !has["kristoferlund/ostt"] {
		t.Errorf("fallback OU: %v", res)
	}
	// "voice terminal" casa só com ostt no modo E.
	if got := repos(ix.Search("voice terminal", 0)); !reflect.DeepEqual(got, []string{"kristoferlund/ostt"}) {
		t.Errorf("E: %v", got)
	}
	if ix.Search("", 0) != nil || ix.Search("the of", 0) != nil {
		t.Error("consulta vazia deveria dar nil")
	}
	if n := len(ix.Search("omarchy", 3)); n != 3 {
		t.Errorf("limit: %d", n)
	}
}

func TestSimilar(t *testing.T) {
	ix := Build(catalog())
	cases := map[string]string{
		"erans/hyprmon":      "crmne/hyprmoncfg",
		"crmne/hyprmoncfg":   "erans/hyprmon",
		"goodroot/hyprwhspr": "kristoferlund/ostt",
	}
	for repo, want := range cases {
		res := ix.Similar(repo, 3)
		if len(res) == 0 || res[0].Repo != want {
			t.Errorf("parecidos com %s = %v, want %s primeiro", repo, repos(res), want)
		}
		for _, r := range res {
			if r.Repo == repo {
				t.Errorf("%s aparece como parecido consigo mesmo", repo)
			}
		}
	}
	// Os editores de imagem se agrupam.
	photo := repos(ix.Similar("ZacharyZhang-NY/OmaPhoto", 2))
	gfx := map[string]bool{"pch/rawmakase": true, "michaelmonetized/omadesign": true}
	if len(photo) != 2 || !gfx[photo[0]] || !gfx[photo[1]] {
		t.Errorf("parecidos com OmaPhoto = %v", photo)
	}
	// Um app de calendário não é parecido com os monitores.
	for _, r := range ix.Similar("t4t5/rencal", 0) {
		if r.Repo == "erans/hyprmon" || r.Repo == "crmne/hyprmoncfg" {
			t.Errorf("rencal parecido com %s (%.3f)", r.Repo, r.Score)
		}
	}
	if ix.Similar("x/y", 5) != nil {
		t.Error("repo desconhecido")
	}
}

// A ordem de entrada nunca muda o resultado.
func TestDeterministic(t *testing.T) {
	base := Build(catalog())
	queries := []string{"monitor", "photo", "voice", "omarchy", "linux editor"}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20; i++ {
		docs := catalog()
		rng.Shuffle(len(docs), func(a, b int) { docs[a], docs[b] = docs[b], docs[a] })
		ix := Build(docs)
		for _, q := range queries {
			if !reflect.DeepEqual(ix.Search(q, 0), base.Search(q, 0)) {
				t.Fatalf("busca %q mudou com a ordem de entrada", q)
			}
		}
		for _, d := range docs {
			if !reflect.DeepEqual(ix.Similar(d.Repo, 0), base.Similar(d.Repo, 0)) {
				t.Fatalf("parecidos de %s mudaram com a ordem de entrada", d.Repo)
			}
		}
	}
}

func BenchmarkSearch(b *testing.B) {
	var docs []Doc
	for i := 0; i < 50; i++ {
		for _, d := range catalog() {
			d.Repo = d.Repo + string(rune('a'+i%26)) + string(rune('a'+i/26))
			docs = append(docs, d)
		}
	}
	ix := Build(docs)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix.Search("monitr hyprland", 20)
	}
}
