// Package notify sends desktop notifications (org.freedesktop.Notifications)
// and avoids repeating the same notice.
package notify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/godbus/dbus/v5"
)

// Notifier shows a notification to the user.
type Notifier interface {
	Notify(ctx context.Context, summary, body string) error
}

// DBus sends through the session's org.freedesktop.Notifications interface.
// It runs no program (no notify-send through PATH).
type DBus struct {
	AppName string
	Icon    string
}

// Notify implements Notifier.
func (d DBus) Notify(ctx context.Context, summary, body string) error {
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("connect to the session D-Bus: %w", err)
	}
	defer conn.Close()
	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := obj.CallWithContext(ctx, "org.freedesktop.Notifications.Notify", 0,
		d.AppName, uint32(0), d.Icon, summary, body, []string{}, map[string]dbus.Variant{}, int32(-1))
	if call.Err != nil {
		return fmt.Errorf("send notification: %w", call.Err)
	}
	return nil
}

// Once sends the notification only if key differs from the last one sent,
// stored in stateFile. Returns whether it sent.
func Once(ctx context.Context, n Notifier, stateFile, key, summary, body string) (bool, error) {
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:])
	if b, err := os.ReadFile(stateFile); err == nil && strings.TrimSpace(string(b)) == h {
		return false, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := n.Notify(ctx, summary, body); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o755); err != nil {
		return true, err
	}
	return true, os.WriteFile(stateFile, []byte(h+"\n"), 0o644)
}

// Update is an available update.
type Update struct {
	Repo, Name, From, To string
}

// UpdatesMessage builds the notification and the deduplication key (the set
// of repo@version, in a stable order).
func UpdatesMessage(updates []Update) (key, summary, body string) {
	sorted := append([]Update(nil), updates...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Repo < sorted[j].Repo })
	var keys, lines []string
	for _, u := range sorted {
		keys = append(keys, u.Repo+"@"+u.To)
		lines = append(lines, fmt.Sprintf("%s: %s → %s", u.Name, u.From, u.To))
	}
	if len(sorted) == 1 {
		summary = "OmaStore: 1 update available"
	} else {
		summary = fmt.Sprintf("OmaStore: %d updates available", len(sorted))
	}
	const maxLines = 5
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], fmt.Sprintf("… and %d more", len(sorted)-maxLines))
	}
	return strings.Join(keys, "\n"), summary, strings.Join(lines, "\n")
}
