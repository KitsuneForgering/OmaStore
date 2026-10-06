package store

import (
	"context"
	"testing"
)

func TestSettings(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if v, err := s.Setting(ctx, SettingAutoUpdate, "on"); err != nil || v != "on" {
		t.Fatalf("default = %q, %v", v, err)
	}
	for _, want := range []string{"off", "on"} {
		if err := s.SetSetting(ctx, SettingAutoUpdate, want); err != nil {
			t.Fatal(err)
		}
		if v, err := s.Setting(ctx, SettingAutoUpdate, "x"); err != nil || v != want {
			t.Fatalf("got %q, %v; want %q", v, err, want)
		}
	}
}
