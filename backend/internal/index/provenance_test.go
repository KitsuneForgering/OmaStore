package index

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/KitsuneForgering/OmaStore/backend/internal/asset"
	"github.com/KitsuneForgering/OmaStore/backend/internal/provenance"
)

// attestGH adds attestations to fakeGH: digest → bundles.
type attestGH struct {
	*fakeGH
	bundles map[string][][]byte
	asked   []string
	err     error
}

func (f *attestGH) Attestations(ctx context.Context, repo, digest string) ([][]byte, error) {
	f.mu.Lock()
	f.asked = append(f.asked, digest)
	f.mu.Unlock()
	return f.bundles[digest], f.err
}

// fakeVerifier accepts the bundle "good" and refuses the rest.
type fakeVerifier struct{ err error }

func (v fakeVerifier) Verify(b []byte, repo, hex string) (provenance.Result, error) {
	if v.err != nil {
		return provenance.Result{}, v.err
	}
	if string(b) != "good" {
		return provenance.Result{}, provenance.ErrNotVerified
	}
	return provenance.Result{Workflow: ".github/workflows/release.yml", Ref: "refs/tags/v1", Commit: "c0ffee"}, nil
}

func TestIndexRecordsProvenance(t *testing.T) {
	ix, gh, st := setup(t)
	ctx := context.Background()
	agh := &attestGH{fakeGH: gh, bundles: map[string][][]byte{"sha256:aa": {[]byte("forged"), []byte("good")}}}
	ix.GH = agh
	ix.Provenance = fakeVerifier{}
	if _, err := ix.Run(ctx, Options{Only: []string{"acme/omaphoto"}}); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetApp(ctx, "acme/omaphoto")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range d.Assets {
		switch a.Arch {
		case asset.ArchAMD64:
			if !strings.Contains(a.Provenance, `"workflow":".github/workflows/release.yml"`) {
				t.Errorf("amd64 provenance = %q", a.Provenance)
			}
		default:
			// No digest: nothing to ask GitHub about.
			if a.Provenance != "" {
				t.Errorf("%s provenance = %q", a.Arch, a.Provenance)
			}
		}
	}
	if len(agh.asked) != 1 {
		t.Errorf("asked for %v, want only the amd64 file's digest", agh.asked)
	}

	// A trust root that cannot load fails the run, to be retried.
	ix.Provenance = fakeVerifier{err: errors.New("no trust root")}
	if stats, err := ix.Run(ctx, Options{Only: []string{"acme/omaphoto"}, Force: true}); err == nil && stats.Failed == 0 {
		t.Errorf("a verifier that cannot run should fail the repository: %+v", stats)
	}
}
