// Package notify envia notificações desktop (org.freedesktop.Notifications)
// e evita repetir o mesmo aviso.
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

// Notifier mostra uma notificação ao usuário.
type Notifier interface {
	Notify(ctx context.Context, summary, body string) error
}

// DBus envia pela interface org.freedesktop.Notifications da sessão. Não
// executa nenhum programa (nada de notify-send pelo PATH).
type DBus struct {
	AppName string
	Icon    string
}

// Notify implementa Notifier.
func (d DBus) Notify(ctx context.Context, summary, body string) error {
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("conectar ao D-Bus da sessão: %w", err)
	}
	defer conn.Close()
	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := obj.CallWithContext(ctx, "org.freedesktop.Notifications.Notify", 0,
		d.AppName, uint32(0), d.Icon, summary, body, []string{}, map[string]dbus.Variant{}, int32(-1))
	if call.Err != nil {
		return fmt.Errorf("enviar notificação: %w", call.Err)
	}
	return nil
}

// Once envia a notificação só se key for diferente da última enviada,
// guardada em stateFile. Retorna se enviou.
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

// Update é uma atualização disponível.
type Update struct {
	Repo, Name, From, To string
}

// UpdatesMessage monta a notificação e a chave de deduplicação (o conjunto
// de repo@versão, em ordem estável).
func UpdatesMessage(updates []Update) (key, summary, body string) {
	sorted := append([]Update(nil), updates...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Repo < sorted[j].Repo })
	var keys, lines []string
	for _, u := range sorted {
		keys = append(keys, u.Repo+"@"+u.To)
		lines = append(lines, fmt.Sprintf("%s: %s → %s", u.Name, u.From, u.To))
	}
	if len(sorted) == 1 {
		summary = "OmaStore: 1 atualização disponível"
	} else {
		summary = fmt.Sprintf("OmaStore: %d atualizações disponíveis", len(sorted))
	}
	const maxLines = 5
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], fmt.Sprintf("… e mais %d", len(sorted)-maxLines))
	}
	return strings.Join(keys, "\n"), summary, strings.Join(lines, "\n")
}
