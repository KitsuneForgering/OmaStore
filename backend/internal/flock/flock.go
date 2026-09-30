// Package flock takes exclusive, non-blocking file locks shared across
// processes (the daemon, the CLI and the index timer). The kernel releases a
// lock when its process dies, so a crash never leaves one stuck.
package flock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrLocked means another process holds the lock.
var ErrLocked = errors.New("locked by another process")

// TryLock locks path, creating it (and its directory) if needed. It returns
// ErrLocked at once if another process holds it.
func TryLock(path string) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() { f.Close() }, nil
}
