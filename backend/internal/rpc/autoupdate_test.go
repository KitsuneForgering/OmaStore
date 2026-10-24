package rpc

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/index"
	"github.com/KitsuneForgering/OmaStore/backend/internal/install"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

// autoFake adds automatic updates to fakeBackend.
type autoFake struct {
	*fakeBackend
	amu        sync.Mutex
	on         bool
	due        bool
	marked     bool
	strict     bool
	indexed    [][]string
	ready      []store.ListItem
	indexStats *index.Stats
	autoLists  int
	started    chan string
	finish     chan struct{}
	active     int
	peak       int
}

func (f *autoFake) AutoUpdate(context.Context) (bool, error) {
	f.amu.Lock()
	defer f.amu.Unlock()
	return f.on, nil
}

func (f *autoFake) SetAutoUpdate(_ context.Context, on bool) error {
	f.amu.Lock()
	defer f.amu.Unlock()
	f.on = on
	return nil
}

func (f *autoFake) AutoUpdates(context.Context) (ready, waiting []store.ListItem, err error) {
	f.amu.Lock()
	defer f.amu.Unlock()
	f.autoLists++
	if f.ready != nil {
		return f.ready, nil, nil
	}
	return []store.ListItem{{App: store.App{FullName: "acme/photo", Name: "Photo"}, LatestTag: "v2", InstalledVersion: "v1"}},
		[]store.ListItem{{App: store.App{FullName: "acme/raw", Name: "Raw"}, LatestTag: "v3", InstalledVersion: "v2"}}, nil
}

func (f *autoFake) AutoCheckDue(context.Context, time.Duration) (bool, error) {
	f.amu.Lock()
	defer f.amu.Unlock()
	return f.on && f.due, nil
}

func (f *autoFake) RequireProvenance(context.Context) (bool, error) {
	f.amu.Lock()
	defer f.amu.Unlock()
	return f.strict, nil
}

func (f *autoFake) SetRequireProvenance(_ context.Context, on bool) error {
	f.amu.Lock()
	defer f.amu.Unlock()
	f.strict = on
	return nil
}

func (f *autoFake) MarkAutoCheck(context.Context) error {
	f.amu.Lock()
	defer f.amu.Unlock()
	f.marked = true
	return nil
}

func (f *autoFake) Index(ctx context.Context, opts index.Options) (index.Stats, error) {
	f.amu.Lock()
	f.indexed = append(f.indexed, opts.Only)
	f.amu.Unlock()
	st, err := f.fakeBackend.Index(ctx, opts)
	if f.indexStats != nil {
		st = *f.indexStats
	}
	return st, err
}

func TestSettingsMethods(t *testing.T) {
	f := &autoFake{fakeBackend: newFake(), on: true}
	close(f.release)
	_, sock := startServer(t, f)
	cl := dial(t, sock)

	var st Settings
	if e := cl.call("settings.get", nil, &st); e != nil || !st.AutoUpdate {
		t.Fatalf("settings.get = %+v, %v; want autoUpdate on", st, e)
	}
	if e := cl.call("settings.set", map[string]any{"autoUpdate": false}, &st); e != nil || st.AutoUpdate {
		t.Fatalf("settings.set off = %+v, %v", st, e)
	}
	if e := cl.call("settings.set", map[string]any{"requireProvenance": true}, &st); e != nil || !st.RequireProvenance || st.AutoUpdate {
		t.Fatalf("settings.set requireProvenance = %+v, %v", st, e)
	}
	if e := cl.call("settings.set", map[string]any{"bogus": 1}, nil); e == nil || e.Code != CodeInvalidParams {
		t.Fatalf("unknown setting: %v, want invalid params", e)
	}
	// Turning it back on catches up at once.
	if e := cl.call("settings.set", map[string]any{"autoUpdate": true}, &st); e != nil || !st.AutoUpdate {
		t.Fatalf("settings.set on = %+v, %v", st, e)
	}
	cl.waitNote("job.started", func(j Job) bool { return j.Kind == KindUpdate && j.Repo == "acme/photo" })

	// A backend without automatic updates says so.
	_, sock = startServer(t, newFake())
	if e := dial(t, sock).call("settings.get", nil, nil); e == nil || e.Code != CodeMethodNotFound {
		t.Fatalf("settings.get without support: %v", e)
	}
}

// After an index, the verifiable updates install on their own; the others
// wait for the user.
func TestIndexStartsAutomaticUpdates(t *testing.T) {
	for _, on := range []bool{true, false} {
		f := &autoFake{fakeBackend: newFake(), on: on}
		close(f.release)
		_, sock := startServer(t, f)
		cl := dial(t, sock)
		var idx Job
		if e := cl.call("index.start", nil, &idx); e != nil {
			t.Fatal(e)
		}
		cl.waitNote("job.done", func(j Job) bool { return j.ID == idx.ID })
		if on {
			up := cl.waitNote("job.started", func(j Job) bool { return j.Kind == KindUpdate })
			if up.Repo != "acme/photo" {
				t.Errorf("updated %s, want only the verifiable acme/photo", up.Repo)
			}
			cl.waitNote("job.done", func(j Job) bool { return j.ID == up.ID })
			continue
		}
		time.Sleep(100 * time.Millisecond)
		var js []Job
		cl.call("jobs.list", nil, &js)
		for _, j := range js {
			if j.Kind == KindUpdate {
				t.Errorf("automatic updates off, but %s started", j.Repo)
			}
		}
	}
}

// On start, a stale check refreshes only the installed apps, records the
// check and installs their updates.
func TestAutoUpdateOnStart(t *testing.T) {
	f := &autoFake{fakeBackend: newFake(), on: true, due: true}
	close(f.release)
	s, sock := startServer(t, f)
	cl := dial(t, sock)
	cl.call("settings.get", nil, nil) // the connection is registered
	s.AutoUpdateOnStart(time.Hour)
	cl.waitNote("job.started", func(j Job) bool { return j.Kind == KindUpdate && j.Repo == "acme/photo" })
	f.amu.Lock()
	defer f.amu.Unlock()
	if len(f.indexed) != 1 || len(f.indexed[0]) != 1 || f.indexed[0][0] != "acme/photo" {
		t.Errorf("indexed %v, want only the installed acme/photo", f.indexed)
	}
	if !f.marked {
		t.Error("the check was not recorded")
	}

	// Not due: nothing runs.
	f2 := &autoFake{fakeBackend: newFake(), on: true}
	s2, _ := startServer(t, f2)
	s2.AutoUpdateOnStart(time.Hour)
	if js := s2.jobs.list(); len(js) != 0 {
		t.Errorf("jobs %+v, want none when the check is recent", js)
	}
}

// After Shutdown no job starts, even from a background goroutine (automatic
// updates), so nothing races with the final wait.
func TestNoJobAfterShutdown(t *testing.T) {
	s, _ := startServer(t, newFake())
	s.Shutdown()
	if _, err := s.startInstallJob(KindUpdate, "acme/photo", false); err == nil {
		t.Error("a job started after Shutdown")
	}
}

// A partial check keeps its result, reports failure and remains due on the
// next startup instead of recording a complete 12-hour refresh.
func TestPartialAutoCheckDoesNotMarkOrUpdate(t *testing.T) {
	f := &autoFake{fakeBackend: newFake(), on: true, due: true, indexStats: &index.Stats{Updated: 1, Failed: 2}}
	close(f.release)
	s, sock := startServer(t, f)
	cl := dial(t, sock)
	cl.call("settings.get", nil, nil)
	for range 2 {
		s.AutoUpdateOnStart(time.Hour)
		failed := cl.waitNote("job.failed", func(j Job) bool { return j.Kind == KindIndex })
		if failed.State != StateFailed || failed.Error == nil {
			t.Fatalf("job: %+v", failed)
		}
		f.amu.Lock()
		marked, lists := f.marked, f.autoLists
		f.amu.Unlock()
		if marked || lists != 0 {
			t.Fatalf("partial check: marked=%v automatic lists=%d", marked, lists)
		}
	}
	f.amu.Lock()
	defer f.amu.Unlock()
	if len(f.indexed) != 2 {
		t.Fatalf("checks: %v", f.indexed)
	}
}

func (f *autoFake) Update(ctx context.Context, name string, opts install.Options) (*store.Install, error) {
	if f.started == nil {
		return f.fakeBackend.Update(ctx, name, opts)
	}
	f.amu.Lock()
	f.active++
	if f.active > f.peak {
		f.peak = f.active
	}
	f.amu.Unlock()
	defer func() { f.amu.Lock(); f.active--; f.amu.Unlock() }()
	select {
	case f.started <- name:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-f.finish:
		return &store.Install{FullName: name, Version: "v2"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestAutomaticUpdateConcurrencyAndDisable(t *testing.T) {
	for _, disable := range []bool{false, true} {
		t.Run(fmt.Sprint(disable), func(t *testing.T) {
			f := &autoFake{fakeBackend: newFake(), on: true, started: make(chan string, 10), finish: make(chan struct{})}
			for i := range 6 {
				f.ready = append(f.ready, store.ListItem{App: store.App{FullName: fmt.Sprintf("acme/app%d", i)}})
			}
			s := NewServer(f, nil)
			t.Cleanup(s.Shutdown)
			done := make(chan struct{})
			go func() { s.startAutoUpdates(); close(done) }()
			next := func() string {
				t.Helper()
				select {
				case name := <-f.started:
					return name
				case <-time.After(3 * time.Second):
					t.Fatal("no update started")
					return ""
				}
			}
			next()
			next()
			// Another trigger must not start another pool while this batch is busy.
			s.startAutoUpdates()
			select {
			case name := <-f.started:
				t.Fatalf("too many simultaneous updates: %s", name)
			case <-time.After(30 * time.Millisecond):
			}
			if disable {
				if err := f.SetAutoUpdate(context.Background(), false); err != nil {
					t.Fatal(err)
				}
			} else {
				f.finish <- struct{}{}
				next() // a queued app starts after one finishes
			}
			close(f.finish)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("batch did not finish")
			}
			f.amu.Lock()
			peak := f.peak
			f.amu.Unlock()
			if peak != autoUpdateWorkers {
				t.Fatalf("peak=%d", peak)
			}
			if disable && len(f.started) != 0 {
				t.Fatalf("started pending apps after disabling: %d", len(f.started))
			}
		})
	}
}

func TestAutomaticBatchAllowsManualUpdateAndShutsDown(t *testing.T) {
	f := &autoFake{fakeBackend: newFake(), on: true, started: make(chan string, 10), finish: make(chan struct{})}
	for i := range 5 {
		f.ready = append(f.ready, store.ListItem{App: store.App{FullName: fmt.Sprintf("acme/app%d", i)}})
	}
	s := NewServer(f, nil)
	t.Cleanup(s.Shutdown)
	done := make(chan struct{})
	go func() { s.startAutoUpdates(); close(done) }()
	next := func() string {
		t.Helper()
		select {
		case name := <-f.started:
			return name
		case <-time.After(3 * time.Second):
			t.Fatal("update did not start")
			return ""
		}
	}
	next()
	next()
	if _, err := s.startInstallJob(KindUpdate, "acme/manual", false); err != nil {
		t.Fatal(err)
	}
	if name := next(); name != "acme/manual" {
		t.Fatalf("manual update waited behind automatic queue: %s", name)
	}
	s.Shutdown()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("automatic queue survived shutdown")
	}
	if len(f.started) != 0 {
		t.Fatalf("queued updates started during shutdown: %d", len(f.started))
	}
	f.amu.Lock()
	defer f.amu.Unlock()
	if f.active != 0 {
		t.Fatalf("active updates after shutdown: %d", f.active)
	}
}
