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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/github"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/install"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
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
		Assets:  []store.Asset{{Name: "p.tar.gz", Format: "tar.gz", Digest: "sha256:x"}, {Name: "p.bin", Format: "binary"}},
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
	opts.Progress(index.Progress{Total: 2, Done: 1, Current: "acme/photo"})
	if err := f.wait(ctx); err != nil {
		return index.Stats{}, err
	}
	return index.Stats{Updated: 2}, nil
}

func (f *fakeBackend) Install(ctx context.Context, name string, p func(install.Progress)) (*store.Install, error) {
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

func (f *fakeBackend) Update(ctx context.Context, name string, p func(install.Progress)) (*store.Install, error) {
	return f.Install(ctx, name, p)
}

func (f *fakeBackend) Uninstall(ctx context.Context, name string) error {
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
		!d.Assets[0].Verified || d.Assets[1].Verified || !d.UpdateAvailable {
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
