package index

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrBusy means another process is already indexing.
var ErrBusy = errors.New("another indexing is already running")

// lock takes an exclusive, non-blocking lock on LockPath. The kernel releases
// it if the process dies, so a crash never leaves it stuck.
func (ix *Indexer) lock() (unlock func(), err error) {
	if ix.LockPath == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(ix.LockPath), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(ix.LockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("index lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, fmt.Errorf("index lock: %w", err)
	}
	return func() { f.Close() }, nil
}
