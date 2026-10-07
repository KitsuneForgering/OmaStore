package index

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/repoid"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

// SnapshotFormat is the version of the snapshot's JSON layout.
const SnapshotFormat = 1

// Snapshot is a published copy of the catalog (omastore export-catalog),
// built daily by the catalog workflow so that a first run, even without a
// GitHub token, shows the whole catalog after one download.
type Snapshot struct {
	Format       int             `json:"format"`
	IndexVersion int             `json:"indexVersion"`
	Created      time.Time       `json:"created"`
	Repos        []SnapshotEntry `json:"repos"`
}

// SnapshotEntry is one repository as the indexer stored it.
type SnapshotEntry struct {
	Repo   store.Repo    `json:"repo"`
	App    store.App     `json:"app"`
	Assets []store.Asset `json:"assets"`
}

// Export copies the stored catalog. Installations are not part of it, and
// ETags are dropped: GitHub varies them with the credentials.
func (ix *Indexer) Export(ctx context.Context) (*Snapshot, error) {
	names, err := ix.Store.RepoNames(ctx)
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{Format: SnapshotFormat, IndexVersion: Version, Created: ix.now().UTC(), Repos: []SnapshotEntry{}}
	for _, name := range names {
		d, err := ix.Store.GetApp(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("export %s: %w", name, err)
		}
		r := d.Repo
		r.ETag = ""
		snap.Repos = append(snap.Repos, SnapshotEntry{Repo: r, App: d.App, Assets: d.Assets})
	}
	return snap, nil
}

// Import writes a snapshot into the catalog and returns how many
// repositories it added. Entries with an invalid name, or a release file
// outside the repository's own GitHub releases, are skipped: the store only
// downloads from the app's releases, whatever a snapshot says.
func (ix *Indexer) Import(ctx context.Context, snap *Snapshot) (int, error) {
	if snap.Format != SnapshotFormat {
		return 0, fmt.Errorf("catalog snapshot format %d, this OmaStore reads %d", snap.Format, SnapshotFormat)
	}
	n := 0
	for _, e := range snap.Repos {
		name := e.Repo.FullName
		if repoid.Validate(name) != nil || !strings.EqualFold(e.App.FullName, name) {
			ix.log().Warn("snapshot entry skipped", "repo", name, "why", "invalid name")
			continue
		}
		if bad := foreignAsset(name, e.Assets); bad != "" {
			ix.log().Warn("snapshot entry skipped", "repo", name, "why", "asset outside its releases", "url", bad)
			continue
		}
		e.App.FullName = name
		if err := ix.Store.SaveIndexed(ctx, e.Repo, e.App, e.Assets); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// foreignAsset returns the first asset or checksum URL that is not a file of
// repo's GitHub releases, or "".
func foreignAsset(repo string, assets []store.Asset) string {
	prefix := "/" + strings.ToLower(repo) + "/releases/download/"
	for _, a := range assets {
		for _, raw := range []string{a.URL, a.ChecksumURL} {
			if raw == "" {
				continue
			}
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Host != "github.com" ||
				!strings.HasPrefix(strings.ToLower(u.EscapedPath()), prefix) {
				return raw
			}
		}
	}
	return ""
}
