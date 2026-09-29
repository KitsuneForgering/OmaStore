package install

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
)

// tx records what an installation created or replaced, to undo everything
// if any step fails halfway.
type tx struct {
	created []string
	backups []backup
}

type backup struct{ path, saved string }

func randSuffix() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// prepare must be called before creating path. If something already exists
// there, it is moved aside (same directory, atomic rename) so it can be restored.
func (t *tx) prepare(path string) error {
	if _, err := os.Lstat(path); err == nil {
		saved := path + ".omastore-bak-" + randSuffix()
		if err := os.Rename(path, saved); err != nil {
			return err
		}
		t.backups = append(t.backups, backup{path, saved})
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	t.created = append(t.created, path)
	return nil
}

// rollback removes what was created and restores what was replaced.
func (t *tx) rollback() error {
	var errs []error
	for i := len(t.created) - 1; i >= 0; i-- {
		if err := os.RemoveAll(t.created[i]); err != nil {
			errs = append(errs, err)
		}
	}
	for i := len(t.backups) - 1; i >= 0; i-- {
		b := t.backups[i]
		if err := os.Rename(b.saved, b.path); err != nil {
			errs = append(errs, err)
		}
	}
	t.created, t.backups = nil, nil
	return errors.Join(errs...)
}

// commit discards the backup copies.
func (t *tx) commit() error {
	var errs []error
	for _, b := range t.backups {
		if err := os.RemoveAll(b.saved); err != nil {
			errs = append(errs, err)
		}
	}
	t.created, t.backups = nil, nil
	return errors.Join(errs...)
}
