// Package provenance verifies GitHub artifact attestations: SLSA build
// provenance signed through Sigstore by a GitHub Actions workflow. A verified
// attestation says which workflow of which repository built a release file,
// which a checksum alone cannot.
package provenance

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// GitHub Actions' OIDC issuer: the identity Fulcio certified.
const actionsIssuer = "https://token.actions.githubusercontent.com"

// SLSA provenance predicates GitHub's attest-build-provenance produces.
var slsaPredicates = map[string]bool{
	"https://slsa.dev/provenance/v1":   true,
	"https://slsa.dev/provenance/v0.2": true,
}

// ErrNotVerified means no attestation proved the claim.
var ErrNotVerified = errors.New("no verifiable build provenance")

// Result is what a verified attestation proves.
type Result struct {
	Workflow string `json:"workflow"` // path in the repository, e.g. .github/workflows/release.yml
	Ref      string `json:"ref"`      // e.g. refs/tags/v1.2.3
	Commit   string `json:"commit"`   // source commit the workflow ran on
}

// Verifier checks attestation bundles against Sigstore's public-good trust
// root (the one GitHub uses for public repositories).
type Verifier struct {
	sev *verify.Verifier
}

// New builds a Verifier on trusted material.
func New(tm root.TrustedMaterial) (*Verifier, error) {
	sev, err := verify.NewVerifier(tm, verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1))
	if err != nil {
		return nil, fmt.Errorf("sigstore verifier: %w", err)
	}
	return &Verifier{sev: sev}, nil
}

// Live returns a Verifier whose trust root comes from Sigstore's TUF
// repository, cached in cacheDir and refreshed as TUF requires.
func Live(cacheDir string) (*Verifier, error) {
	opts := tuf.DefaultOptions().WithCachePath(cacheDir)
	tm, err := root.NewLiveTrustedRoot(opts)
	if err != nil {
		return nil, fmt.Errorf("sigstore trust root: %w", err)
	}
	return New(tm)
}

// Verify checks that bundleJSON attests the file with this sha256 (hex) as
// built by a GitHub Actions workflow of repo itself (owner/repo; a reusable
// workflow from another repository does not count).
func (v *Verifier) Verify(bundleJSON []byte, repo, sha256Hex string) (Result, error) {
	digest, err := hex.DecodeString(sha256Hex)
	if err != nil || len(digest) != 32 {
		return Result{}, fmt.Errorf("%w: bad sha256 %q", ErrNotVerified, sha256Hex)
	}
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotVerified, err)
	}
	repoURL := "https://github.com/" + repo
	id, err := verify.NewShortCertificateIdentity(actionsIssuer, "", "",
		"(?i)^"+regexp.QuoteMeta(repoURL)+"/\\.github/workflows/[^@]+@")
	if err != nil {
		return Result{}, err
	}
	res, err := v.sev.Verify(&b, verify.NewPolicy(verify.WithArtifactDigest("sha256", digest),
		verify.WithCertificateIdentity(id)))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotVerified, err)
	}
	if res.Statement == nil || !slsaPredicates[res.Statement.GetPredicateType()] {
		return Result{}, fmt.Errorf("%w: not a SLSA provenance statement", ErrNotVerified)
	}
	if res.Signature == nil || res.Signature.Certificate == nil {
		return Result{}, fmt.Errorf("%w: no signing certificate", ErrNotVerified)
	}
	cert := res.Signature.Certificate
	// The source must be the repository too (the workflow identity above
	// already is, but a fork's run would differ here).
	if !strings.EqualFold(cert.SourceRepositoryURI, repoURL) {
		return Result{}, fmt.Errorf("%w: built from %s", ErrNotVerified, cert.SourceRepositoryURI)
	}
	path, ref, _ := strings.Cut(strings.TrimPrefix(cert.SubjectAlternativeName, cert.SourceRepositoryURI+"/"), "@")
	return Result{Workflow: path, Ref: ref, Commit: cert.SourceRepositoryDigest}, nil
}

// Lazy builds a Live verifier on first use, so a run without attested files
// never fetches the trust root. A failed build is retried on the next call.
type Lazy struct {
	CacheDir string

	mu sync.Mutex
	v  *Verifier
}

// Verify implements the same check as Verifier.Verify.
func (l *Lazy) Verify(bundleJSON []byte, repo, sha256Hex string) (Result, error) {
	l.mu.Lock()
	if l.v == nil {
		v, err := Live(l.CacheDir)
		if err != nil {
			l.mu.Unlock()
			return Result{}, err
		}
		l.v = v
	}
	v := l.v
	l.mu.Unlock()
	return v.Verify(bundleJSON, repo, sha256Hex)
}
