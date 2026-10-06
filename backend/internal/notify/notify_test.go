package notify

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fake struct {
	sent []string
	err  error
}

func (f *fake) Notify(ctx context.Context, msg Message) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, msg.Summary+"|"+msg.Body)
	return nil
}

func TestOnceDeduplicates(t *testing.T) {
	f := &fake{}
	state := filepath.Join(t.TempDir(), "sub", "notified")
	ctx := context.Background()
	for i, want := range []bool{true, false, false} {
		sent, err := Once(ctx, f, state, "a@v2", Message{Summary: "s", Body: "b"})
		if err != nil || sent != want {
			t.Fatalf("envio %d: sent=%v err=%v", i, sent, err)
		}
	}
	if sent, _ := Once(ctx, f, state, "a@v3", Message{Summary: "s", Body: "b"}); !sent {
		t.Error("change not notified")
	}
	if len(f.sent) != 2 {
		t.Errorf("enviados = %v", f.sent)
	}
	// A failed notification does not write the state: it tries again next time.
	f.err = errors.New("no notification server")
	if _, err := Once(ctx, f, state, "a@v4", Message{Summary: "s", Body: "b"}); err == nil {
		t.Error("expected an error")
	}
	f.err = nil
	if sent, _ := Once(ctx, f, state, "a@v4", Message{Summary: "s", Body: "b"}); !sent {
		t.Error("should resend after a failure")
	}
}

func TestUpdatesMessage(t *testing.T) {
	ups := []Update{
		{Repo: "b/two", Name: "Two", From: "v1", To: "v2"},
		{Repo: "a/one", Name: "One", From: "1.0", To: "1.1"},
	}
	key, msg := UpdatesMessage(ups, "")
	key2, _ := UpdatesMessage([]Update{ups[1], ups[0]}, "")
	summary, body := msg.Summary, msg.Body
	if key != key2 || key != "a/one@1.1\nb/two@v2" {
		t.Errorf("unstable key: %q %q", key, key2)
	}
	if summary != "OmaStore: 2 updates available" || !strings.HasPrefix(body, "One: 1.0 → 1.1") {
		t.Errorf("%q / %q", summary, body)
	}
	var many []Update
	for i := 0; i < 8; i++ {
		many = append(many, Update{Repo: string(rune('a'+i)) + "/x", Name: "x", From: "1", To: "2"})
	}
	_, m1 := UpdatesMessage(many[:1], "")
	_, m8 := UpdatesMessage(many, "")
	s1, b, b8 := m1.Summary, m1.Body, m8.Body
	if s1 != "OmaStore: 1 update available" || strings.Count(b8, "\n") != 5 || !strings.Contains(b8, "3 more") || b == "" {
		t.Errorf("s1=%q b8=%q", s1, b8)
	}
}

func TestUpdatesMessageClick(t *testing.T) {
	one := []Update{{Repo: "acme/app", Name: "App", From: "v1", To: "v2"}}
	two := append(one, Update{Repo: "acme/other", Name: "Other", From: "v1", To: "v2"})
	if _, m := UpdatesMessage(one, ""); m.Exec != nil || m.Glyph == "" {
		t.Errorf("without the GUI: %+v", m)
	}
	if _, m := UpdatesMessage(one, "/usr/bin/omastore-gui"); !slices.Equal(m.Exec, []string{"/usr/bin/omastore-gui", "--open", "acme/app"}) {
		t.Errorf("one update: %q", m.Exec)
	}
	if _, m := UpdatesMessage(two, "/usr/bin/omastore-gui"); !slices.Equal(m.Exec, []string{"/usr/bin/omastore-gui", "--page", "installed"}) {
		t.Errorf("several updates: %q", m.Exec)
	}
}

func TestHints(t *testing.T) {
	if h := (Message{Summary: "s"}).hints(); len(h) != 0 {
		t.Errorf("plain message has hints: %v", h)
	}
	h := Message{Glyph: "x", Exec: []string{"/bin/gui", "--open", `a"b`}}.hints()
	if h[hintGlyph].Value() != "x" || h[hintExecArgv].Value() != `["/bin/gui","--open","a\"b"]` {
		t.Errorf("hints: %v", h)
	}
	// The shell would refuse these; do not send them.
	for _, argv := range [][]string{{""}, {"-rf"}} {
		if _, ok := (Message{Exec: argv}).hints()[hintExecArgv]; ok {
			t.Errorf("argv %q sent", argv)
		}
	}
}

func TestUpdatedMessage(t *testing.T) {
	one := UpdatedMessage([]Update{{Repo: "a/x", Name: "X", From: "v1", To: "v2"}}, "/gui")
	if one.Summary != "OmaStore updated X" || one.Body != "X: v1 → v2" || strings.Join(one.Exec, " ") != "/gui --open a/x" {
		t.Errorf("one = %+v", one)
	}
	two := UpdatedMessage([]Update{{Repo: "b/y", Name: "Y", From: "1", To: "2"}, {Repo: "a/x", Name: "X", From: "v1", To: "v2"}}, "")
	if two.Summary != "OmaStore updated 2 apps" || two.Body != "X: v1 → v2\nY: 1 → 2" || two.Exec != nil {
		t.Errorf("two = %+v", two)
	}
}
