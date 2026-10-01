package flock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestTryLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "x.lock")
	unlock, err := TryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	// flock locks belong to the open file, so a second open conflicts even
	// in the same process, as another process would.
	if _, err := TryLock(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock: %v", err)
	}
	unlock()
	unlock2, err := TryLock(path)
	if err != nil {
		t.Fatalf("after unlock: %v", err)
	}
	unlock2()
}
