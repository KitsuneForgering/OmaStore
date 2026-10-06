package index

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/KitsuneForgering/OmaStore/backend/internal/asset"
	"github.com/KitsuneForgering/OmaStore/backend/internal/github"
	"github.com/KitsuneForgering/OmaStore/backend/internal/manifest"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

// Check statuses: a failure keeps the app out of the catalog or from
// installing; a warning only makes the experience worse.
const (
	CheckOK   = "ok"
	CheckWarn = "warning"
	CheckFail = "fail"
)

// Check is one item of a compatibility report.
type Check struct {
	Status string
	Item   string
	Detail string
	Fix    string
}

// Report is what the store understands of a repository, for its author.
type Report struct {
	Repo        string
	Name        string
	Summary     string
	Category    string
	IconURL     string
	Screenshots []string
	Tag         string
	Assets      []store.Asset
	Checks      []Check
	// SuggestedManifest is a starter omastore.toml built from the release,
	// when the repository has no manifest or declares no asset.
	SuggestedManifest string
	// LocalManifest means the report used a manifest given by the author
	// instead of the published one: compatible then means "compatible once
	// this file is pushed", not "ready".
	LocalManifest bool
}

// Compatible reports whether nothing failed.
func (r *Report) Compatible() bool {
	for _, c := range r.Checks {
		if c.Status == CheckFail {
			return false
		}
	}
	return true
}

func (r *Report) add(status, item, detail, fix string) {
	r.Checks = append(r.Checks, Check{Status: status, Item: item, Detail: detail, Fix: fix})
}

// Check diagnoses whether a repository can enter the catalog and be installed
// on this machine, running the same rules as indexing without writing to the
// database. override, when not nil, is used as the omastore.toml instead of
// the published one (to test a manifest before pushing it).
func (ix *Indexer) Check(ctx context.Context, name string, override *string) (*Report, error) {
	r := &Report{Repo: name, LocalManifest: override != nil}
	repo, _, _, err := ix.GH.GetRepo(ctx, name, "")
	if github.IsNotFound(err) {
		r.add(CheckFail, "Repository", "not found, or private", "make the repository public on GitHub")
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if repo.FullName != "" {
		name, r.Repo = repo.FullName, repo.FullName
	}
	if repo.Archived {
		r.add(CheckFail, "Repository", "archived: archived repositories are left out", "unarchive it on GitHub")
		return r, nil
	}

	// Manifest: a missing one fails, but the rest of the report still shows
	// what the store would infer, with a suggested file to start from.
	var data string
	found := true
	if override != nil {
		data = *override
	} else if data, found, err = ix.GH.File(ctx, name, manifest.FileName, repo.DefaultBranch, manifest.MaxSize); err != nil {
		var rl *github.RateLimitError
		if errors.As(err, &rl) {
			return nil, err
		}
		r.add(CheckFail, manifest.FileName, "unreadable: "+err.Error(), "")
		found = false
	}
	m := &manifest.Manifest{Kind: manifest.KindApp}
	switch {
	case !found:
		r.add(CheckFail, manifest.FileName, "missing at the root of the default branch: the store ignores the repository",
			"add omastore.toml (it may be empty; the suggestion below is a start) and push it")
	default:
		parsed, problems, err := manifest.Parse([]byte(data), true)
		if err != nil {
			r.add(CheckFail, manifest.FileName, err.Error(), "fix the TOML syntax; `omastore lint-manifest` shows the line")
			return r, nil
		}
		for _, p := range problems {
			status := CheckWarn
			if !p.Warning && strings.HasPrefix(p.Field, "services.") {
				status = CheckFail
			}
			r.add(status, manifest.FileName, p.String(), "fix the service declaration; see the authors guide")
		}
		if parsed == nil { // unknown fields: read it the lenient way, like the indexer
			var lenientProblems []manifest.Problem
			parsed, lenientProblems, _ = manifest.Parse([]byte(data), false)
			for _, p := range lenientProblems {
				if !p.Warning && strings.HasPrefix(p.Field, "services.") {
					r.add(CheckFail, manifest.FileName, p.String(), "fix the service declaration; see the authors guide")
				}
			}
		}
		m = parsed
		if !m.IsApp() {
			r.add(CheckFail, "Kind", "kind = "+m.Kind+": OmaStore only lists standalone apps",
				`remove kind or set kind = "app"`)
			return r, nil
		}
		where := "published"
		if override != nil {
			where = "local, not published yet"
		}
		r.add(CheckOK, manifest.FileName, where, "")
	}

	// Code search for omastore.toml needs a GitHub token; the topic search
	// also works anonymously, so without the topic many users never find it.
	if contains(repo.Topics, "omarchy") {
		r.add(CheckOK, "Discovery", "omarchy topic: found with or without a GitHub token", "")
	} else {
		r.add(CheckWarn, "Discovery", "no omarchy topic: only found by manifest search, which needs a GitHub token",
			"add the omarchy topic to the repository (Settings → Topics)")
	}

	sha, err := ix.GH.HeadSHA(ctx, name, repo.DefaultBranch, "")
	if err != nil {
		return nil, err
	}
	rel, err := ix.GH.LatestRelease(ctx, name)
	if err != nil {
		return nil, err
	}
	app, assets, err := ix.extract(ctx, repo, sha, rel, m)
	if err != nil {
		return nil, err
	}
	r.Name, r.Summary, r.Category = app.Name, app.Summary, app.Category
	r.IconURL, r.Screenshots, r.Tag, r.Assets = app.IconURL, app.Screenshots, tagOf(rel), assets

	if app.Summary != "" {
		r.add(CheckOK, "Summary", app.Summary, "")
	} else {
		r.add(CheckWarn, "Summary", "empty", "fill in the repository description or summary in the manifest")
	}
	if len(repo.Topics) == 0 && m.MainCategory() == "" {
		r.add(CheckWarn, "Category", app.Category+" (inferred from the description)",
			"add topics to the repository or categories in the manifest")
	} else {
		r.add(CheckOK, "Category", app.Category, "")
	}
	if repo.License != "" {
		r.add(CheckOK, "License", repo.License, "")
	} else {
		r.add(CheckWarn, "License", "not detected", "add a LICENSE file")
	}

	if rel == nil {
		r.add(CheckFail, "Release", "no stable release (pre-releases and drafts do not count)",
			"publish a release with a Linux binary, e.g. myapp-1.0.0-x86_64-linux.tar.gz")
		r.SuggestedManifest = suggestManifest(repo, app, nil, "", m, found)
		return r, nil
	}
	r.add(CheckOK, "Release", rel.Tag, "")

	if app.IconURL != "" {
		r.add(CheckOK, "Icon", app.IconURL, "")
	} else {
		r.add(CheckWarn, "Icon", "none found", "commit an SVG or PNG and declare icon in the manifest")
	}
	if n := len(app.Screenshots); n > 0 {
		r.add(CheckOK, "Screenshots", fmt.Sprintf("%d found", n), "")
	} else {
		r.add(CheckWarn, "Screenshots", "none found", "add images to the README or screenshots in the manifest")
	}

	goarch := ix.goarch()
	for arch, t := range m.Linux {
		if t.Asset == "" {
			continue
		}
		matched := false
		for _, a := range rel.Assets {
			if manifest.MatchAsset(t.Asset, rel.Tag, a.Name) {
				matched = true
				break
			}
		}
		if !matched {
			r.add(CheckWarn, "Declared asset", fmt.Sprintf("%q (%s) matches no file of %s: the app is not installable on %s",
				t.Asset, archLabel(arch), rel.Tag, archLabel(arch)), "fix the pattern; {version} is the tag without the v")
		}
	}
	names := make([]string, 0, len(rel.Assets))
	for _, a := range rel.Assets {
		names = append(names, a.Name)
	}
	if app.Installable {
		r.add(CheckOK, "Installable on "+archLabel(goarch), assetList(assets, goarch), "")
	} else {
		detail := "the release has no files"
		if len(names) > 0 {
			detail = "no Linux binary for this architecture; release files: " + strings.Join(names, ", ")
		}
		r.add(CheckFail, "Installable on "+archLabel(goarch), detail,
			"publish myapp-<version>-"+archLabel(goarch)+"-linux.tar.gz, or declare the asset in the manifest")
	}
	for _, arch := range []string{asset.ArchAMD64, asset.ArchARM64} {
		if arch == goarch {
			continue
		}
		if !hasArch(assets, arch) {
			r.add(CheckWarn, "Build for "+archLabel(arch), "no asset for this architecture",
				"add an "+archLabel(arch)+" build to the release workflow")
		}
	}
	var unverified []string
	for _, a := range assets {
		if a.Digest == "" && a.ChecksumURL == "" {
			unverified = append(unverified, a.Name)
		}
	}
	switch {
	case len(assets) == 0:
	case len(unverified) > 0:
		r.add(CheckWarn, "Checksum", "no sha256 for "+strings.Join(unverified, ", "),
			"publish checksums.txt; users are asked to confirm installs without one")
	default:
		r.add(CheckOK, "Checksum", "every asset has a verifiable sha256", "")
	}
	if len(assets) > 0 && ix.Provenance != nil {
		var attested []string
		for _, a := range assets {
			if a.Provenance != "" {
				attested = append(attested, a.Name)
			}
		}
		if len(attested) > 0 {
			r.add(CheckOK, "Build provenance", "verified for "+strings.Join(attested, ", "), "")
		} else {
			r.add(CheckWarn, "Build provenance", "no verifiable GitHub attestation for the files OmaStore installs",
				"add actions/attest-build-provenance to the release workflow (id-token: write, attestations: write); "+
					"users who require provenance cannot install it")
		}
	}
	r.SuggestedManifest = suggestManifest(repo, app, rel, goarch, m, found)
	return r, nil
}

func archLabel(goarch string) string {
	switch goarch {
	case asset.ArchAMD64:
		return "x86_64"
	case asset.ArchARM64:
		return "aarch64"
	}
	return goarch
}

func hasArch(assets []store.Asset, arch string) bool {
	for _, a := range assets {
		if a.Arch == arch || a.Arch == "" {
			return true
		}
	}
	return false
}

func assetList(assets []store.Asset, goarch string) string {
	var out []string
	for _, a := range assets {
		if (asset.Info{Format: a.Format, Arch: a.Arch}).Installable(goarch) {
			out = append(out, a.Name)
		}
	}
	return strings.Join(out, ", ")
}

// suggestManifest writes a starter omastore.toml from what the store
// inferred: name, summary, icon and one asset pattern per architecture, with
// the version replaced by {version}. Empty when the published manifest
// already declares assets.
func suggestManifest(repo *github.Repo, app store.App, rel *github.Release, goarch string, m *manifest.Manifest, found bool) string {
	if found && len(m.Linux) > 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# omastore.toml: every field is optional; declare what the store gets wrong.\n")
	b.WriteString("kind = \"app\"\n")
	if app.Name != "" {
		fmt.Fprintf(&b, "name = %s\n", tomlString(app.Name))
	}
	if app.Summary != "" {
		fmt.Fprintf(&b, "summary = %s\n", tomlString(app.Summary))
	}
	prefix := "https://raw.githubusercontent.com/" + repo.FullName + "/"
	if p, ok := strings.CutPrefix(app.IconURL, prefix); ok {
		if _, path, ok := strings.Cut(p, "/"); ok {
			fmt.Fprintf(&b, "icon = %s\n", tomlString(path))
		}
	}
	if rel == nil {
		return b.String()
	}
	version := strings.TrimPrefix(strings.TrimPrefix(rel.Tag, "v"), "V")
	best := map[string]store.Asset{}
	for _, a := range releaseAssets(rel, nil) {
		if a.Arch == "" {
			continue
		}
		if cur, ok := best[a.Arch]; !ok || asset.FormatRank(a.Format) < asset.FormatRank(cur.Format) {
			best[a.Arch] = a
		}
	}
	arches := make([]string, 0, len(best))
	for arch := range best {
		arches = append(arches, arch)
	}
	sort.Strings(arches)
	for _, arch := range arches {
		pattern := best[arch].Name
		if version != "" {
			pattern = strings.ReplaceAll(pattern, version, "{version}")
		}
		fmt.Fprintf(&b, "\n[linux.%s]\nasset = %s\n", archLabel(arch), tomlString(pattern))
	}
	return b.String()
}

// tomlString quotes s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
