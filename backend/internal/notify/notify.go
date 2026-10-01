// Package notify sends desktop notifications (org.freedesktop.Notifications)
// and avoids repeating the same notice.
package notify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	Notify(ctx context.Context, msg Message) error
}

// Message is a notification. Glyph and Exec travel as the Omarchy shell's
// hints: it shows the glyph (a Nerd Font character) and, on click, runs Exec as
// an argv (never a shell string). Other notification servers ignore unknown
// hints, so the message degrades to a plain one outside Omarchy.
type Message struct {
	Summary, Body string
	Glyph         string
	Exec          []string
}

// Hint names understood by the Omarchy shell (omarchy-notification-send). They
// are not a stable public API: keep them here only.
const (
	hintGlyph    = "omarchy-glyph"
	hintExecArgv = "omarchy-exec-argv"
)

// hints builds the a{sv} hints of msg.
func (msg Message) hints() map[string]dbus.Variant {
	h := map[string]dbus.Variant{}
	if msg.Glyph != "" {
		h[hintGlyph] = dbus.MakeVariant(msg.Glyph)
	}
	// The shell refuses an empty program or one starting with '-'.
	if len(msg.Exec) > 0 && msg.Exec[0] != "" && !strings.HasPrefix(msg.Exec[0], "-") {
		if b, err := json.Marshal(msg.Exec); err == nil {
			h[hintExecArgv] = dbus.MakeVariant(string(b))
		}
	}
	return h
}

// DBus sends through the session's org.freedesktop.Notifications interface.
// It runs no program (no notify-send through PATH).
type DBus struct {
	AppName string
	Icon    string
}

// Notify implements Notifier.
func (d DBus) Notify(ctx context.Context, msg Message) error {
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("connect to the session D-Bus: %w", err)
	}
	defer conn.Close()
	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := obj.CallWithContext(ctx, "org.freedesktop.Notifications.Notify", 0,
		d.AppName, uint32(0), d.Icon, msg.Summary, msg.Body, []string{}, msg.hints(), int32(-1))
	if call.Err != nil {
		return fmt.Errorf("send notification: %w", call.Err)
	}
	return nil
}

// Once sends the notification only if key differs from the last one sent,
// stored in stateFile. Returns whether it sent.
func Once(ctx context.Context, n Notifier, stateFile, key string, msg Message) (bool, error) {
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:])
	if b, err := os.ReadFile(stateFile); err == nil && strings.TrimSpace(string(b)) == h {
		return false, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := n.Notify(ctx, msg); err != nil {
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

// updatesGlyph is nf-md-package_variant, the glyph of the update notification.
const updatesGlyph = "\U000F03D6"

// UpdatesMessage builds the notification and the deduplication key (the set
// of repo@version, in a stable order). With gui (the absolute path of
// omastore-gui), a click opens the app, or the Installed page when there are
// several updates.
func UpdatesMessage(updates []Update, gui string) (key string, msg Message) {
	sorted := append([]Update(nil), updates...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Repo < sorted[j].Repo })
	var keys, lines []string
	for _, u := range sorted {
		keys = append(keys, u.Repo+"@"+u.To)
		lines = append(lines, fmt.Sprintf("%s: %s → %s", u.Name, u.From, u.To))
	}
	msg.Glyph = updatesGlyph
	if len(sorted) == 1 {
		msg.Summary = "OmaStore: 1 update available"
		if gui != "" {
			msg.Exec = []string{gui, "--open", sorted[0].Repo}
		}
	} else {
		msg.Summary = fmt.Sprintf("OmaStore: %d updates available", len(sorted))
		if gui != "" {
			msg.Exec = []string{gui, "--page", "installed"}
		}
	}
	const maxLines = 5
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], fmt.Sprintf("… and %d more", len(sorted)-maxLines))
	}
	msg.Body = strings.Join(lines, "\n")
	return strings.Join(keys, "\n"), msg
}
