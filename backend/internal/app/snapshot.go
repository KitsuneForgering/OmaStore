package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/index"
	"github.com/KitsuneForgering/OmaStore/backend/internal/provenance"
)

// The catalog snapshot the catalog workflow publishes every day, on a
// pre-release (so it never becomes the "latest" release install.sh uses).
const (
	snapshotURL      = "https://github.com/KitsuneForgering/OmaStore/releases/download/catalog/catalog.json"
	snapshotRepo     = "KitsuneForgering/OmaStore"
	snapshotWorkflow = ".github/workflows/catalog.yml"
	maxSnapshotBytes = 32 << 20
)

// snapshotFetcher downloads the snapshot and accepts it only with a build
// attestation from OmaStore's own catalog workflow, bound to its sha256.
type snapshotFetcher struct {
	URL      string
	HTTP     *http.Client
	Lister   index.AttestationLister
	Verifier index.ProvenanceVerifier
}

func (f snapshotFetcher) Fetch(ctx context.Context) (*index.Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download catalog snapshot: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download catalog snapshot: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSnapshotBytes+1))
	if err != nil {
		return nil, fmt.Errorf("download catalog snapshot: %w", err)
	}
	if len(data) > maxSnapshotBytes {
		return nil, fmt.Errorf("catalog snapshot larger than %d bytes", maxSnapshotBytes)
	}
	sum := sha256.Sum256(data)
	hexsum := hex.EncodeToString(sum[:])
	bundles, err := f.Lister.Attestations(ctx, snapshotRepo, "sha256:"+hexsum)
	if err != nil {
		return nil, err
	}
	verified := false
	for _, b := range bundles {
		res, err := f.Verifier.Verify(b, snapshotRepo, hexsum)
		if errors.Is(err, provenance.ErrNotVerified) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if res.Workflow == snapshotWorkflow {
			verified = true
			break
		}
	}
	if !verified {
		return nil, fmt.Errorf("catalog snapshot %s: no attestation from %s", hexsum[:12], snapshotWorkflow)
	}
	var snap index.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("read catalog snapshot: %w", err)
	}
	return &snap, nil
}

var snapshotClient = &http.Client{Timeout: 2 * time.Minute}
