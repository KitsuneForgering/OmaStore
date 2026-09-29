package index

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

var urls = repoURLs{FullName: "acme/omaphoto", Ref: "abc123"}

const sampleReadme = `<!-- comment -->
<p align="center"><img src="assets/logo.png" width="128"></p>

# OmaPhoto

[![CI](https://github.com/acme/omaphoto/actions/workflows/ci.yml/badge.svg)](https://github.com/acme/omaphoto/actions)
![Stars](https://img.shields.io/github/stars/acme/omaphoto)

A fast, **native** photo editor for [Omarchy](https://omarchy.org),
built with GTK4.

![Main window](docs/screenshots/main.png)
<img src='./docs/screenshots/edit.png' alt="edit">

See [the docs](docs/USAGE.md#install) and [changelog](/CHANGELOG.md).

` + "```sh\n![not an image](nope.png)\n```\n" + `
[logo]: assets/logo.svg
`

func TestRewriteReadme(t *testing.T) {
	out := RewriteReadme(sampleReadme, urls)
	for _, want := range []string{
		`<img src="https://raw.githubusercontent.com/acme/omaphoto/abc123/assets/logo.png"`,
		`![Main window](https://raw.githubusercontent.com/acme/omaphoto/abc123/docs/screenshots/main.png)`,
		`<img src='https://raw.githubusercontent.com/acme/omaphoto/abc123/docs/screenshots/edit.png'`,
		`[the docs](https://github.com/acme/omaphoto/blob/abc123/docs/USAGE.md#install)`,
		`[changelog](https://github.com/acme/omaphoto/blob/abc123/CHANGELOG.md)`,
		`[Omarchy](https://omarchy.org)`,
		`[logo]: https://raw.githubusercontent.com/acme/omaphoto/abc123/assets/logo.svg`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q", want)
		}
	}
	if strings.Contains(out, "comment -->") {
		t.Error("HTML comment should be removed")
	}
}

func TestResolveRejects(t *testing.T) {
	for _, ref := range []string{"javascript:alert(1)", "data:image/png;base64,xx", "../../etc/passwd"} {
		if got := urls.resolve(ref, true); got != "" {
			t.Errorf("%q → %q", ref, got)
		}
	}
	sub := repoURLs{FullName: "a/b", Ref: "r", BaseDir: "docs"}
	if got := sub.resolve("img/a b.png", true); got != "https://raw.githubusercontent.com/a/b/r/docs/img/a%20b.png" {
		t.Errorf("subdir: %q", got)
	}
	if got := urls.resolve("https://github.com/x/y/blob/main/s.png?raw=true", true); got != "https://raw.githubusercontent.com/x/y/main/s.png" {
		t.Errorf("blob → raw: %q", got)
	}
}

func TestReadmeImages(t *testing.T) {
	got := ReadmeImages(RewriteReadme(sampleReadme, urls))
	want := []string{
		"https://raw.githubusercontent.com/acme/omaphoto/abc123/assets/logo.png",
		"https://raw.githubusercontent.com/acme/omaphoto/abc123/docs/screenshots/main.png",
		"https://raw.githubusercontent.com/acme/omaphoto/abc123/docs/screenshots/edit.png",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestTitleAndSummary(t *testing.T) {
	if got := Title(sampleReadme); got != "OmaPhoto" {
		t.Errorf("title = %q", got)
	}
	want := "A fast, native photo editor for Omarchy, built with GTK4."
	if got := Summary(sampleReadme, 200); got != want {
		t.Errorf("summary = %q", got)
	}
	if got := Summary(sampleReadme, 20); got != "A fast, native photo…" {
		t.Errorf("truncado = %q", got)
	}
	if got := Summary("# X\n\n- item\n- item\n", 100); got != "" {
		t.Errorf("no paragraph: %q", got)
	}
	if got := Title(`<h1 align="center">Rawmakase</h1>`); got != "Rawmakase" {
		t.Errorf("h1 html: %q", got)
	}
}

func TestCategory(t *testing.T) {
	cases := []struct {
		topics      []string
		name, descr string
		want        string
	}{
		{[]string{"omarchy", "photo"}, "", "", CatGraphics},
		{[]string{"omarchy", "qemu"}, "", "", CatSystem},
		{[]string{"omarchy"}, "omamail", "Email client for Omarchy", CatNetwork},
		{[]string{"omarchy"}, "thing", "Does stuff", CatUtility},
		{[]string{"Voice"}, "", "", CatAudioVideo},
		{nil, "rawmakase", "Free Lightroom alternative for Linux", CatGraphics},
	}
	for _, c := range cases {
		if got := Category(c.topics, c.name, c.descr); got != c.want {
			t.Errorf("%v %q: got %s, want %s", c.topics, c.descr, got, c.want)
		}
	}
}

func TestScore(t *testing.T) {
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	fresh := Score(10, now, now)
	old := Score(10, now.AddDate(-2, 0, 0), now)
	more := Score(11, now.AddDate(-5, 0, 0), now)
	if !(more > fresh && fresh > old && old >= 10) {
		t.Errorf("fresh=%v old=%v more=%v", fresh, old, more)
	}
	if Score(0, time.Time{}, now) != 0 {
		t.Error("without pushed_at it should be 0")
	}
}
