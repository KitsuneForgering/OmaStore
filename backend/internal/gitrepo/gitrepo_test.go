package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// makeRepo cria um repositório git local com os arquivos dados e retorna
// o caminho e uma função para adicionar commits.
func makeRepo(t *testing.T, files map[string]string) (string, func(map[string]string) string) {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := r.Worktree()
	commit := func(fs map[string]string) string {
		for name, content := range fs {
			p := filepath.Join(dir, name)
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := wt.Add(name); err != nil {
				t.Fatal(err)
			}
		}
		h, err := wt.Commit("c", &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()}})
		if err != nil {
			t.Fatal(err)
		}
		return h.String()
	}
	commit(files)
	return dir, commit
}

func TestSyncCloneAndUpdate(t *testing.T) {
	src, commit := makeRepo(t, map[string]string{"README.md": "x", "assets/icon.svg": "<svg/>"})
	c := &Cache{Dir: t.TempDir(), URL: func(string) string { return src }}
	ctx := context.Background()

	dir, sha1, err := c.Sync(ctx, "acme/app")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(c.Dir, "acme__app") {
		t.Errorf("dir = %q", dir)
	}
	files, err := ListFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	if !reflect.DeepEqual(files, []string{"README.md", "assets/icon.svg"}) {
		t.Errorf("files = %v", files)
	}

	want := commit(map[string]string{"screenshots/main.png": "png"})
	_, sha2, err := c.Sync(ctx, "acme/app")
	if err != nil {
		t.Fatal(err)
	}
	if sha2 == sha1 || sha2 != want {
		t.Errorf("sha após update = %s, want %s", sha2, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "screenshots/main.png")); err != nil {
		t.Errorf("arquivo novo ausente: %v", err)
	}
}

func TestSyncRecoversFromBrokenClone(t *testing.T) {
	src, _ := makeRepo(t, map[string]string{"a.txt": "a"})
	c := &Cache{Dir: t.TempDir(), URL: func(string) string { return src }}
	broken := c.Path("acme/app")
	os.MkdirAll(filepath.Join(broken, ".git"), 0o755)
	os.WriteFile(filepath.Join(broken, "lixo"), []byte("x"), 0o644)

	dir, _, err := c.Sync(context.Background(), "acme/app")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "lixo")); !os.IsNotExist(err) {
		t.Error("clone quebrado não foi substituído")
	}
}

func TestListFilesSkips(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.png", "node_modules/x/y.png", "src/b.svg"} {
		p := filepath.Join(dir, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, nil, 0o644)
	}
	os.Symlink("/etc/passwd", filepath.Join(dir, "link.png"))
	files, _ := ListFiles(dir)
	sort.Strings(files)
	if !reflect.DeepEqual(files, []string{"a.png", "src/b.svg"}) {
		t.Errorf("files = %v", files)
	}
}
