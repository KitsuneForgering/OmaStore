package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/install"
)

// stampBackend is a backend whose database another process can change.
type stampBackend struct {
	*fakeBackend
	stamp atomic.Int32
}

func (b *stampBackend) ChangeStamp(ctx context.Context) (string, error) {
	return fmt.Sprint(b.stamp.Load()), nil
}

// A change made outside the daemon (e.g. the index timer) reaches the
// connected clients as catalog.changed.
func TestWatchChangesNotifiesExternalChanges(t *testing.T) {
	b := &stampBackend{fakeBackend: newFake()}
	s, sock := startServer(t, b)
	cl := dial(t, sock)
	cl.call("daemon.hello", nil, nil) // the connection is registered
	go s.WatchChanges(10 * time.Millisecond)

	// No change: no notification.
	select {
	case n := <-cl.notes:
		t.Fatalf("unexpected notification %s", n["method"])
	case <-time.After(50 * time.Millisecond):
	}

	b.stamp.Add(1)
	select {
	case n := <-cl.notes:
		var m string
		json.Unmarshal(n["method"], &m)
		if m != "catalog.changed" {
			t.Errorf("method = %s", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no catalog.changed after an external change")
	}
}

func TestNewErrorCodes(t *testing.T) {
	if c := toError(fmt.Errorf("x: %w", index.ErrBusy)).Code; c != CodeBusy {
		t.Errorf("index busy → %d", c)
	}
	if c := toError(fmt.Errorf("x: %w", install.ErrIncomplete)).Code; c != CodeIncomplete {
		t.Errorf("incomplete → %d", c)
	}
}
