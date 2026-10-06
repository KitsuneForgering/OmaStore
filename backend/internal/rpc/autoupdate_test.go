package rpc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/index"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

// autoFake adds automatic updates to fakeBackend.
type autoFake struct {
	*fakeBackend
	amu     sync.Mutex
	on      bool
	due     bool
	marked  bool
	indexed [][]string
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
	return []store.ListItem{{App: store.App{FullName: "acme/photo", Name: "Photo"}, LatestTag: "v2", InstalledVersion: "v1"}},
		[]store.ListItem{{App: store.App{FullName: "acme/raw", Name: "Raw"}, LatestTag: "v3", InstalledVersion: "v2"}}, nil
}

func (f *autoFake) AutoCheckDue(context.Context, time.Duration) (bool, error) {
	f.amu.Lock()
	defer f.amu.Unlock()
	return f.on && f.due, nil
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
	return f.fakeBackend.Index(ctx, opts)
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
