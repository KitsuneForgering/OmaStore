package rpc

import (
	"context"
	"testing"
)

type skillBackend struct {
	Backend
	calls int
}

func (b *skillBackend) InstallSkills(context.Context) (int, error) { b.calls++; return 3, nil }

func TestSkillsInstallDispatch(t *testing.T) {
	b := &skillBackend{}
	s := &Server{backend: b, ctx: context.Background()}
	result, err := s.call("skills.install", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.calls != 1 || result.(map[string]any)["installed"] != 3 {
		t.Fatalf("result = %v, calls = %d", result, b.calls)
	}
}
