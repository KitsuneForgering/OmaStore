package install

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ErrInUse means a process is running from the app's files: removing them
// would pull them out from under it (an interpreter loading modules later,
// an Electron app reading its resources). Close the app, or force.
var ErrInUse = errors.New("the app is running")

// procRoot is /proc; tests point it at a fake tree.
var procRoot = "/proc"

// Process is a process found using an app's files.
type Process struct {
	PID  int
	Name string // comm, for messages
}

// procsUsing lists the processes whose executable, working directory or a
// mapped file (shared libraries, an AppImage's payload) is inside dir. It only
// reads /proc: nothing is run or signaled. Processes of other users are
// unreadable and skipped, which is fine: apps live in this user's $HOME.
func procsUsing(dir string) ([]Process, error) {
	dir = filepath.Clean(dir)
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	self := os.Getpid()
	var out []Process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		base := filepath.Join(procRoot, e.Name())
		if usesDir(base, dir) {
			name, _ := os.ReadFile(filepath.Join(base, "comm"))
			out = append(out, Process{PID: pid, Name: strings.TrimSpace(string(name))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out, nil
}

func usesDir(base, dir string) bool {
	for _, link := range []string{"exe", "cwd"} {
		if target, err := os.Readlink(filepath.Join(base, link)); err == nil && insideDeleted(dir, target) {
			return true
		}
	}
	f, err := os.Open(filepath.Join(base, "maps"))
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		// address perms offset dev inode path
		fields := strings.SplitN(sc.Text(), " ", 6)
		if len(fields) == 6 && insideDeleted(dir, strings.TrimSpace(fields[5])) {
			return true
		}
	}
	return false
}

// insideDeleted is within, also for the " (deleted)" suffix the kernel adds
// to a removed file: a process still running a deleted version uses it too.
func insideDeleted(dir, p string) bool {
	p = strings.TrimSuffix(p, " (deleted)")
	return filepath.IsAbs(p) && within(dir, p)
}

// describe is "name (pid), …" for messages.
func describe(ps []Process) string {
	parts := make([]string, 0, len(ps))
	for _, p := range ps {
		name := p.Name
		if name == "" {
			name = "process"
		}
		parts = append(parts, fmt.Sprintf("%s (%d)", name, p.PID))
	}
	return strings.Join(parts, ", ")
}
