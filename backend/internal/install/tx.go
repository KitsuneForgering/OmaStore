package install

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
)

// tx registra o que uma instalação criou ou substituiu, para desfazer tudo
// se alguma etapa falhar no meio.
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

// prepare deve ser chamado antes de criar path. Se já existir algo lá, é
// movido de lado (mesmo diretório, rename atômico) para poder ser restaurado.
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

// rollback remove o que foi criado e restaura o que foi substituído.
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

// commit descarta as cópias de segurança.
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
