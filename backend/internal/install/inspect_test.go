package install

import (
	"context"
	"errors"
	"testing"

	"github.com/KitsuneForgering/OmaStore/backend/internal/asset"
	"github.com/KitsuneForgering/OmaStore/backend/internal/manifest"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

func TestInspectReleaseWithoutInstalling(t *testing.T) {
	e := newEnv(t)
	data := tarGz(t, []entry{
		{name: "bin/omaphoto", body: "#!/bin/sh\nexit 0\n", mode: 0o755},
		{name: "bin/sessiond", body: "#!/bin/sh\nexit 0\n", mode: 0o755},
	})
	e.files["/app.tar.gz"] = data
	a := store.Asset{Tag: "v1", Name: "app.tar.gz", URL: e.url + "/app.tar.gz",
		Format: asset.FormatTarGz, Arch: asset.ArchAMD64, Digest: "sha256:" + sha(data)}
	m := &manifest.Manifest{Kind: manifest.KindApp, Services: map[string]manifest.Service{
		"sessiond": {Type: "systemd-user", Unit: "omaphoto-sessiond.service", Exec: "bin/sessiond"},
	}}
	got, err := e.in.Inspect(context.Background(), a, "omaphoto", m, asset.ArchAMD64)
	if err != nil {
		t.Fatal(err)
	}
	if got.Executable != "bin/omaphoto" || !got.Verified || len(got.Services) != 1 {
		t.Fatalf("inspection = %+v", got)
	}
	if names, _ := e.st.RepoNames(context.Background()); len(names) != 0 {
		t.Fatalf("inspection wrote to catalog: %v", names)
	}
	if _, err := e.in.Inspect(context.Background(), a, "omaphoto", m, asset.ArchAMD64); err != nil {
		t.Fatal(err)
	}
	if got := e.hits.Load(); got != 2 {
		t.Fatalf("expected only two downloads, got %d", got)
	}
}

func TestInspectRejectsBadChecksumAndMissingService(t *testing.T) {
	e := newEnv(t)
	data := tarGz(t, []entry{{name: "bin/omaphoto", body: "#!/bin/sh\n", mode: 0o755}})
	e.files["/app.tar.gz"] = data
	a := store.Asset{Tag: "v1", Name: "app.tar.gz", URL: e.url + "/app.tar.gz",
		Format: asset.FormatTarGz, Arch: asset.ArchAMD64, Digest: "sha256:" + sha([]byte("wrong"))}
	_, err := e.in.Inspect(context.Background(), a, "omaphoto", &manifest.Manifest{}, asset.ArchAMD64)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("want checksum failure, got %v", err)
	}
	a.Digest = "sha256:" + sha(data)
	m := &manifest.Manifest{Services: map[string]manifest.Service{"daemon": {Exec: "bin/missing"}}}
	_, err = e.in.Inspect(context.Background(), a, "omaphoto", m, asset.ArchAMD64)
	var ie *InspectionError
	if !errors.As(err, &ie) || ie.Stage != "service" {
		t.Fatalf("want missing service failure, got %v", err)
	}
}
