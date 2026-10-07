package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SetBlocklist replaces the blocked apps (owner/repo → reason).
func (s *Store) SetBlocklist(ctx context.Context, blocked map[string]string) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, `DELETE FROM blocked`); err != nil {
		return fmt.Errorf("clear blocklist: %w", err)
	}
	for name, reason := range blocked {
		if _, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO blocked (full_name, reason) VALUES (?, ?)`, name, reason); err != nil {
			return fmt.Errorf("block %s: %w", name, err)
		}
	}
	return tx.Commit()
}

// Blocked reports whether an app is blocked, and why.
func (s *Store) Blocked(ctx context.Context, fullName string) (reason string, blocked bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT reason FROM blocked WHERE full_name = ?`, fullName).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read blocklist: %w", err)
	}
	return reason, true, nil
}
