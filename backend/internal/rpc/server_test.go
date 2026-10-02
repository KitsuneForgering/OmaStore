package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/github"
	"github.com/KitsuneForgering/OmaStore/backend/internal/index"
	"github.com/KitsuneForgering/OmaStore/backend/internal/install"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
	"github.com/KitsuneForgering/OmaStore/backend/internal/sysdeps"
)

// fakeBackend controls the pace of long operations through channels.
type fakeBackend struct {
	mu          sync.Mutex
	installed   map[string]string
	release     chan struct{} // releases Install/Index when closed or received from
	installErr  error
	uninstalled []string
}

func newFake() *fakeBackend {
	return &fakeBackend{installed: map[string]string{}, release: make(chan struct{})}
}

func (f *fakeBackend) ListApps(ctx context.Context, fl store.Filter) ([]store.ListItem, error) {
	items := []store.ListItem{
		{App: store.App{FullName: "acme/photo", Name: "Photo", Category: "Graphics", Installable: true},
			Stars: 10, LatestTag: "v2", InstalledVersion: "v1"},
		{App: store.App{FullName: "acme/vm", Name: "VM", Category: "System", Installable: true}, LatestTag: "v1"},
	}
	if fl.Category != "" {
		var out []store.ListItem
		for _, it := range items {
			if it.Category == fl.Category {
				out = append(out, it)
			}
		}
		return out, nil
	}
	return items, nil
}

func (f *fakeBackend) GetApp(ctx context.Context, name string) (*store.AppDetail, error) {
	if name != "acme/photo" {
		return nil, store.ErrNotFound
	}
	return &store.AppDetail{
		App:     store.App{FullName: name, Name: "Photo", Readme: "# Photo"},
		Repo:    store.Repo{FullName: name, LatestTag: "v2", License: "MIT"},
		Assets:  []store.Asset{{Name: "p.tar.gz", Format: "tar.gz", Digest: "sha256:" + strings.Repeat("ab", 32)}, {Name: "p.bin", Format: "binary"}},
		Install: &store.Install{FullName: name, Version: "v1"},
	}, nil
}

func (f *fakeBackend) Similar(ctx context.Context, name string, limit int) ([]store.ListItem, error) {
	if name != "acme/photo" {
		return nil, store.ErrNotFound
	}
	items := []store.ListItem{
		{App: store.App{FullName: "acme/draw", Name: "Draw"}},
		{App: store.App{FullName: "acme/paint", Name: "Paint"}},
	}
	if limit < len(items) {
		items = items[:limit]
	}
	return items, nil
}

func (f *fakeBackend) Categories(ctx context.Context) ([]store.CategoryCount, error) {
	return []store.CategoryCount{{Category: "Graphics", Count: 1}}, nil
}

func (f *fakeBackend) ListInstalls(ctx context.Context) ([]store.Install, error) {
	return []store.Install{{FullName: "acme/photo", Version: "v1"}}, nil
}

func (f *fakeBackend) wait(ctx context.Context) error {
	select {
	case <-f.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakeBackend) Index(ctx context.Context, opts index.Options) (index.Stats, error) {
	opts.Progress(index.Progress{Stage: index.StageIndex, Total: 2, Done: 1, Current: "acme/photo",
		Stats: index.Stats{Updated: 1}})
	if err := f.wait(ctx); err != nil {
		return index.Stats{}, err
	}
	return index.Stats{Updated: 2}, nil
}

func (f *fakeBackend) Install(ctx context.Context, name string, opts install.Options) (*store.Install, error) {
	p := opts.Progress
	p(install.Progress{Stage: install.StageDownload, Done: 5, Total: 10})
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	if f.installErr != nil {
		return nil, f.installErr
	}
	p(install.Progress{Stage: install.StageDone})
	f.mu.Lock()
	f.installed[name] = "v2"
	f.mu.Unlock()
	return &store.Install{FullName: name, Version: "v2", ExecPath: "/x"}, nil
}

func (f *fakeBackend) Update(ctx context.Context, name string, opts install.Options) (*store.Install, error) {
	return f.Install(ctx, name, opts)
}

func (f *fakeBackend) Rollback(ctx context.Context, name string) (*store.Install, error) {
	return nil, install.ErrNoPrevious
}

func (f *fakeBackend) Uninstall(ctx context.Context, name string, force bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.installed[name]; !ok && name != "acme/photo" {
		return install.ErrNotInstalled
	}
	f.uninstalled = append(f.uninstalled, name)
	return nil
}

func (f *fakeBackend) Image(ctx context.Context, url string) (string, error) {
	if !strings.HasPrefix(url, "https://") {
		return "", errors.New("invalid URL")
	}
	return "/cache/" + filepath.Base(url), nil
}

func (f *fakeBackend) Check(ctx context.Context, name string, m *string) (*index.Report, error) {
	if name == "acme/limited" {
		return nil, &github.RateLimitError{Reset: time.Now().Add(time.Hour)}
	}
	r := &index.Report{Repo: name, Name: "Photo", Checks: []index.Check{{Status: index.CheckOK, Item: "Release", Detail: "v1"}}}
	if m == nil {
		r.Checks = append(r.Checks, index.Check{Status: index.CheckFail, Item: "omastore.toml", Detail: "missing", Fix: "add it"})
		r.SuggestedManifest = "kind = \"app\"\n"
	}
	return r, nil
}

func (f *fakeBackend) Starred(ctx context.Context, name string) (bool, error) {
	if name == "acme/anon" {
		return false, github.ErrNoToken
	}
	return name == "acme/photo", nil
}

func (f *fakeBackend) Star(ctx context.Context, name string, starred bool) (int, error) {
	if name == "acme/anon" {
		return 0, github.ErrNoToken
	}
	if starred {
		return 11, nil
	}
	return 10, nil
}

func (f *fakeBackend) SysDeps(ctx context.Context, name string) (sysdeps.Report, error) {
	rep := sysdeps.Report{Source: "PKGBUILD", Deps: []sysdeps.DepState{
		{Dep: sysdeps.Dep{Spec: "glibc"}, Status: sysdeps.StatusInstalled},
		{Dep: sysdeps.Dep{Spec: "ffmpeg>=6", Reason: "video", Optional: true}, Status: sysdeps.StatusAvailable, Package: "extra/ffmpeg"},
		{Dep: sysdeps.Dep{Spec: "aur-only"}, Status: sysdeps.StatusUnavailable},
	}}
	if name == "acme/other-os" {
		for i := range rep.Deps {
			rep.Deps[i].Status, rep.Deps[i].Package = sysdeps.StatusUnknown, ""
		}
		return rep, sysdeps.ErrNoPacman
	}
	return rep, nil
}

func (f *fakeBackend) InstallSysDeps(ctx context.Context, name string) (sysdeps.Report, error) {
	if err := f.wait(ctx); err != nil {
		return sysdeps.Report{}, err
	}
	if name == "acme/denied" {
		return sysdeps.Report{}, sysdeps.ErrDenied
	}
	return sysdeps.Report{Source: "PKGBUILD"}, nil
}

func (f *fakeBackend) SelfStatus(ctx context.Context) (install.SelfStatus, error) {
	return install.SelfStatus{Mode: install.SelfManaged, Version: "0.1.0", Latest: "0.2.0", UpdateAvailable: true,
		Notes: "notes"}, nil
}

func (f *fakeBackend) SelfUpdate(ctx context.Context, p func(install.Progress)) (*install.SelfResult, error) {
	p(install.Progress{Stage: install.StageDownload, Done: 1, Total: 2})
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	return &install.SelfResult{From: "0.1.0", To: "0.2.0", GUI: "/home/u/.local/bin/omastore-gui"}, nil
}

// client is a test client that separates responses from notifications.
type client struct {
	t     *testing.T
	c     net.Conn
	seq   int
	resps chan map[string]json.RawMessage
	notes chan map[string]json.RawMessage
}

func startServer(t *testing.T, b Backend) (*Server, string) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "omastore.sock")
	l, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(b, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.jobs.minInterval = 0
	go s.Serve(l)
	t.Cleanup(s.Shutdown)
	return s, sock
}

func dial(t *testing.T, sock string) *client {
	t.Helper()
	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	cl := &client{t: t, c: c, resps: make(chan map[string]json.RawMessage, 100), notes: make(chan map[string]json.RawMessage, 100)}
	go func() {
		sc := bufio.NewScanner(c)
		sc.Buffer(nil, 4<<20)
		for sc.Scan() {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Errorf("invalid line from the server: %q", sc.Text())
				continue
			}
			if _, ok := m["id"]; ok {
				cl.resps <- m
			} else {
				cl.notes <- m
			}
		}
	}()
	return cl
}

func (cl *client) raw(line string) map[string]json.RawMessage {
	cl.t.Helper()
	if _, err := cl.c.Write([]byte(line + "\n")); err != nil {
		cl.t.Fatal(err)
	}
	select {
	case r := <-cl.resps:
		return r
	case <-time.After(3 * time.Second):
		cl.t.Fatalf("no response to %s", line)
		return nil
	}
}

// call makes a call and decodes result into out; returns the RPC error.
func (cl *client) call(method string, params any, out any) *Error {
	cl.t.Helper()
	cl.seq++
	req := map[string]any{"jsonrpc": "2.0", "id": cl.seq, "method": method}
	if params != nil {
		req["params"] = params
	}
	b, _ := json.Marshal(req)
	r := cl.raw(string(b))
	if string(r["id"]) != fmt.Sprint(cl.seq) {
		cl.t.Fatalf("response id = %s, want %d", r["id"], cl.seq)
	}
	if e, ok := r["error"]; ok {
		var re Error
		json.Unmarshal(e, &re)
		return &re
	}
	if out != nil {
		if err := json.Unmarshal(r["result"], out); err != nil {
			cl.t.Fatalf("result: %v (%s)", err, r["result"])
		}
	}
	return nil
}

// waitNote waits for a notification with the given method and filter.
func (cl *client) waitNote(method string, match func(Job) bool) Job {
	cl.t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case n := <-cl.notes:
			var m string
			json.Unmarshal(n["method"], &m)
			if m != method {
				continue
			}
			var j Job
			json.Unmarshal(n["params"], &j)
			if match == nil || match(j) {
				return j
			}
		case <-deadline:
			cl.t.Fatalf("notification %s did not arrive", method)
			return Job{}
		}
	}
}

// daemon.hello advertises exactly the methods the dispatcher answers: an
// interface decides what to offer from that list, so a method added to the
// switch but not to Methods (or the reverse) would hide a feature or offer a
// broken one.
func TestHelloAdvertisesEveryMethod(t *testing.T) {
	src, err := os.ReadFile("methods.go")
	if err != nil {
		t.Fatal(err)
	}
	handled := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\tcase ("[a-z]+\.[a-zA-Z]+"(?:, "[a-z]+\.[a-zA-Z]+")*):`).FindAllStringSubmatch(string(src), -1) {
		for _, name := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(m[1], -1) {
			handled[name[1]] = true
		}
	}
	advertised := map[string]bool{}
	for _, m := range Methods {
		advertised[m] = true
		if !handled[m] {
			t.Errorf("%s is advertised but not handled", m)
		}
	}
	for m := range handled {
		if !advertised[m] {
			t.Errorf("%s is handled but missing from Methods", m)
		}
	}
	if len(handled) < 20 {
		t.Fatalf("parsed only %d methods: %v", len(handled), handled)
	}

	_, sock := startServer(t, newFake())
	var hello struct {
		Protocol int      `json:"protocol"`
		Methods  []string `json:"methods"`
	}
	if e := dial(t, sock).call("daemon.hello", nil, &hello); e != nil || hello.Protocol != ProtocolVersion || len(hello.Methods) != len(Methods) {
		t.Errorf("hello: %+v %v", hello, e)
	}
}

func TestProtocolErrors(t *testing.T) {
	_, sock := startServer(t, newFake())
	cl := dial(t, sock)

	var hello map[string]any
	if e := cl.call("daemon.hello", nil, &hello); e != nil || hello["protocol"] != float64(ProtocolVersion) {
		t.Errorf("hello: %v %v", hello, e)
	}
	if e := cl.call("nope", nil, nil); e == nil || e.Code != CodeMethodNotFound {
		t.Errorf("unknown method: %v", e)
	}
	r := cl.raw(`{not json`)
	if !strings.Contains(string(r["error"]), fmt.Sprint(CodeParse)) || string(r["id"]) != "null" {
		t.Errorf("parse: %s", r["error"])
	}
	r = cl.raw(`{"jsonrpc":"1.0","id":7,"method":"daemon.hello"}`)
	if !strings.Contains(string(r["error"]), fmt.Sprint(CodeInvalidRequest)) || string(r["id"]) != "7" {
		t.Errorf("wrong version: %v", r)
	}
	if e := cl.call("catalog.get", map[string]any{"repo": "no-slash"}, nil); e == nil || e.Code != CodeInvalidParams {
		t.Errorf("invalid repo: %v", e)
	}
	if e := cl.call("catalog.list", map[string]any{"bogus": 1}, nil); e == nil || e.Code != CodeInvalidParams {
		t.Errorf("unknown field: %v", e)
	}
	if e := cl.call("catalog.list", map[string]any{"limit": -1}, nil); e == nil || e.Code != CodeInvalidParams {
		t.Errorf("negative limit: %v", e)
	}
	if e := cl.call("catalog.get", map[string]any{"repo": "x/y"}, nil); e == nil || e.Code != CodeNotFound {
		t.Errorf("not found: %v", e)
	}

	// Client notification (no id): no response; the next call gets its
	// own response.
	cl.c.Write([]byte(`{"jsonrpc":"2.0","method":"daemon.hello"}` + "\n"))
	if e := cl.call("daemon.hello", nil, nil); e != nil {
		t.Error(e)
	}
}

func TestCatalogDTOs(t *testing.T) {
	_, sock := startServer(t, newFake())
	cl := dial(t, sock)

	var items []map[string]any
	if e := cl.call("catalog.list", nil, &items); e != nil {
		t.Fatal(e)
	}
	if len(items) != 2 || items[0]["repo"] != "acme/photo" || items[0]["updateAvailable"] != true ||
		items[1]["updateAvailable"] != false || items[0]["latestVersion"] != "v2" {
		t.Errorf("items = %v", items)
	}
	if _, ok := items[1]["screenshots"].([]any); !ok {
		t.Error("screenshots must be [] and not null")
	}
	var g []AppItem
	cl.call("catalog.list", map[string]any{"category": "System"}, &g)
	if len(g) != 1 || g[0].Repo != "acme/vm" {
		t.Errorf("filter: %v", g)
	}

	var d AppDetail
	if e := cl.call("catalog.get", map[string]any{"repo": "acme/photo"}, &d); e != nil {
		t.Fatal(e)
	}
	if d.License != "MIT" || d.Install == nil || d.Install.Version != "v1" || len(d.Assets) != 2 ||
		d.Assets[0].Checksum == "" || d.Assets[1].Checksum != "" || !d.UpdateAvailable {
		t.Errorf("detail = %+v", d)
	}

	var sim []AppItem
	if e := cl.call("catalog.similar", map[string]any{"repo": "acme/photo", "limit": 1}, &sim); e != nil || len(sim) != 1 || sim[0].Repo != "acme/draw" {
		t.Errorf("similar: %v %v", sim, e)
	}
	if e := cl.call("catalog.similar", map[string]any{"repo": "acme/photo"}, &sim); e != nil || len(sim) != 2 {
		t.Errorf("similar without limit: %v %v", sim, e)
	}
	if e := cl.call("catalog.similar", map[string]any{"repo": "x/y"}, nil); e == nil || e.Code != CodeNotFound {
		t.Errorf("similar missing: %v", e)
	}

	var cats []map[string]any
	cl.call("catalog.categories", nil, &cats)
	if len(cats) != 1 || cats[0]["name"] != "Graphics" {
		t.Errorf("categories = %v", cats)
	}
	var ins []InstallInfo
	cl.call("installs.list", nil, &ins)
	if len(ins) != 1 || ins[0].Repo != "acme/photo" {
		t.Errorf("installed = %v", ins)
	}
	var img map[string]string
	if e := cl.call("image.get", map[string]any{"url": "https://x/a.png"}, &img); e != nil || img["path"] != "/cache/a.png" {
		t.Errorf("image: %v %v", img, e)
	}
}

func TestInstallJobLifecycle(t *testing.T) {
	f := newFake()
	_, sock := startServer(t, f)
	cl := dial(t, sock)
	other := dial(t, sock) // notifications go to every client

	var j Job
	if e := cl.call("install.start", map[string]any{"repo": "acme/vm"}, &j); e != nil {
		t.Fatal(e)
	}
	if j.ID == "" || j.Kind != KindInstall || j.State != StateRunning {
		t.Fatalf("job = %+v", j)
	}
	byID := func(x Job) bool { return x.ID == j.ID }
	p := cl.waitNote("job.progress", byID)
	if p.Stage != install.StageDownload || p.Done != 5 || p.Total != 10 {
		t.Errorf("progress = %+v", p)
	}

	// Same app busy: a new install and an uninstall are refused.
	if e := cl.call("install.start", map[string]any{"repo": "ACME/vm"}, nil); e == nil || e.Code != CodeBusy {
		t.Errorf("duplicate install: %v", e)
	}
	if e := cl.call("install.uninstall", map[string]any{"repo": "acme/vm"}, nil); e == nil || e.Code != CodeBusy {
		t.Errorf("uninstall during install: %v", e)
	}
	// Another app can install in parallel.
	var j2 Job
	if e := cl.call("install.start", map[string]any{"repo": "acme/photo"}, &j2); e != nil {
		t.Errorf("different app: %v", e)
	}

	var running []Job
	cl.call("jobs.list", nil, &running)
	if len(running) != 2 {
		t.Errorf("jobs = %+v", running)
	}

	close(f.release)
	done := other.waitNote("job.done", byID)
	if done.State != StateDone || done.Finished.IsZero() {
		t.Errorf("done = %+v", done)
	}
	other.waitNote("catalog.changed", nil)

	if e := cl.call("install.uninstall", map[string]any{"repo": "acme/vm"}, nil); e != nil {
		t.Errorf("uninstall: %v", e)
	}
	if e := cl.call("install.uninstall", map[string]any{"repo": "acme/none"}, nil); e == nil || e.Code != CodeNotInstalled {
		t.Errorf("uninstall missing: %v", e)
	}
}

// Apps written by a running index reach the frontend before the index ends.
func TestIndexSendsCatalogChangedWhileRunning(t *testing.T) {
	f := newFake()
	_, sock := startServer(t, f)
	cl := dial(t, sock)

	var idx Job
	if e := cl.call("index.start", nil, &idx); e != nil {
		t.Fatal(e)
	}
	cl.waitNote("catalog.changed", nil)
	var js []Job
	if e := cl.call("jobs.list", nil, &js); e != nil {
		t.Fatal(e)
	}
	if len(js) != 1 || js[0].State != StateRunning || js[0].Stage != index.StageIndex {
		t.Errorf("jobs = %+v, want the index running in stage %q", js, index.StageIndex)
	}
	close(f.release)
	cl.waitNote("job.done", func(j Job) bool { return j.ID == idx.ID })
}

func TestJobErrorsAndCancel(t *testing.T) {
	f := newFake()
	_, sock := startServer(t, f)
	cl := dial(t, sock)

	var idx Job
	cl.call("index.start", map[string]any{"force": true}, &idx)
	if e := cl.call("index.start", nil, nil); e == nil || e.Code != CodeBusy {
		t.Errorf("second index: %v", e)
	}
	cl.waitNote("job.progress", func(j Job) bool { return j.ID == idx.ID && j.Message == "acme/photo" })
	if e := cl.call("jobs.cancel", map[string]any{"job": idx.ID}, nil); e != nil {
		t.Fatal(e)
	}
	c := cl.waitNote("job.failed", func(j Job) bool { return j.ID == idx.ID })
	if res, ok := c.Result.(map[string]any); !ok || res["updated"] == nil {
		t.Errorf("index result without camelCase: %#v", c.Result)
	}
	if c.State != StateCanceled || c.Error == nil || c.Error.Code != CodeCanceled {
		t.Errorf("canceled = %+v", c)
	}
	if e := cl.call("jobs.cancel", map[string]any{"job": "job-999"}, nil); e == nil || e.Code != CodeNotFound {
		t.Errorf("cancel missing: %v", e)
	}

	// Backend errors become stable codes.
	cases := []struct {
		err  error
		code int
	}{
		{install.ErrNotInstallable, CodeNotInstallable},
		{fmt.Errorf("x: %w", install.ErrChecksum), CodeChecksum},
		{&github.RateLimitError{Reset: time.Now()}, CodeRateLimited},
		{install.ErrConflict, CodeConflict},
	}
	close(f.release)
	for _, tc := range cases {
		f.installErr = tc.err
		var j Job
		cl.call("install.start", map[string]any{"repo": "acme/vm"}, &j)
		failed := cl.waitNote("job.failed", func(x Job) bool { return x.ID == j.ID })
		if failed.State != StateFailed || failed.Error == nil || failed.Error.Code != tc.code {
			t.Errorf("%v: %+v", tc.err, failed.Error)
		}
	}
}

func TestShutdownCancelsJobs(t *testing.T) {
	f := newFake()
	s, sock := startServer(t, f)
	cl := dial(t, sock)
	var j Job
	cl.call("install.start", map[string]any{"repo": "acme/vm"}, &j)
	cl.waitNote("job.progress", nil)

	done := make(chan struct{})
	go func() { s.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown did not finish")
	}
	if _, err := net.Dial("unix", sock); err == nil {
		t.Error("server still accepts connections")
	}
}

func TestOversizedMessage(t *testing.T) {
	_, sock := startServer(t, newFake())
	cl := dial(t, sock)
	big := `{"jsonrpc":"2.0","id":1,"method":"x","params":"` + strings.Repeat("a", maxMessage) + `"}`
	r := cl.raw(big)
	if !strings.Contains(string(r["error"]), fmt.Sprint(CodeInvalidRequest)) {
		t.Errorf("large message: %s", r["error"])
	}
}

func TestListenSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "sub", "omastore.sock")

	// An orphan socket (file with nobody listening) is replaced.
	os.MkdirAll(filepath.Dir(sock), 0o700)
	orphan, _ := net.Listen("unix", sock)
	orphan.(*net.UnixListener).SetUnlinkOnClose(false)
	orphan.Close()
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("precondition: %v", err)
	}

	l, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	st, _ := os.Stat(sock)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("permission = %v", st.Mode().Perm())
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if _, err := Listen(sock); err == nil {
		t.Error("second daemon should fail")
	}
}

func TestActivationListenerIgnoredWithoutEnv(t *testing.T) {
	t.Setenv("LISTEN_PID", "")
	l, err := ActivationListener()
	if l != nil || err != nil {
		t.Errorf("l=%v err=%v", l, err)
	}
	t.Setenv("LISTEN_PID", "1") // another process
	t.Setenv("LISTEN_FDS", "1")
	if l, _ := ActivationListener(); l != nil {
		t.Error("should not use another process's fds")
	}
}

func TestJobsPrune(t *testing.T) {
	js := newJobs(func(string, any) {})
	js.keep = 3
	for i := 0; i < 10; i++ {
		j, err := js.start(context.Background(), KindInstall, fmt.Sprintf("a/%d", i),
			func(ctx context.Context, r func(progress)) (any, error) { return nil, nil }, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = j
	}
	js.wg.Wait()
	if n := len(js.list()); n != 3 {
		t.Errorf("history = %d, want 3", n)
	}
}

func TestListenPathTooLong(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("d", 120), "omastore.sock")
	_, err := Listen(long)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("err = %v", err)
	}
}

func TestIdleFor(t *testing.T) {
	f := newFake()
	s, sock := startServer(t, f)
	time.Sleep(20 * time.Millisecond)
	if s.IdleFor() < 10*time.Millisecond {
		t.Errorf("freshly started without clients should be idle: %v", s.IdleFor())
	}

	cl := dial(t, sock)
	cl.call("daemon.hello", nil, nil)
	if s.IdleFor() != 0 {
		t.Error("not idle with a connected client")
	}
	// A running job keeps the daemon active even without clients.
	var j Job
	cl.call("install.start", map[string]any{"repo": "acme/vm"}, &j)
	cl.waitNote("job.progress", nil)
	cl.c.Close()
	time.Sleep(100 * time.Millisecond)
	if s.IdleFor() != 0 {
		t.Error("not idle with a running job")
	}
	close(f.release)
	deadline := time.Now().Add(2 * time.Second)
	for s.jobs.running() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	// The end of the job counts as activity: idleness starts now, not when
	// the client disconnected.
	if d := s.IdleFor(); d > 40*time.Millisecond {
		t.Errorf("idleness should count from the end of the job: %v", d)
	}
	time.Sleep(20 * time.Millisecond)
	if d := s.IdleFor(); d < 10*time.Millisecond {
		t.Errorf("without clients or jobs it should be idle: %v", d)
	}
}

func TestAuthorCheck(t *testing.T) {
	_, sock := startServer(t, newFake())
	cl := dial(t, sock)

	var r CheckReport
	if e := cl.call("author.check", map[string]any{"repo": "acme/photo"}, &r); e != nil {
		t.Fatal(e)
	}
	if r.Compatible || len(r.Checks) != 2 || r.Checks[1].Status != "fail" || r.Checks[1].Fix != "add it" ||
		r.SuggestedManifest == "" || r.Screenshots == nil {
		t.Errorf("report = %+v", r)
	}
	if e := cl.call("author.check", map[string]any{"repo": "acme/photo", "manifest": ""}, &r); e != nil {
		t.Fatal(e)
	}
	if !r.Compatible {
		t.Errorf("an (empty) local manifest was not passed through: %+v", r)
	}
	if e := cl.call("author.check", map[string]any{"repo": "nope"}, nil); e == nil || e.Code != CodeInvalidParams {
		t.Errorf("invalid repo: %+v", e)
	}
	if e := cl.call("author.check", map[string]any{"repo": "acme/limited"}, nil); e == nil || e.Code != CodeRateLimited {
		t.Errorf("rate limit: %+v", e)
	}
}

// A client that stops reading must not hold back notifications (sent from
// inside the indexer) nor the other clients: once its queue fills up, it is
// disconnected.
func TestSlowClientDoesNotBlockBroadcast(t *testing.T) {
	s, sock := startServer(t, newFake())
	stuck, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer stuck.Close()
	good, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer good.Close()
	var got atomic.Int64
	lines := make(chan int, 1)
	go func() {
		sc := bufio.NewScanner(good)
		n := 0
		for sc.Scan() {
			n++
			got.Store(int64(n))
			if strings.Contains(sc.Text(), `"last"`) {
				lines <- n
				return
			}
		}
	}()
	for deadline := time.Now().Add(2 * time.Second); ; {
		s.mu.Lock()
		n := len(s.conns)
		s.mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d connections", n)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Far more than the socket buffer plus the queue. The good client is
	// waited for between batches, so only the stuck one falls behind.
	const total, batch = 20000, 200
	var spent time.Duration
	for i := 0; i < total; i += batch {
		start := time.Now()
		for j := i; j < i+batch; j++ {
			s.broadcast("catalog.changed", map[string]int{"i": j})
		}
		spent += time.Since(start)
		for deadline := time.Now().Add(5 * time.Second); got.Load() < int64(i+batch); {
			if time.Now().After(deadline) {
				t.Fatalf("good client stuck at %d of %d", got.Load(), i+batch)
			}
			time.Sleep(time.Millisecond)
		}
	}
	s.broadcast("catalog.changed", map[string]string{"last": "yes"})
	if spent > 2*time.Second {
		t.Errorf("broadcast took %s", spent)
	}
	select {
	case n := <-lines:
		if n != total+1 {
			t.Errorf("good client got %d messages, want %d", n, total+1)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("good client did not get the last message")
	}
	s.mu.Lock()
	n := len(s.conns)
	s.mu.Unlock()
	if n != 1 {
		t.Errorf("%d connections; the stuck client should have been dropped", n)
	}
}

func TestStarMethods(t *testing.T) {
	_, sock := startServer(t, newFake())
	cl := dial(t, sock)

	var got map[string]any
	if e := cl.call("star.get", map[string]any{"repo": "acme/photo"}, &got); e != nil || got["starred"] != true {
		t.Errorf("star.get: %v %v", got, e)
	}
	if e := cl.call("star.set", map[string]any{"repo": "acme/vm", "starred": true}, &got); e != nil ||
		got["starred"] != true || got["stars"] != float64(11) {
		t.Errorf("star.set: %v %v", got, e)
	}
	cl.waitNote("catalog.changed", nil)
	if e := cl.call("star.set", map[string]any{"repo": "acme/anon", "starred": true}, nil); e == nil || e.Code != CodeAuthRequired {
		t.Errorf("without token: %v", e)
	}
	if e := cl.call("star.get", map[string]any{"repo": "bad"}, nil); e == nil || e.Code != CodeInvalidParams {
		t.Errorf("invalid repo: %v", e)
	}
}

func TestDepsMethods(t *testing.T) {
	f := newFake()
	_, sock := startServer(t, f)
	cl := dial(t, sock)

	var rep DepsReport
	if e := cl.call("deps.check", map[string]any{"repo": "acme/photo"}, &rep); e != nil {
		t.Fatal(e)
	}
	if !rep.Pacman || rep.Source != "PKGBUILD" || len(rep.Deps) != 3 || rep.Missing != 2 ||
		len(rep.ToInstall) != 1 || rep.ToInstall[0] != "extra/ffmpeg" {
		t.Fatalf("report = %+v", rep)
	}
	if d := rep.Deps[1]; d.Name != "ffmpeg" || d.Spec != "ffmpeg>=6" || !d.Optional || d.Reason != "video" || d.Status != "available" {
		t.Errorf("dep = %+v", d)
	}
	// Without pacman the dependencies are still listed.
	if e := cl.call("deps.check", map[string]any{"repo": "acme/other-os"}, &rep); e != nil || rep.Pacman || rep.Deps[0].Status != "unknown" {
		t.Errorf("no pacman: %+v %v", rep, e)
	}

	var j Job
	if e := cl.call("deps.install", map[string]any{"repo": "acme/denied"}, &j); e != nil || j.Kind != KindDeps {
		t.Fatalf("deps.install: %+v %v", j, e)
	}
	// pacman has one lock: a second dependency job waits for the first.
	if e := cl.call("deps.install", map[string]any{"repo": "acme/photo"}, nil); e == nil || e.Code != CodeBusy {
		t.Errorf("second deps job: %v", e)
	}
	f.release <- struct{}{}
	failed := cl.waitNote("job.failed", func(x Job) bool { return x.ID == j.ID })
	if failed.Error == nil || failed.Error.Code != CodeDenied {
		t.Errorf("denied job = %+v", failed)
	}
}

func TestSelfMethods(t *testing.T) {
	f := newFake()
	s, sock := startServer(t, f)
	cl := dial(t, sock)

	var st SelfInfo
	if e := cl.call("self.status", nil, &st); e != nil || st.Mode != "self" || st.Latest != "0.2.0" || !st.UpdateAvailable {
		t.Fatalf("self.status: %+v %v", st, e)
	}
	var j Job
	if e := cl.call("self.update", nil, &j); e != nil || j.Kind != KindSelf {
		t.Fatalf("self.update: %+v %v", j, e)
	}
	if e := cl.call("self.update", nil, nil); e == nil || e.Code != CodeBusy {
		t.Errorf("second self.update: %v", e)
	}
	// No restart while a job runs: it would be canceled halfway.
	if e := cl.call("self.restart", nil, nil); e == nil || e.Code != CodeBusy {
		t.Errorf("self.restart during a job: %v", e)
	}
	f.release <- struct{}{}
	done := cl.waitNote("job.done", func(x Job) bool { return x.ID == j.ID })
	var r SelfUpdateResult
	b, _ := json.Marshal(done.Result)
	if err := json.Unmarshal(b, &r); err != nil || r.To != "0.2.0" || r.GUI == "" {
		t.Errorf("self.update result = %s (%v)", b, err)
	}

	restartDelay = 0
	if e := cl.call("self.restart", nil, nil); e != nil {
		t.Fatalf("self.restart: %v", e)
	}
	select {
	case <-s.Restart():
	case <-time.After(3 * time.Second):
		t.Fatal("Restart() was not closed")
	}
	// A second request is harmless.
	if e := cl.call("self.restart", nil, nil); e != nil {
		t.Errorf("second self.restart: %v", e)
	}
}
