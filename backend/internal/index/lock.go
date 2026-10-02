package index

import (
	"errors"
	"fmt"

	"github.com/KitsuneForgering/OmaStore/backend/internal/flock"
)

// ErrBusy means another process is already indexing.
var ErrBusy = errors.New("another indexing is already running")

// lock takes an exclusive, non-blocking lock on LockPath, shared across
// processes.
func (ix *Indexer) lock() (unlock func(), err error) {
	if ix.LockPath == "" {
		return func() {}, nil
	}
	unlock, err = flock.TryLock(ix.LockPath)
	if errors.Is(err, flock.ErrLocked) {
		return nil, ErrBusy
	}
	if err != nil {
		return nil, fmt.Errorf("index lock: %w", err)
	}
	return unlock, nil
}
