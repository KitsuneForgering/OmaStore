package gitrepo

import (
	"reflect"
	"testing"
)

func TestFindIcon(t *testing.T) {
	cases := []struct {
		name  string
		repo  string
		files []string
		want  string
	}{
		{"none", "app", []string{"README.md", "main.go", "docs/diagram.png"}, ""},
		{"icon at the root", "app", []string{"icon.png", "assets/logo.png"}, "icon.png"},
		{"svg vence png", "app", []string{"assets/icon.png", "assets/icon.svg"}, "assets/icon.svg"},
		{"repo name", "omaphoto", []string{"data/omaphoto.svg", "docs/logo-old.png"}, "data/omaphoto.svg"},
		{"larger hicolor", "omavm", []string{
			"data/icons/hicolor/16x16/apps/omavm.png",
			"data/icons/hicolor/256x256/apps/omavm.png",
		}, "data/icons/hicolor/256x256/apps/omavm.png"},
		{"ignora screenshots e testes", "app", []string{
			"screenshots/icon.png", "testdata/icon.png", "internal/latest/logo.png",
		}, "internal/latest/logo.png"},
		{"favicon perde", "app", []string{"web/favicon.png", "assets/app-logo.png"}, "assets/app-logo.png"},
		{"jpg is not an icon", "app", []string{"icon.jpg"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FindIcon(c.files, c.repo); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestFindScreenshots(t *testing.T) {
	files := []string{
		"screenshots/b.png", "screenshots/a.jpg", "docs/preview.webp",
		"assets/icon.png", "screenshots/notes.txt", "docs/Screenshot 1.PNG",
	}
	got := FindScreenshots(files, 0)
	want := []string{"docs/Screenshot 1.PNG", "docs/preview.webp", "screenshots/a.jpg", "screenshots/b.png"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if n := len(FindScreenshots(files, 2)); n != 2 {
		t.Errorf("max: %d", n)
	}
}
