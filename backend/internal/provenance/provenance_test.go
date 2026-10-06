package provenance

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/root"
)

// testdata holds a real attestation (OmaStore v0.3.5's x86_64 tarball, from
// GET /repos/KitsuneForgering/OmaStore/attestations/sha256:...) and the
// public-good trust root at that time (gh attestation trusted-root), so the
// test runs offline.
const omastoreDigest = "2bc89b8ede0a0597fd8fbcf7512824f41e03099642ece527ce0043775e2bd499"

func verifier(t *testing.T) *Verifier {
	t.Helper()
	tr, err := root.NewTrustedRootFromPath("testdata/trusted_root.json")
	if err != nil {
		t.Fatal(err)
	}
	v, err := New(tr)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestVerify(t *testing.T) {
	v := verifier(t)
	b, err := os.ReadFile("testdata/omastore-bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := v.Verify(b, "KitsuneForgering/OmaStore", omastoreDigest)
	if err != nil {
		t.Fatal(err)
	}
	if res.Workflow != ".github/workflows/release.yml" || res.Ref != "refs/tags/v0.3.5" || len(res.Commit) != 40 {
		t.Errorf("result = %+v", res)
	}
	// GitHub names are case-insensitive.
	if _, err := v.Verify(b, "kitsuneforgering/omastore", omastoreDigest); err != nil {
		t.Errorf("lower case: %v", err)
	}

	for name, c := range map[string]struct {
		bundle       []byte
		repo, digest string
	}{
		"another repository":   {b, "someone/else", omastoreDigest},
		"a prefix of the repo": {b, "KitsuneForgering/Oma", omastoreDigest},
		"another file":         {b, "KitsuneForgering/OmaStore", strings.Repeat("ab", 32)},
		"bad digest":           {b, "KitsuneForgering/OmaStore", "xyz"},
		"tampered bundle":      {[]byte(strings.Replace(string(b), `"payload":"ey`, `"payload":"ez`, 1)), "KitsuneForgering/OmaStore", omastoreDigest},
		"not a bundle":         {[]byte(`{}`), "KitsuneForgering/OmaStore", omastoreDigest},
	} {
		if _, err := v.Verify(c.bundle, c.repo, c.digest); !errors.Is(err, ErrNotVerified) {
			t.Errorf("%s: err = %v, want ErrNotVerified", name, err)
		}
	}
}
