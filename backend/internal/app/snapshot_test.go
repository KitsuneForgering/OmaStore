package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KitsuneForgering/OmaStore/backend/internal/provenance"
)

type fakeLister map[string][][]byte

func (f fakeLister) Attestations(_ context.Context, _, digest string) ([][]byte, error) {
	return f[digest], nil
}

// fakeVerifier accepts a bundle named after the workflow that "signed" it.
type fakeVerifier struct{}

func (fakeVerifier) Verify(b []byte, repo, hexsum string) (provenance.Result, error) {
	if !strings.HasPrefix(string(b), ".github/") {
		return provenance.Result{}, provenance.ErrNotVerified
	}
	return provenance.Result{Workflow: string(b)}, nil
}

// The snapshot is used only with an attestation from the catalog workflow
// for exactly the downloaded bytes.
func TestSnapshotFetcher(t *testing.T) {
	body := `{"format":1,"indexVersion":1,"repos":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer srv.Close()
	sum := sha256.Sum256([]byte(body))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	ctx := context.Background()
	fetch := func(l fakeLister) error {
		_, err := snapshotFetcher{URL: srv.URL, HTTP: srv.Client(), Lister: l, Verifier: fakeVerifier{}}.Fetch(ctx)
		return err
	}
	if err := fetch(fakeLister{digest: {[]byte("forged"), []byte(snapshotWorkflow)}}); err != nil {
		t.Errorf("attested snapshot refused: %v", err)
	}
	for name, l := range map[string]fakeLister{
		"no attestation":       {},
		"another workflow":     {digest: {[]byte(".github/workflows/release.yml")}},
		"attests another file": {"sha256:" + strings.Repeat("ab", 32): {[]byte(snapshotWorkflow)}},
	} {
		if err := fetch(l); err == nil {
			t.Errorf("%s: snapshot accepted", name)
		}
	}
}
