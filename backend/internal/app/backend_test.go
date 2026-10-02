package app

import (
	"testing"

	"github.com/KitsuneForgering/OmaStore/backend/internal/rpc"
)

// *App must satisfy rpc.Backend.
var _ rpc.Backend = (*App)(nil)

func TestVersionRoot(t *testing.T) {
	apps := "/home/u/.local/share/omastore/apps"
	for exec, want := range map[string]string{
		apps + "/acme__x/v1/bin/x":      apps + "/acme__x/v1",
		apps + "/acme__x/v1/x":          apps + "/acme__x/v1",
		apps + "/acme__x/x":             "",
		"/usr/bin/x":                    "",
		apps + "/../elsewhere/v1/bin/x": "",
	} {
		if got := versionRoot(apps, exec); got != want {
			t.Errorf("versionRoot(%s) = %q, want %q", exec, got, want)
		}
	}
}
