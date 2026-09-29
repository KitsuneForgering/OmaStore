package notify

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

type fake struct {
	sent []string
	err  error
}

func (f *fake) Notify(ctx context.Context, summary, body string) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, summary+"|"+body)
	return nil
}

func TestOnceDeduplicates(t *testing.T) {
	f := &fake{}
	state := filepath.Join(t.TempDir(), "sub", "notified")
	ctx := context.Background()
	for i, want := range []bool{true, false, false} {
		sent, err := Once(ctx, f, state, "a@v2", "s", "b")
		if err != nil || sent != want {
			t.Fatalf("envio %d: sent=%v err=%v", i, sent, err)
		}
	}
	if sent, _ := Once(ctx, f, state, "a@v3", "s", "b"); !sent {
		t.Error("mudança não notificada")
	}
	if len(f.sent) != 2 {
		t.Errorf("enviados = %v", f.sent)
	}
	// Falha ao notificar não grava o estado: tenta de novo na próxima vez.
	f.err = errors.New("sem servidor de notificação")
	if _, err := Once(ctx, f, state, "a@v4", "s", "b"); err == nil {
		t.Error("esperava erro")
	}
	f.err = nil
	if sent, _ := Once(ctx, f, state, "a@v4", "s", "b"); !sent {
		t.Error("deveria reenviar após falha")
	}
}

func TestUpdatesMessage(t *testing.T) {
	ups := []Update{
		{Repo: "b/two", Name: "Two", From: "v1", To: "v2"},
		{Repo: "a/one", Name: "One", From: "1.0", To: "1.1"},
	}
	key, summary, body := UpdatesMessage(ups)
	key2, _, _ := UpdatesMessage([]Update{ups[1], ups[0]})
	if key != key2 || key != "a/one@1.1\nb/two@v2" {
		t.Errorf("chave instável: %q %q", key, key2)
	}
	if summary != "OmaStore: 2 atualizações disponíveis" || !strings.HasPrefix(body, "One: 1.0 → 1.1") {
		t.Errorf("%q / %q", summary, body)
	}
	var many []Update
	for i := 0; i < 8; i++ {
		many = append(many, Update{Repo: string(rune('a'+i)) + "/x", Name: "x", From: "1", To: "2"})
	}
	_, s1, b := UpdatesMessage(many[:1])
	_, _, b8 := UpdatesMessage(many)
	if s1 != "OmaStore: 1 atualização disponível" || strings.Count(b8, "\n") != 5 || !strings.Contains(b8, "mais 3") || b == "" {
		t.Errorf("s1=%q b8=%q", s1, b8)
	}
}
