// Package index discovers Omarchy app repositories on GitHub, extracts their
// display data and writes it to the store, without reprocessing what did not change.
package index

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/github"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/gitrepo"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/manifest"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
)

// Version is the version of the extraction and classification logic. Bump it
// when changing rules that affect what is stored (assets, categories, README...):
// repositories stored with a lower version are reprocessed even without changes
// on GitHub.
const Version = 6

// GitHub is the subset of the client used by the indexer.
type GitHub interface {
	SearchByTopic(ctx context.Context, topic string, max int) ([]string, error)
	GetRepo(ctx context.Context, fullName, etag string) (*github.Repo, string, bool, error)
	HeadSHA(ctx context.Context, fullName, ref, lastSHA string) (string, error)
	LatestRelease(ctx context.Context, fullName string) (*github.Release, error)
	Readme(ctx context.Context, fullName string) (string, string, error)
	Tree(ctx context.Context, fullName, sha string) ([]string, bool, error)
	File(ctx context.Context, fullName, path, ref string, maxBytes int) (string, bool, error)
}

// ManifestSearcher is implemented by clients that find repositories by their
// omastore.toml file (code search; requires a token).
type ManifestSearcher interface {
	SearchManifests(ctx context.Context, max int) ([]string, error)
}

// Batcher is implemented by clients that fetch the state of many
// repositories at once (GraphQL). If Snapshots returns
// github.ErrNoToken, the indexer uses the REST API.
type Batcher interface {
	Snapshots(ctx context.Context, names []string) (map[string]*github.Snapshot, error)
}

// Indexer runs the indexing pipeline.
type Indexer struct {
	GH     GitHub
	Store  *store.Store
	Repos  *gitrepo.Cache // optional: used when the Trees API truncates
	Topics []string       // default: ["omarchy"]
	Seeds  []string       // default: Seeds()
	// MaxSearch limits the results per topic (default 300).
	MaxSearch int
	Workers   int // default 4
	// Prune removes repositories that are no longer discovered from the catalog.
	// It is skipped when a discovery source failed.
	Prune bool
	// LockPath, if set, is a file locked during Run so that two processes
	// (daemon and CLI/timer) do not index at the same time.
	LockPath string
	// NoBatch turns off the batch query (GraphQL) and uses only the REST API.
	NoBatch bool
	Log     *slog.Logger
	Now     func() time.Time
	// GOARCH decides what counts as installable (default runtime.GOARCH).
	GOARCH string
}

// Options controls a run.
type Options struct {
	// Force reprocesses every repository, ignoring the cache.
	Force bool
	// Only restricts the run to these repositories (no discovery).
	Only []string
	// ManifestOverride uses this content as a repository's omastore.toml
	// (owner/repo → content) instead of the published file. It lets the
	// author see the result before publishing the manifest.
	ManifestOverride map[string]string
	// Progress, if set, receives the progress after each repository. The
	// calls are serialized; the callback must not block.
	Progress func(Progress)
}

// Progress is an indexing run's progress.
type Progress struct {
	Total   int
	Done    int
	Current string
	Stats
}

// Stats summarizes the result.
type Stats struct {
	Updated   int // fully reprocessed
	Refreshed int // only stars/score updated
	Unchanged int // nothing changed
	Removed   int // left the catalog
	Skipped   int // do not exist or are archived, and were not in the catalog
	// NotApps have no valid omastore.toml or do not declare kind = "app"
	// (plugins, themes), and were not in the catalog.
	NotApps int
	Failed  int
}

// Result of processing a repository.
type outcome int

const (
	outUnchanged outcome = iota
	outRefreshed
	outUpdated
	outRemoved
	outSkipped
	outNotApp
)

func (ix *Indexer) now() time.Time {
	if ix.Now != nil {
		return ix.Now()
	}
	return time.Now()
}

func (ix *Indexer) log() *slog.Logger {
	if ix.Log != nil {
		return ix.Log
	}
	return slog.Default()
}

func (ix *Indexer) goarch() string {
	if ix.GOARCH != "" {
		return ix.GOARCH
	}
	return runtime.GOARCH
}

// Discover returns the duplicate-free union of the topic searches and the seeds.
func (ix *Indexer) Discover(ctx context.Context) ([]string, error) {
	names, _, err := ix.discover(ctx)
	return names, err
}

// discover is Discover that also reports whether a source failed softly (an
// unreadable curated list or a failed manifest search): the list is then
// partial and must not be used to prune the catalog.
func (ix *Indexer) discover(ctx context.Context) (names []string, partial bool, err error) {
	topics := ix.Topics
	if len(topics) == 0 {
		topics = []string{"omarchy"}
	}
	seeds := ix.Seeds
	if seeds == nil {
		seeds = Seeds()
	}
	max := ix.MaxSearch
	if max <= 0 {
		max = 300
	}
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		k := strings.ToLower(n)
		if !seen[k] {
			seen[k] = true
			out = append(out, n)
		}
	}
	for _, s := range seeds {
		if list, ok := strings.CutPrefix(s, "list:"); ok {
			repos, err := ix.fromList(ctx, list)
			if err != nil {
				var rl *github.RateLimitError
				if errors.As(err, &rl) {
					return nil, false, err
				}
				ix.log().Warn("unreadable curated list", "list", list, "err", err)
				partial = true
			}
			for _, r := range repos {
				add(r)
			}
			continue
		}
		add(s)
	}
	// Repositories with omastore.toml at the root, even without the topic.
	if ms, ok := ix.GH.(ManifestSearcher); ok {
		names, err := ms.SearchManifests(ctx, max)
		var rl *github.RateLimitError
		switch {
		case errors.As(err, &rl):
			return nil, false, err
		case errors.Is(err, github.ErrNoToken):
		case err != nil:
			ix.log().Warn("manifest search failed", "err", err)
			partial = true
		}
		for _, n := range names {
			add(n)
		}
	}
	for _, t := range topics {
		names, err := ix.GH.SearchByTopic(ctx, t, max)
		if err != nil {
			return nil, false, err
		}
		for _, n := range names {
			add(n)
		}
	}
	return out, partial, nil
}

// maxFromList limits how many repositories a curated list can bring in.
const maxFromList = 300

var reRepoLink = regexp.MustCompile(`(?i)https?://github\.com/([A-Za-z0-9][A-Za-z0-9-]{0,38})/([A-Za-z0-9._-]{1,100})`)

// reservedOwners are github.com paths that are not users.
var reservedOwners = map[string]bool{
	"topics": true, "orgs": true, "sponsors": true, "marketplace": true, "features": true,
	"settings": true, "apps": true, "collections": true, "about": true, "login": true,
}

// fromList extracts the repositories mentioned in a curated list's README
// (e.g. an "awesome" list), in the order they appear.
func (ix *Indexer) fromList(ctx context.Context, list string) ([]string, error) {
	readme, _, err := ix.GH.Readme(ctx, list)
	if err != nil {
		return nil, err
	}
	return RepoLinks(readme, list, maxFromList), nil
}

// RepoLinks extracts github.com/owner/repo links from a text, without
// duplicates and without the list's own repository.
func RepoLinks(text, self string, max int) []string {
	seen := map[string]bool{strings.ToLower(self): true}
	var out []string
	for _, m := range reRepoLink.FindAllStringSubmatch(text, -1) {
		owner, repo := m[1], strings.TrimSuffix(m[2], ".git")
		if reservedOwners[strings.ToLower(owner)] || repo == "" || strings.HasPrefix(repo, ".") {
			continue
		}
		name := owner + "/" + repo
		if k := strings.ToLower(name); !seen[k] {
			seen[k] = true
			out = append(out, name)
			if len(out) == max {
				break
			}
		}
	}
	return out
}

// Run runs a full indexing. Without opts.Only, it processes what discovery
// found plus every repository already stored: a stored repository that is no
// longer discovered (manifest removed, repository deleted or renamed) is still
// checked, and leaves the catalog through the normal rules.
func (ix *Indexer) Run(ctx context.Context, opts Options) (Stats, error) {
	unlock, err := ix.lock()
	if err != nil {
		return Stats{}, err
	}
	defer unlock()

	names := opts.Only
	// discovered is what this run found; canPrune is false when discovery did
	// not run or was partial.
	var discovered []string
	canPrune := false
	if len(names) == 0 {
		found, partial, err := ix.discover(ctx)
		if err != nil {
			return Stats{}, fmt.Errorf("discovery: %w", err)
		}
		discovered, canPrune = found, !partial
		if partial && ix.Prune {
			ix.log().Warn("discovery was partial; not pruning")
		}
		stored, err := ix.Store.RepoNames(ctx)
		if err != nil {
			return Stats{}, err
		}
		names = union(found, stored)
	}

	ix.log().Debug("discovery done", "repos", len(names))

	// Batch state: one GraphQL request per 50 repositories, instead of 3–4
	// REST requests per repository.
	var snaps map[string]*github.Snapshot
	if b, ok := ix.GH.(Batcher); ok && !ix.NoBatch {
		var err error
		snaps, err = b.Snapshots(ctx, names)
		ix.log().Debug("batch state fetched", "repos", len(snaps), "err", err)
		switch {
		case errors.Is(err, github.ErrNoToken):
			snaps = nil
		case err != nil:
			var rl *github.RateLimitError
			if errors.As(err, &rl) {
				return Stats{}, err
			}
			ix.log().Warn("batch query failed; using the REST API", "err", err)
			snaps = nil
		}
	}

	workers := ix.Workers
	if workers <= 0 {
		workers = 4
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	var (
		mu   sync.Mutex
		prog = Progress{Total: len(names)}
		// resolved are the canonical names the API answered for renamed or
		// transferred repositories; prune must keep them too.
		resolved []string
		fatal    error
		jobs     = make(chan string)
		wg       sync.WaitGroup
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				snap, batched := snaps[name]
				// With a local manifest, reprocess: the published one did not change, but the one that counts did.
				canonical := name
				out, err := ix.process(ctx, name, opts.Force || len(opts.ManifestOverride) > 0, snap, batched, opts.ManifestOverride, &canonical)
				mu.Lock()
				if canonical != name {
					resolved = append(resolved, canonical)
				}
				prog.Done++
				prog.Current = name
				switch {
				case err != nil:
					prog.Failed++
					var rl *github.RateLimitError
					if errors.As(err, &rl) || errors.Is(err, context.Canceled) {
						if fatal == nil {
							fatal = err
							cancel(err)
						}
					} else {
						ix.log().Warn("indexing failed", "repo", name, "err", err)
					}
				case out == outUpdated:
					prog.Updated++
				case out == outRefreshed:
					prog.Refreshed++
				case out == outRemoved:
					prog.Removed++
				case out == outSkipped:
					prog.Skipped++
				case out == outNotApp:
					prog.NotApps++
				default:
					prog.Unchanged++
				}
				// Called under the lock: callbacks run serially, in increasing Done order.
				if opts.Progress != nil {
					opts.Progress(prog)
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, n := range names {
		select {
		case jobs <- n:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()

	if fatal != nil {
		return prog.Stats, fatal
	}
	if err := ctx.Err(); err != nil {
		return prog.Stats, err
	}
	if ix.Prune && canPrune {
		n, err := ix.prune(ctx, append(discovered, resolved...))
		prog.Removed += n
		if err != nil {
			return prog.Stats, err
		}
	}
	return prog.Stats, nil
}

// union returns a followed by the names of b not in a, ignoring case.
func union(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, n := range list {
			if k := strings.ToLower(n); !seen[k] {
				seen[k] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// prune removes from the catalog what was not discovered in this run.
func (ix *Indexer) prune(ctx context.Context, found []string) (int, error) {
	keep := map[string]bool{}
	for _, n := range found {
		keep[strings.ToLower(n)] = true
	}
	all, err := ix.Store.RepoNames(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, name := range all {
		if keep[strings.ToLower(name)] {
			continue
		}
		if err := ix.Store.RemoveRepo(ctx, name); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// process applies the pipeline to a repository. snap, when batched, is the
// state fetched in batch through GraphQL (nil = repository does not exist);
// without a batch, the state comes from the REST API with conditional requests.
// canonical receives the name the API answered (renamed/transferred repo).
func (ix *Indexer) process(ctx context.Context, name string, force bool, snap *github.Snapshot, batched bool,
	overrides map[string]string, canonical *string) (outcome, error) {
	prev, err := ix.Store.RepoState(ctx, name)
	known := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return 0, err
	}
	now := ix.now()
	removed := func() (outcome, error) {
		if known {
			return outRemoved, ix.Store.RemoveRepo(ctx, name)
		}
		return outSkipped, nil
	}

	// The indexer rules changed since last time: reprocess everything.
	if known && prev.IndexVersion < Version {
		force = true
	}

	var (
		repo    *github.Repo
		newETag = prev.ETag
	)
	if batched {
		if snap == nil {
			return removed()
		}
		repo = &snap.Repo
	} else {
		etag := prev.ETag
		if force {
			etag = ""
		}
		var notModified bool
		repo, newETag, notModified, err = ix.GH.GetRepo(ctx, name, etag)
		if github.IsNotFound(err) {
			return removed()
		}
		if err != nil {
			return 0, err
		}
		if notModified {
			// Same metadata (so pushed_at and HEAD too); a release can be
			// published on an existing tag, so we check the tag.
			rel, err := ix.GH.LatestRelease(ctx, name)
			if err != nil {
				return 0, err
			}
			if tagOf(rel) == prev.LatestTag {
				return outUnchanged, ix.Store.TouchRepo(ctx, name, now)
			}
			repo, newETag, _, err = ix.GH.GetRepo(ctx, name, "")
			if err != nil {
				return 0, err
			}
		}
	}

	if repo.Archived {
		return removed()
	}
	// The API may return another name (renamed/transferred repo).
	if repo.FullName != "" && repo.FullName != name {
		if known {
			if err := ix.Store.RemoveRepo(ctx, name); err != nil {
				return 0, err
			}
		}
		// An installation recorded under the old name follows the repository,
		// so it can still be updated.
		if err := ix.Store.RenameInstall(ctx, name, repo.FullName); err != nil {
			ix.log().Warn("installation not moved to the new name", "from", name, "to", repo.FullName, "err", err)
		}
		name = repo.FullName
		*canonical = name
		prev, err = ix.Store.RepoState(ctx, name)
		known = err == nil
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return 0, err
		}
		// The reloaded state may be from an older indexer version
		// (e.g. a name discovered with different capitalization).
		if known && prev.IndexVersion < Version {
			force = true
		}
	}

	var (
		sha string
		rel *github.Release
	)
	if batched {
		sha, rel = snap.HeadSHA, snap.Release
	} else {
		lastSHA := prev.HeadSHA
		if force {
			lastSHA = ""
		}
		if sha, err = ix.GH.HeadSHA(ctx, name, repo.DefaultBranch, lastSHA); err != nil {
			return 0, err
		}
		if rel, err = ix.GH.LatestRelease(ctx, name); err != nil {
			return 0, err
		}
	}
	score := Score(repo.Stars, repo.PushedAt, now)

	// Cache check: if pushed_at, HEAD and tag did not change, do not
	// reprocess. Only the volatile data (stars etc.) is updated.
	if known && !force && prev.PushedAt.Equal(repo.PushedAt) && prev.HeadSHA == sha && prev.LatestTag == tagOf(rel) {
		if batched && prev.Stars == repo.Stars && prev.Description == repo.Description {
			return outUnchanged, ix.Store.TouchRepo(ctx, name, now)
		}
		return outRefreshed, ix.Store.UpdateStats(ctx, name, repo.Stars, repo.Description, repo.Topics, newETag, score, now)
	}

	// Only repositories with an omastore.toml declaring an app (no plugins or
	// themes) enter the catalog. The presence of the file is the author's opt-in.
	m, why, err := ix.manifestFor(ctx, name, sha, snap, batched, overrides)
	if err != nil {
		return 0, err
	}
	if m == nil {
		ix.log().Debug("out of the catalog", "repo", name, "reason", why)
		if known {
			return outRemoved, ix.Store.RemoveRepo(ctx, name)
		}
		return outNotApp, nil
	}

	app, assets, err := ix.extract(ctx, repo, sha, rel, m)
	if err != nil {
		return 0, err
	}
	app.Score = score
	r := store.Repo{
		FullName:      name,
		Description:   repo.Description,
		Stars:         repo.Stars,
		Topics:        repo.Topics,
		License:       repo.License,
		HTMLURL:       repo.HTMLURL,
		DefaultBranch: repo.DefaultBranch,
		PushedAt:      repo.PushedAt,
		HeadSHA:       sha,
		LatestTag:     tagOf(rel),
		ETag:          newETag,
		IndexedAt:     now,
		IndexVersion:  Version,
	}
	if err := ix.Store.SaveIndexed(ctx, r, app, assets); err != nil {
		return 0, err
	}
	ix.log().Debug("indexed", "repo", name, "tag", r.LatestTag, "installable", app.Installable)
	return outUpdated, nil
}

func tagOf(rel *github.Release) string {
	if rel == nil {
		return ""
	}
	return rel.Tag
}

// maxScreenshots limits the screenshots per app.
const maxScreenshots = 8

// extract builds a repository's display data and assets.
func (ix *Indexer) extract(ctx context.Context, repo *github.Repo, sha string, rel *github.Release, m *manifest.Manifest) (store.App, []store.Asset, error) {
	name := repo.FullName
	// GitHub descriptions sometimes have spaces/line breaks at the ends.
	app := store.App{FullName: name, Name: repo.Name, Summary: strings.Join(strings.Fields(repo.Description), " ")}
	app.Category = Category(repo.Topics, repo.Name, repo.Description)
	app.Manifest = m.Encode()
	if m.Name != "" {
		app.Name = m.Name
	}
	if m.Summary != "" {
		app.Summary = m.Summary
	}
	if c := m.MainCategory(); c != "" {
		app.Category = c
	}

	// Without a release there is nothing to install: store only the basics,
	// without spending requests on README and tree. When a release shows up,
	// the tag changes and the repository is fully reprocessed.
	if rel == nil {
		return app, nil, nil
	}

	readme, readmePath, err := ix.GH.Readme(ctx, name)
	if err != nil {
		return app, nil, err
	}
	urls := repoURLs{FullName: name, Ref: sha}
	if d := path.Dir(readmePath); d != "." && d != "" {
		urls.BaseDir = d
	}
	var readmeImages []string
	if readme != "" {
		app.Readme = RewriteReadme(readme, urls)
		readmeImages = ReadmeImages(app.Readme)
		if t := Title(readme); m.Name == "" && t != "" && normalize(t) == normalize(repo.Name) {
			app.Name = t // keeps the author's spelling, e.g. "OmaPhoto"
		}
		if app.Summary == "" {
			app.Summary = Summary(readme, 200)
		}
	}

	files, err := ix.listFiles(ctx, name, sha)
	filesKnown := err == nil
	if err != nil {
		// Without the file list we can still index with the README.
		ix.log().Warn("no file list", "repo", name, "err", err)
	}
	if m.Icon != "" && (!filesKnown || contains(files, m.Icon)) {
		app.IconURL = urls.Raw(m.Icon)
	} else if icon := gitrepo.FindIcon(files, repo.Name); icon != "" {
		app.IconURL = urls.Raw(icon)
	} else {
		for _, img := range readmeImages {
			l := strings.ToLower(path.Base(img))
			if strings.Contains(l, "icon") || strings.Contains(l, "logo") {
				app.IconURL = img
				break
			}
		}
	}

	seen := map[string]bool{app.IconURL: true}
	for _, img := range readmeImages {
		l := strings.ToLower(path.Base(img))
		if seen[img] || strings.Contains(l, "logo") || strings.Contains(l, "icon") || strings.Contains(l, "banner") {
			continue
		}
		seen[img] = true
		app.Screenshots = append(app.Screenshots, img)
	}
	for _, f := range gitrepo.FindScreenshots(files, 0) {
		u := urls.Raw(f)
		if !seen[u] {
			seen[u] = true
			app.Screenshots = append(app.Screenshots, u)
		}
	}
	if len(m.Screenshots) > 0 {
		app.Screenshots = nil
		for _, sh := range m.Screenshots {
			if strings.HasPrefix(sh, "https://") {
				app.Screenshots = append(app.Screenshots, sh)
			} else {
				app.Screenshots = append(app.Screenshots, urls.Raw(sh))
			}
		}
	}
	if len(app.Screenshots) > maxScreenshots {
		app.Screenshots = app.Screenshots[:maxScreenshots]
	}

	assets := releaseAssets(rel, m)
	for _, a := range assets {
		if (AssetInfo{Format: a.Format, Arch: a.Arch}).Installable(ix.goarch()) {
			app.Installable = true
			break
		}
	}
	return app, assets, nil
}

// manifestFor fetches and validates the root omastore.toml at commit sha: from
// the GraphQL batch when available, otherwise through the REST API. Returns nil
// (with the reason) if there is no usable manifest or it does not declare an app.
func (ix *Indexer) manifestFor(ctx context.Context, name, sha string, snap *github.Snapshot, batched bool,
	overrides map[string]string) (*manifest.Manifest, string, error) {
	var data string
	if content, ok := overrideFor(overrides, name); ok {
		data = content
	} else if batched {
		if snap.Manifest == nil {
			return nil, "no " + manifest.FileName, nil
		}
		data = *snap.Manifest
	} else {
		content, found, err := ix.GH.File(ctx, name, manifest.FileName, sha, manifest.MaxSize)
		if err != nil {
			var rl *github.RateLimitError
			if errors.As(err, &rl) {
				return nil, "", err
			}
			return nil, "unreadable manifest: " + err.Error(), nil
		}
		if !found {
			return nil, "no " + manifest.FileName, nil
		}
		data = content
	}
	m, problems, err := manifest.Parse([]byte(data), false)
	if err != nil {
		return nil, "invalid manifest: " + err.Error(), nil
	}
	for _, p := range problems {
		ix.log().Info("manifest problem", "repo", name, "problem", p.String())
	}
	if !m.IsApp() {
		return nil, "kind = " + m.Kind + " (only apps are indexed)", nil
	}
	return m, "", nil
}

func overrideFor(overrides map[string]string, name string) (string, bool) {
	for k, v := range overrides {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return "", false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// listFiles lists the repo files through the Trees API; if the listing is
// truncated and there is a clone cache, it clones and lists locally.
func (ix *Indexer) listFiles(ctx context.Context, name, sha string) ([]string, error) {
	files, truncated, err := ix.GH.Tree(ctx, name, sha)
	if err == nil && !truncated {
		return files, nil
	}
	if ix.Repos == nil {
		return files, err
	}
	dir, _, cerr := ix.Repos.Sync(ctx, name)
	if cerr != nil {
		if err == nil {
			return files, nil // truncated, but better than nothing
		}
		return nil, errors.Join(err, cerr)
	}
	return gitrepo.ListFiles(dir)
}

// releaseAssets classifies the release assets and keeps the installable ones
// (of any supported architecture), linked to the matching checksum.
// Assets matching the manifest pattern come in with the declared
// architecture, even if the name would not allow inferring it.
func releaseAssets(rel *github.Release, m *manifest.Manifest) []store.Asset {
	if rel == nil {
		return nil
	}
	declared := map[string]string{} // asset name → manifest architecture
	if m != nil {
		for arch, t := range m.Linux {
			if t.Asset == "" {
				continue
			}
			for _, a := range rel.Assets {
				if manifest.MatchAsset(t.Asset, rel.Tag, a.Name) && !ClassifyAsset(a.Name).Checksum {
					declared[a.Name] = arch
				}
			}
		}
	}
	sums := map[string]string{} // asset name → URL of its .sha256
	general := ""               // checksums.txt / SHA256SUMS
	for _, a := range rel.Assets {
		if !ClassifyAsset(a.Name).Checksum {
			continue
		}
		lower := strings.ToLower(a.Name)
		if ext := path.Ext(lower); ext == ".sha256" || ext == ".sha256sum" || ext == ".sha512" || ext == ".sha512sum" {
			sums[strings.TrimSuffix(a.Name, path.Ext(a.Name))] = a.URL
		} else if general == "" || strings.Contains(lower, "sha256") {
			general = a.URL
		}
	}
	var out []store.Asset
	for _, a := range rel.Assets {
		info := ClassifyAsset(a.Name)
		if arch, ok := declared[a.Name]; ok {
			info = AssetInfo{Format: FormatOf(a.Name), Arch: arch}
		}
		if info.Format == "" {
			continue
		}
		sum := sums[a.Name]
		if sum == "" {
			sum = general
		}
		out = append(out, store.Asset{
			Tag: rel.Tag, Name: a.Name, URL: a.URL, Size: a.Size,
			Arch: info.Arch, Format: info.Format, Digest: a.Digest, ChecksumURL: sum,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func normalize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == ' ' || r == '.' {
			return -1
		}
		return r
	}, strings.ToLower(s))
}
