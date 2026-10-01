package rpc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Job kinds.
const (
	KindIndex   = "index"
	KindInstall = "install"
	KindUpdate  = "update"
	KindDeps    = "deps" // system dependencies through pacman
)

// Job states.
const (
	StateRunning  = "running"
	StateDone     = "done"
	StateFailed   = "failed"
	StateCanceled = "canceled"
)

var errCanceled = errors.New("canceled")

// Job is a long operation, running or finished.
type Job struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Repo     string    `json:"repo,omitempty"`
	State    string    `json:"state"`
	Stage    string    `json:"stage,omitempty"`
	Done     int64     `json:"done"`
	Total    int64     `json:"total"`
	Message  string    `json:"message,omitempty"`
	Error    *Error    `json:"error,omitempty"`
	Result   any       `json:"result,omitempty"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`

	cancel context.CancelFunc
}

// progress is what a job function reports.
type progress struct {
	Stage   string
	Done    int64
	Total   int64
	Message string
}

// jobs manages the jobs and publishes notifications.
type jobs struct {
	mu     sync.Mutex
	seq    int
	byID   map[string]*Job
	order  []string
	notify func(method string, params any)
	wg     sync.WaitGroup
	// minInterval limits how often job.progress is sent per job.
	minInterval time.Duration
	keep        int // finished jobs kept in the history
	// lastFinished is when the last job finished (counts as activity).
	lastFinished time.Time
}

func newJobs(notify func(string, any)) *jobs {
	return &jobs{byID: map[string]*Job{}, notify: notify, minInterval: 100 * time.Millisecond, keep: 50}
}

// jobKey identifies jobs that cannot run at the same time: one index at a
// time and one operation per app.
func jobKey(kind, repo string) string {
	if kind == KindIndex || kind == KindDeps {
		return kind // pacman has a single, system-wide lock
	}
	return "app:" + strings.ToLower(repo)
}

// start runs fn in a job. Returns ErrBusy if a conflicting job is running.
// onFinish (optional) runs after the job's final notification.
func (js *jobs) start(parent context.Context, kind, repo string,
	fn func(ctx context.Context, report func(progress)) (any, error), onFinish func(Job, error)) (*Job, error) {
	js.mu.Lock()
	k := jobKey(kind, repo)
	for _, j := range js.byID {
		if j.State == StateRunning && jobKey(j.Kind, j.Repo) == k {
			js.mu.Unlock()
			return nil, fmt.Errorf("%w: job %s (%s %s)", ErrBusy, j.ID, j.Kind, j.Repo)
		}
	}
	js.seq++
	ctx, cancel := context.WithCancel(parent)
	j := &Job{ID: fmt.Sprintf("job-%d", js.seq), Kind: kind, Repo: repo, State: StateRunning,
		Started: time.Now(), cancel: cancel}
	js.byID[j.ID] = j
	js.order = append(js.order, j.ID)
	snap := *j
	js.mu.Unlock()

	js.notify("job.started", snap)
	js.wg.Add(1)
	go func() {
		defer js.wg.Done()
		defer cancel()
		var last time.Time
		lastStage := ""
		report := func(p progress) {
			js.mu.Lock()
			j.Stage, j.Done, j.Total, j.Message = p.Stage, p.Done, p.Total, p.Message
			now := time.Now()
			// Stage changes always go out; byte updates are rate limited.
			send := p.Stage != lastStage || now.Sub(last) >= js.minInterval || (p.Total > 0 && p.Done == p.Total)
			if send {
				last, lastStage = now, p.Stage
			}
			snap := *j
			js.mu.Unlock()
			if send {
				js.notify("job.progress", snap)
			}
		}
		result, err := fn(ctx, report)

		js.mu.Lock()
		j.Finished = time.Now()
		js.lastFinished = j.Finished
		switch {
		case err == nil:
			j.State, j.Result = StateDone, result
		case ctx.Err() != nil && errors.Is(err, context.Canceled):
			j.State, j.Error, j.Result = StateCanceled, toError(errCanceled), result
		default:
			j.State, j.Error, j.Result = StateFailed, toError(err), result
		}
		snap := *j
		js.prune()
		js.mu.Unlock()

		switch snap.State {
		case StateDone:
			js.notify("job.done", snap)
		default:
			js.notify("job.failed", snap)
		}
		if onFinish != nil {
			onFinish(snap, err)
		}
	}()
	return &snap, nil
}

// prune drops old finished jobs (called with the lock held).
func (js *jobs) prune() {
	finished := 0
	for _, id := range js.order {
		if js.byID[id].State != StateRunning {
			finished++
		}
	}
	if finished <= js.keep {
		return
	}
	var order []string
	for _, id := range js.order {
		if finished > js.keep && js.byID[id].State != StateRunning {
			delete(js.byID, id)
			finished--
			continue
		}
		order = append(order, id)
	}
	js.order = order
}

// running counts the running jobs and reports when the last one finished.
func (js *jobs) running() int {
	n, _ := js.activity()
	return n
}

func (js *jobs) activity() (running int, lastFinished time.Time) {
	js.mu.Lock()
	defer js.mu.Unlock()
	for _, j := range js.byID {
		if j.State == StateRunning {
			running++
		}
	}
	return running, js.lastFinished
}

// list returns copies of the jobs, oldest first.
func (js *jobs) list() []Job {
	js.mu.Lock()
	defer js.mu.Unlock()
	out := make([]Job, 0, len(js.order))
	for _, id := range js.order {
		out = append(out, *js.byID[id])
	}
	return out
}

// cancel cancels a running job.
func (js *jobs) cancel(id string) error {
	js.mu.Lock()
	defer js.mu.Unlock()
	j, ok := js.byID[id]
	if !ok {
		return &Error{Code: CodeNotFound, Message: "job not found: " + id}
	}
	if j.State == StateRunning {
		j.cancel()
	}
	return nil
}

// shutdown cancels everything and waits for the jobs to finish.
func (js *jobs) shutdown() {
	js.mu.Lock()
	for _, j := range js.byID {
		if j.State == StateRunning {
			j.cancel()
		}
	}
	js.mu.Unlock()
	js.wg.Wait()
}
