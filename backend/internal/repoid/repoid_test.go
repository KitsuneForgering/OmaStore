package repoid

import (
	"errors"
	"testing"
)

func TestSplit(t *testing.T) {
	for _, bad := range []string{
		"", "a", "a/", "/b", "a/b/c", "a/../b", "../a/b", "a/..", "a/.hidden", "a/b c",
		"-a/b", "a_b/c", "a/b\n", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/b", // owner with 40 characters
	} {
		if _, _, err := Split(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, good := range []string{"a/b", "pch/rawmakase", "Some-Org/My_App.v2", "a1/b-c"} {
		if err := Validate(good); err != nil {
			t.Errorf("%q rejected: %v", good, err)
		}
	}
	o, r, err := Split("KitsuneForgering/OmaStore")
	if err != nil || o != "KitsuneForgering" || r != "OmaStore" {
		t.Errorf("split = %q %q %v", o, r, err)
	}
}

func TestEqual(t *testing.T) {
	if !Equal("PCH/Rawmakase", "pch/rawmakase") || Equal("a/b", "a/c") {
		t.Error("Equal must ignore case only")
	}
}
