package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Setting keys.
const (
	// SettingAutoUpdate is "on" or "off"; missing means on.
	SettingAutoUpdate = "auto_update"
	// SettingAutoCheckAt is when the installed apps were last refreshed for
	// automatic updates (RFC 3339).
	SettingAutoCheckAt = "auto_check_at"
	// SettingRequireProvenance is "on" or "off" (missing means off): install
	// and update only files with verified build provenance.
	SettingRequireProvenance = "require_provenance"
)

// Setting returns the value of key, or def when it was never set.
func (s *Store) Setting(ctx context.Context, key, def string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	if err != nil {
		return "", fmt.Errorf("read setting %s: %w", key, err)
	}
	return v, nil
}

// SetSetting stores the value of key.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}
	return nil
}
