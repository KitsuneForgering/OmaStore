package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/KitsuneForgering/OmaStore/backend/internal/asset"
	"github.com/KitsuneForgering/OmaStore/backend/internal/manifest"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

type fakeSystemd struct {
	dir     string
	enabled map[string]bool
	active  map[string]bool
	foreign map[string]string
	fail    string
	calls   []string
}

func (f *fakeSystemd) run(_ context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if len(args) == 0 {
		return "", errors.New("no args")
	}
	if f.fail == args[0] {
		return "", errors.New("simulated " + f.fail + " failure")
	}
	if args[0] == "daemon-reload" {
		return "", nil
	}
	unit := args[len(args)-1]
	path := filepath.Join(f.dir, unit)
	if f.foreign[unit] != "" {
		path = f.foreign[unit]
	}
	_, statErr := os.Stat(path)
	if args[0] == "show" {
		load, fragment := "not-found", ""
		if statErr == nil {
			load, fragment = "loaded", path
		}
		enabled, active := "disabled", "inactive"
		if f.enabled[unit] {
			enabled = "enabled"
		}
		if f.active[unit] {
			active = "active"
		}
		return fmt.Sprintf("LoadState=%s\nFragmentPath=%s\nActiveState=%s\nUnitFileState=%s\n", load, fragment, active, enabled), nil
	}
	switch args[0] {
	case "enable":
		f.enabled[unit] = true
	case "disable":
		f.enabled[unit] = false
	case "start", "restart":
		f.active[unit] = true
	case "stop":
		f.active[unit] = false
	default:
		return "", fmt.Errorf("unexpected command: %v", args)
	}
	return "", nil
}

func serviceEnv(t *testing.T, service manifest.Service) (*env, *fakeSystemd) {
	t.Helper()
	e := newEnv(t)
	f := &fakeSystemd{dir: e.paths.UserUnits, enabled: map[string]bool{}, active: map[string]bool{}, foreign: map[string]string{}}
	e.in.systemctl = f.run
	publishService := func(tag, suffix string) {
		data := tarGz(t, []entry{
			{name: "omaphoto-" + suffix + "/omaphoto", body: string(elfBin) + suffix, mode: 0o755},
			{name: "omaphoto-" + suffix + "/sessiond", body: string(elfBin) + suffix, mode: 0o755},
		})
		e.publish(t, tag, data, asset.FormatTarGz, true)
		setServiceManifest(t, e, map[string]manifest.Service{"session": service})
	}
	publishService("v1", "1")
	return e, f
}

func setServiceManifest(t *testing.T, e *env, services map[string]manifest.Service) {
	t.Helper()
	d, err := e.st.GetApp(context.Background(), "acme/omaphoto")
	if err != nil {
		t.Fatal(err)
	}
	d.App.Manifest = (&manifest.Manifest{Kind: manifest.KindApp, Services: services}).Encode()
	if err := e.st.SaveIndexed(context.Background(), d.Repo, d.App, d.Assets); err != nil {
		t.Fatal(err)
	}
}

func publishServiceVersion(t *testing.T, e *env, tag, suffix string, services map[string]manifest.Service) {
	t.Helper()
	e.publish(t, tag, tarGz(t, []entry{
		{name: "omaphoto-" + suffix + "/omaphoto", body: string(elfBin) + suffix, mode: 0o755},
		{name: "omaphoto-" + suffix + "/sessiond", body: string(elfBin) + suffix, mode: 0o755},
	}), asset.FormatTarGz, true)
	setServiceManifest(t, e, services)
}

func TestUserServiceLifecycle(t *testing.T) {
	s := manifest.Service{Type: "systemd-user", Unit: "omaphoto-sessiond.service", Exec: "omaphoto-1/sessiond", Enable: true, Start: true, Restart: "on-failure"}
	e, f := serviceEnv(t, s)
	ctx := context.Background()
	inst, err := e.in.Install(ctx, "acme/omaphoto", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(inst.Integrations) != 1 || !inst.Integrations[0].EnabledByStore || !inst.Integrations[0].StartedByStore {
		t.Fatalf("ownership = %+v", inst.Integrations)
	}
	unitPath := filepath.Join(e.paths.UserUnits, s.Unit)
	b, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "ExecStart=\""+filepath.Join(inst.Files[0], s.Exec)+"\"") || !strings.Contains(string(b), "Restart=on-failure") {
		t.Fatalf("generated unit = %s", b)
	}
	if !f.enabled[s.Unit] || !f.active[s.Unit] {
		t.Fatal("service not enabled and active")
	}
	stored, err := e.st.GetInstall(ctx, inst.FullName)
	if err != nil || len(stored.Integrations) != 1 {
		t.Fatalf("state not persisted: %+v %v", stored, err)
	}
	// Reinstalling the same version is idempotent with respect to ownership.
	if _, err := e.in.Install(ctx, inst.FullName, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := e.in.Uninstall(ctx, inst.FullName, false); err != nil {
		t.Fatal(err)
	}
	if f.enabled[s.Unit] || f.active[s.Unit] {
		t.Fatal("service left enabled or running")
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("unit remains: %v", err)
	}
	if !slices.Contains(f.calls, "disable "+s.Unit) || !slices.Contains(f.calls, "stop "+s.Unit) {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestUserServiceEnableOnlyAndFailures(t *testing.T) {
	s := manifest.Service{Type: "systemd-user", Unit: "omaphoto-sessiond.service", Exec: "omaphoto-1/sessiond", Enable: true}
	for _, fail := range []string{"", "show", "daemon-reload", "enable", "start"} {
		name := fail
		if name == "" {
			name = "enable-only"
		}
		t.Run(name, func(t *testing.T) {
			s.Start = fail == "start"
			e, f := serviceEnv(t, s)
			f.fail = fail
			inst, err := e.in.Install(context.Background(), "acme/omaphoto", Options{})
			if fail == "" {
				if err != nil || inst == nil || !f.enabled[s.Unit] || f.active[s.Unit] {
					t.Fatalf("enable only: %+v %v", inst, err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected failure")
			}
			if _, err := e.st.GetInstall(context.Background(), "acme/omaphoto"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("half-installed record: %v", err)
			}
		})
	}
}

func TestUserServiceUpdateAndRemovedDeclaration(t *testing.T) {
	s := manifest.Service{Type: "systemd-user", Unit: "omaphoto-sessiond.service", Exec: "omaphoto-1/sessiond", Enable: true, Start: true}
	e, f := serviceEnv(t, s)
	ctx := context.Background()
	if _, err := e.in.Install(ctx, "acme/omaphoto", Options{}); err != nil {
		t.Fatal(err)
	}
	s.Exec = "omaphoto-2/sessiond"
	publishServiceVersion(t, e, "v2", "2", map[string]manifest.Service{"session": s})
	if _, err := e.in.Update(ctx, "acme/omaphoto", Options{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.calls, "restart "+s.Unit) {
		t.Fatalf("active service not restarted: %v", f.calls)
	}
	stored, err := e.st.GetInstall(ctx, "acme/omaphoto")
	if err != nil || len(stored.Integrations) != 1 || stored.Integrations[0].Version != "v2" {
		t.Fatalf("update ownership: %+v %v", stored, err)
	}
	setServiceManifest(t, e, nil)
	if _, err := e.in.Install(ctx, "acme/omaphoto", Options{}); err != nil {
		t.Fatal(err)
	}
	if f.active[s.Unit] || f.enabled[s.Unit] {
		t.Fatal("removed declaration kept service active")
	}
	stored, _ = e.st.GetInstall(ctx, "acme/omaphoto")
	if len(stored.Integrations) != 0 {
		t.Fatalf("removed declaration persisted: %+v", stored.Integrations)
	}
}

func TestUserServiceRollbackAndUpdateFailure(t *testing.T) {
	s := manifest.Service{Type: "systemd-user", Unit: "omaphoto-sessiond.service", Exec: "omaphoto-1/sessiond", Enable: true, Start: true}
	e, f := serviceEnv(t, s)
	ctx := context.Background()
	first, err := e.in.Install(ctx, "acme/omaphoto", Options{})
	if err != nil {
		t.Fatal(err)
	}
	s.Exec = "omaphoto-2/sessiond"
	publishServiceVersion(t, e, "v2", "2", map[string]manifest.Service{"session": s})
	f.fail = "restart"
	if _, err := e.in.Update(ctx, first.FullName, Options{}); err == nil {
		t.Fatal("restart failure accepted")
	}
	stored, _ := e.st.GetInstall(ctx, first.FullName)
	if stored.Version != "v1" || !f.active[s.Unit] || !f.enabled[s.Unit] {
		t.Fatalf("failed update lost old state: %+v %+v", stored, f)
	}
	f.fail = ""
	if _, err := e.in.Update(ctx, first.FullName, Options{}); err != nil {
		t.Fatal(err)
	}
	back, err := e.in.Rollback(ctx, first.FullName)
	if err != nil || back.Version != "v1" || len(back.Integrations) != 1 {
		t.Fatalf("rollback: %+v %v", back, err)
	}
	b, err := os.ReadFile(filepath.Join(e.paths.UserUnits, s.Unit))
	if err != nil || !strings.Contains(string(b), "/v1/omaphoto-1/sessiond") {
		t.Fatalf("rollback unit = %s %v", b, err)
	}
	// A manual stop remains stopped on the next update.
	f.active[s.Unit] = false
	before := len(f.calls)
	if _, err := e.in.Update(ctx, first.FullName, Options{}); err != nil {
		t.Fatal(err)
	}
	for _, call := range f.calls[before:] {
		if call == "restart "+s.Unit || call == "start "+s.Unit {
			t.Fatalf("inactive service unexpectedly started: %v", f.calls[before:])
		}
	}
}

func TestUserServiceUninstallFailureKeepsOwnership(t *testing.T) {
	s := manifest.Service{Type: "systemd-user", Unit: "omaphoto-sessiond.service", Exec: "omaphoto-1/sessiond", Enable: true, Start: true}
	e, f := serviceEnv(t, s)
	ctx := context.Background()
	if _, err := e.in.Install(ctx, "acme/omaphoto", Options{}); err != nil {
		t.Fatal(err)
	}
	f.fail = "disable"
	if err := e.in.Uninstall(ctx, "acme/omaphoto", false); err == nil {
		t.Fatal("partial uninstall reported success")
	}
	stored, err := e.st.GetInstall(ctx, "acme/omaphoto")
	if err != nil || len(stored.Integrations) != 1 {
		t.Fatalf("ownership lost: %+v %v", stored, err)
	}
	if _, err := os.Stat(stored.Integrations[0].Path); err != nil {
		t.Fatalf("service unit removed: %v", err)
	}
	f.fail = ""
	if err := e.in.Uninstall(ctx, "acme/omaphoto", false); err != nil {
		t.Fatal(err)
	}
}

func TestUserServiceMissingExecutableAndMultiple(t *testing.T) {
	s := manifest.Service{Type: "systemd-user", Unit: "omaphoto-sessiond.service", Exec: "missing/sessiond", Start: true}
	e, _ := serviceEnv(t, s)
	if _, err := e.in.Install(context.Background(), "acme/omaphoto", Options{}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing executable: %v", err)
	}
	s.Exec = "omaphoto-1/sessiond"
	setServiceManifest(t, e, map[string]manifest.Service{"session": s, "second": {Type: "systemd-user", Unit: "omaphoto-extra.service", Exec: "omaphoto-1/omaphoto"}})
	inst, err := e.in.Install(context.Background(), "acme/omaphoto", Options{})
	if err != nil || len(inst.Integrations) != 2 {
		t.Fatalf("multiple: %+v %v", inst, err)
	}
}

func TestUserServiceCollisionAndEdits(t *testing.T) {
	s := manifest.Service{Type: "systemd-user", Unit: "omaphoto-sessiond.service", Exec: "omaphoto-1/sessiond", Start: true}
	e, f := serviceEnv(t, s)
	f.foreign[s.Unit] = filepath.Join(t.TempDir(), s.Unit)
	if err := os.WriteFile(f.foreign[s.Unit], []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.active[s.Unit], f.enabled[s.Unit] = true, true
	if _, err := e.in.Install(context.Background(), "acme/omaphoto", Options{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign active/enabled unit: %v", err)
	}
	delete(f.foreign, s.Unit)
	f.active[s.Unit], f.enabled[s.Unit] = false, false
	if _, err := e.in.Install(context.Background(), "acme/omaphoto", Options{}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.paths.UserUnits, s.Unit)
	if err := os.WriteFile(path, []byte("changed by user"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.in.Uninstall(context.Background(), "acme/omaphoto", false); !errors.Is(err, ErrConflict) {
		t.Fatalf("edited unit: %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "changed by user" {
		t.Fatal("user edit was removed")
	}
}

func TestServiceCommandSafety(t *testing.T) {
	q, err := quoteSystemdPath(`/home/user/my apps/a"b%$HOME/run`)
	if err != nil || q != `"/home/user/my apps/a\"b%%$$HOME/run"` {
		t.Fatalf("quoted = %q, %v", q, err)
	}
	if _, err := quoteSystemdPath("/home/u/a\n[Install]"); err == nil {
		t.Fatal("newline accepted")
	}
}

func TestOmakadeStylePackageLayout(t *testing.T) {
	e := newEnv(t)
	f := &fakeSystemd{dir: e.paths.UserUnits, enabled: map[string]bool{}, active: map[string]bool{}, foreign: map[string]string{}}
	e.in.systemctl = f.run
	data := tarZst(t, []entry{
		{name: "usr/bin/omaphoto", body: string(elfBin), mode: 0o755},
		{name: "usr/bin/omakade-sessiond", body: string(elfBin), mode: 0o755},
		{name: "usr/lib/systemd/user/omakade-sessiond.service", body: "[Service]\nExecStart=/usr/bin/omakade-sessiond\n", mode: 0o644},
		{name: "usr/share/omakade/sessiond-profiles.json", body: "{}", mode: 0o644},
	})
	e.publish(t, "v1", data, asset.FormatPkg, true)
	d, err := e.st.GetApp(context.Background(), "acme/omaphoto")
	if err != nil {
		t.Fatal(err)
	}
	d.App.Manifest = (&manifest.Manifest{Kind: manifest.KindApp,
		Linux: map[string]manifest.Target{"amd64": {Exec: "usr/bin/omaphoto"}},
		Services: map[string]manifest.Service{"sessiond": {Type: "systemd-user", Unit: "omakade-sessiond.service",
			Exec: "usr/bin/omakade-sessiond", Enable: true, Start: true, Restart: "on-failure"}}}).Encode()
	if err := e.st.SaveIndexed(context.Background(), d.Repo, d.App, d.Assets); err != nil {
		t.Fatal(err)
	}
	inst, err := e.in.Install(context.Background(), "acme/omaphoto", Options{})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(e.paths.UserUnits, "omakade-sessiond.service"))
	if err != nil || !strings.Contains(string(generated), filepath.Join(inst.Files[0], "usr/bin/omakade-sessiond")) ||
		strings.Contains(string(generated), "ExecStart=/usr/bin/omakade-sessiond") {
		t.Fatalf("generated unit = %s %v", generated, err)
	}
	if _, err := os.Stat(filepath.Join(inst.Files[0], "usr/share/omakade/sessiond-profiles.json")); err != nil {
		t.Fatal(err)
	}
	if !f.enabled["omakade-sessiond.service"] || !f.active["omakade-sessiond.service"] {
		t.Fatal("service not enabled and started")
	}
}
