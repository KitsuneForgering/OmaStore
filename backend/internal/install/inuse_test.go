package install

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/asset"
)

// fakeProc points procRoot at a fake /proc where each process has a cwd and
// a maps file.
type fakeProcess struct {
	cwd  string
	maps []string
}

func fakeProc(t *testing.T, procs map[int]fakeProcess) {
	t.Helper()
	root := t.TempDir()
	for pid, p := range procs {
		dir := filepath.Join(root, strconv.Itoa(pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if p.cwd != "" {
			if err := os.Symlink(p.cwd, filepath.Join(dir, "cwd")); err != nil {
				t.Fatal(err)
			}
		}
		var lines []string
		for _, m := range p.maps {
			lines = append(lines, "7f00-7f01 r-xp 00000000 00:1f 42 "+m)
		}
		os.WriteFile(filepath.Join(dir, "maps"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "comm"), []byte("app"+strconv.Itoa(pid)+"\n"), 0o644)
	}
	os.MkdirAll(filepath.Join(root, "self"), 0o755) // not a pid
	old := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = old })
}

func TestProcsUsing(t *testing.T) {
	dir := "/home/u/.local/share/omastore/apps/acme__x"
	fakeProc(t, map[int]fakeProcess{
		101: {cwd: dir + "/v1/bin"},
		102: {cwd: "/home/u", maps: []string{"/usr/lib/libc.so.6", dir + "/v1/lib/libx.so (deleted)"}},
		103: {cwd: "/home/u", maps: []string{"/usr/lib/libc.so.6", "[heap]"}},
		104: {cwd: "/home/u/.local/share/omastore/apps/acme__xy"}, // a prefix, not inside
	})
	ps, err := procsUsing(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].PID != 101 || ps[1].PID != 102 || ps[0].Name != "app101" {
		t.Errorf("processes = %+v", ps)
	}
	if got := describe(ps); got != "app101 (101), app102 (102)" {
		t.Errorf("describe = %q", got)
	}
}

func TestUninstallRefusesWhileRunning(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.publish(t, "v1", appTarGz(t, "1"), asset.FormatTarGz, true)
	inst, err := e.in.Install(ctx, "acme/omaphoto", Options{})
	if err != nil {
		t.Fatal(err)
	}
	fakeProc(t, map[int]fakeProcess{7: {cwd: filepath.Dir(inst.ExecPath)}})

	if err := e.in.Uninstall(ctx, "acme/omaphoto", false); !errors.Is(err, ErrInUse) {
		t.Fatalf("uninstall while running: %v", err)
	}
	if _, err := os.Stat(inst.ExecPath); err != nil {
		t.Fatalf("files removed although it refused: %v", err)
	}
	// Reinstalling the running version would swap its directory under it.
	if _, err := e.in.Install(ctx, "acme/omaphoto", Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("reinstall while running: %v", err)
	}
	if err := e.in.Uninstall(ctx, "acme/omaphoto", true); err != nil {
		t.Fatalf("forced uninstall: %v", err)
	}
	if _, err := os.Stat(inst.ExecPath); !os.IsNotExist(err) {
		t.Error("forced uninstall left the files")
	}
}

// An old version a process still runs from survives the update that would
// remove it, stays registered, and goes away on a later update.
func TestUpdateKeepsRunningOldVersion(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, v := range []string{"v1", "v2"} {
		e.publish(t, v, appTarGz(t, v), asset.FormatTarGz, true)
		if _, err := e.in.Install(ctx, "acme/omaphoto", Options{}); err != nil {
			t.Fatal(err)
		}
	}
	v1 := filepath.Join(e.paths.AppsDir, "acme__omaphoto", "v1")
	fakeProc(t, map[int]fakeProcess{9: {cwd: v1}})

	e.publish(t, "v3", appTarGz(t, "v3"), asset.FormatTarGz, true)
	inst, err := e.in.Update(ctx, "acme/omaphoto", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v1); err != nil || !registered(inst.Files, v1) {
		t.Fatalf("running v1 removed or forgotten: %v %v", err, inst.Files)
	}

	fakeProc(t, nil) // it was closed
	e.publish(t, "v4", appTarGz(t, "v4"), asset.FormatTarGz, true)
	inst, err = e.in.Update(ctx, "acme/omaphoto", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v1); !os.IsNotExist(err) || registered(inst.Files, v1) {
		t.Errorf("v1 left behind: %v %v", err, inst.Files)
	}
}

func TestRollback(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.publish(t, "v1", appTarGz(t, "1"), asset.FormatTarGz, true)
	inst1, err := e.in.Install(ctx, "acme/omaphoto", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.in.Rollback(ctx, "acme/omaphoto"); !errors.Is(err, ErrNoPrevious) {
		t.Errorf("rollback without a previous version: %v", err)
	}
	e.publish(t, "v2", appTarGz(t, "2"), asset.FormatTarGz, true)
	inst2, err := e.in.Update(ctx, "acme/omaphoto", Options{})
	if err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(e.paths.BinDir, "omaphoto")
	desktop := filepath.Join(e.paths.Applications, "omastore-acme-omaphoto.desktop")
	back, err := e.in.Rollback(ctx, "acme/omaphoto")
	if err != nil {
		t.Fatal(err)
	}
	if back.Version != "v1" || back.ExecPath != inst1.ExecPath || back.PreviousVersion != "v2" || back.PreviousExec != inst2.ExecPath {
		t.Errorf("after rollback: %+v", back)
	}
	if target, _ := launcherTarget(link); target != inst1.ExecPath {
		t.Errorf("launcher → %s", target)
	}
	content, _ := os.ReadFile(desktop)
	if !strings.Contains(string(content), "Exec="+inst1.ExecPath+"\n") || !strings.Contains(string(content), "X-OmaStore-Version=v1\n") {
		t.Errorf(".desktop not rolled back:\n%s", content)
	}
	if !strings.Contains(string(content), "Icon=omastore-acme-omaphoto\n") {
		t.Errorf(".desktop lost the icon:\n%s", content)
	}
	stored, _ := e.st.GetInstall(ctx, "acme/omaphoto")
	if stored.Version != "v1" {
		t.Errorf("stored = %+v", stored)
	}

	// Twice: forward again, nothing downloaded.
	hits := e.hits.Load()
	again, err := e.in.Rollback(ctx, "acme/omaphoto")
	if err != nil {
		t.Fatal(err)
	}
	if again.Version != "v2" || e.hits.Load() != hits {
		t.Errorf("second rollback: %+v (downloads %d)", again, e.hits.Load()-hits)
	}

	// The previous version vanished from disk: a clear error, nothing changed.
	os.RemoveAll(filepath.Dir(filepath.Dir(inst1.ExecPath)))
	if _, err := e.in.Rollback(ctx, "acme/omaphoto"); !errors.Is(err, ErrNoPrevious) {
		t.Errorf("rollback to a removed version: %v", err)
	}
	if target, _ := launcherTarget(link); target != inst2.ExecPath {
		t.Errorf("launcher changed by a failed rollback: %s", target)
	}

	// Uninstall removes both versions.
	if err := e.in.Uninstall(ctx, "acme/omaphoto", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.paths.AppsDir, "acme__omaphoto")); !os.IsNotExist(err) {
		t.Error("app directory left behind")
	}
}

// The real /proc: a process whose working directory is inside dir.
func TestProcsUsingRealProc(t *testing.T) {
	if _, err := os.Stat("/proc/self/cwd"); err != nil {
		t.Skip("no /proc")
	}
	dir := t.TempDir()
	cmd := exec.Command("/bin/sleep", "30")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start sleep:", err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	ps, err := procsUsing(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range ps {
		found = found || p.PID == cmd.Process.Pid
	}
	if !found {
		t.Errorf("sleep (%d) not found: %+v", cmd.Process.Pid, ps)
	}
}
