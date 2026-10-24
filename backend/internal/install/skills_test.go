package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/KitsuneForgering/OmaStore/backend/internal/flock"
)

func TestInstallSkillsExplicit(t *testing.T) {
	e := newEnv(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", filepath.Join(e.paths.Home, "custom codex"))
	src := t.TempDir()
	skill := filepath.Join(src, "omastore-check")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.in.InstallSkills(context.Background(), src); err == nil {
		t.Fatal("accepted absent agents")
	}
	dest := filepath.Join(e.paths.Home, "custom codex", "skills", "omastore-check")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("local edits"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(e.in.SelfRoot(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.in.SelfRoot(), selfNoSkills), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := e.in.InstallSkills(context.Background(), src)
	if err != nil || n != 1 {
		t.Fatalf("install = %d, %v", n, err)
	}
	if got := readFile(t, filepath.Join(dest, "SKILL.md")); got != "new" {
		t.Fatal(got)
	}
	backups, _ := filepath.Glob(filepath.Join(e.paths.DataDir, "skill-backups", "omastore-check.*", "omastore-check", "SKILL.md"))
	if len(backups) != 1 || readFile(t, backups[0]) != "local edits" {
		t.Fatalf("backup = %v", backups)
	}
	if err := os.RemoveAll(dest); err != nil {
		t.Fatal(err)
	}
	n, err = e.in.InstallSkills(context.Background(), src)
	if err != nil || n != 1 {
		t.Fatalf("missing skill = %d, %v", n, err)
	}
	unlock, err := flock.TryLock(filepath.Join(e.in.SelfRoot(), ".update.lock"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.in.InstallSkills(context.Background(), src)
	unlock()
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent self-update: %v", err)
	}
	if err := os.Symlink("/tmp", filepath.Join(skill, "unsafe")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.in.InstallSkills(context.Background(), src); err == nil {
		t.Fatal("accepted symlink in bundle")
	}
}
