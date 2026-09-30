package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDesktopFileValidate(t *testing.T) {
	bin, err := exec.LookPath("desktop-file-validate")
	if err != nil {
		t.Skip("desktop-file-validate not installed")
	}
	p := filepath.Join(t.TempDir(), "omastore-a-b.desktop")
	os.WriteFile(p, []byte(Desktop{Name: "Oma\\Photo", Comment: "An editor; 100%", Exec: "/home/u/my apps/a\"b%c",
		Icon: "omastore-a-b", Categories: []string{"Graphics"}, Repo: "a/b", Version: "v1"}.Render()), 0o644)
	out, err := exec.Command(bin, p).CombinedOutput()
	if err != nil || len(out) > 0 {
		t.Errorf("desktop-file-validate: %v\n%s", err, out)
	}
}
