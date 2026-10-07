package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/index"
	"github.com/KitsuneForgering/OmaStore/backend/internal/install"
	"github.com/KitsuneForgering/OmaStore/backend/internal/manifest"
	"github.com/KitsuneForgering/OmaStore/backend/internal/provenance"
	"github.com/KitsuneForgering/OmaStore/backend/internal/repoid"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
	"github.com/KitsuneForgering/OmaStore/backend/internal/sysdeps"
)

func umask(m int) int { return syscall.Umask(m) }

// Backend is the set of operations the server exposes.
type Backend interface {
	ListApps(ctx context.Context, f store.Filter) ([]store.ListItem, error)
	GetApp(ctx context.Context, fullName string) (*store.AppDetail, error)
	Similar(ctx context.Context, fullName string, limit int) ([]store.ListItem, error)
	Categories(ctx context.Context) ([]store.CategoryCount, error)
	ListInstalls(ctx context.Context) ([]store.Install, error)
	Index(ctx context.Context, opts index.Options) (index.Stats, error)
	Install(ctx context.Context, fullName string, opts install.Options) (*store.Install, error)
	Update(ctx context.Context, fullName string, opts install.Options) (*store.Install, error)
	Uninstall(ctx context.Context, fullName string, force bool) error
	Rollback(ctx context.Context, fullName string) (*store.Install, error)
	Image(ctx context.Context, url string) (string, error)
	Check(ctx context.Context, fullName string, manifest *string) (*index.Report, error)
	Starred(ctx context.Context, fullName string) (bool, error)
	Star(ctx context.Context, fullName string, starred bool) (int, error)
	SysDeps(ctx context.Context, fullName string) (sysdeps.Report, error)
	InstallSysDeps(ctx context.Context, fullName string) (sysdeps.Report, error)
	SelfStatus(ctx context.Context) (install.SelfStatus, error)
	SelfUpdate(ctx context.Context, progress func(install.Progress)) (*install.SelfResult, error)
}

// DTOs: the JSON format exposed to the frontend, stable and in camelCase,
// independent of the internal structs.

// AppItem is a catalog row.
type AppItem struct {
	Repo             string   `json:"repo"`
	Name             string   `json:"name"`
	Summary          string   `json:"summary"`
	IconURL          string   `json:"iconUrl"`
	Screenshots      []string `json:"screenshots"`
	Category         string   `json:"category"`
	Stars            int      `json:"stars"`
	Score            float64  `json:"score"`
	Installable      bool     `json:"installable"`
	LatestVersion    string   `json:"latestVersion"`
	InstalledVersion string   `json:"installedVersion"`
	UpdateAvailable  bool     `json:"updateAvailable"`
	// Blocked: on OmaStore's blocklist (listed only when installed);
	// blockedReason says why ("" when the list gives no reason).
	Blocked       bool   `json:"blocked"`
	BlockedReason string `json:"blockedReason"`
}

// AssetInfo is a release asset.
type AssetInfo struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Arch   string `json:"arch"`
	Format string `json:"format"`
	// What can check the download: "digest" (GitHub's sha256 of the file),
	// "file" (a checksum file in the release) or "" (nothing). Availability
	// only: the check itself happens during the install.
	Checksum string `json:"checksum"`
	// Verified build provenance (a GitHub artifact attestation signed by a
	// workflow of the repository itself); nil when there is none.
	Provenance *provenance.Result `json:"provenance"`
}

// InstallInfo is an installed app.
type InstallInfo struct {
	Repo        string    `json:"repo"`
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installedAt"`
	ExecPath    string    `json:"execPath"`
	DesktopPath string    `json:"desktopPath"`
	// The version install.rollback goes back to; "" when there is none.
	PreviousVersion string `json:"previousVersion"`
	// The executable is gone (removed outside OmaStore): reinstall to repair.
	Broken   bool                `json:"broken"`
	History  []InstallEventInfo  `json:"history,omitempty"`
	Services []store.Integration `json:"services,omitempty"`
}

// InstallEventInfo is one recorded install, update or rollback.
type InstallEventInfo struct {
	Action      string    `json:"action"`
	FromVersion string    `json:"fromVersion"`
	ToVersion   string    `json:"toVersion"`
	At          time.Time `json:"at"`
}

// AppDetail is an app's detail.
type AppDetail struct {
	AppItem
	Readme       string      `json:"readme"`
	Changelog    string      `json:"changelog"`
	ReleaseNotes string      `json:"releaseNotes"`
	Description  string      `json:"description"`
	License      string      `json:"license"`
	Topics       []string    `json:"topics"`
	HTMLURL      string      `json:"htmlUrl"`
	PushedAt     time.Time   `json:"pushedAt,omitzero"`
	IndexedAt    time.Time   `json:"indexedAt,omitzero"`
	Assets       []AssetInfo `json:"assets"`
	// The file an install would download on this machine; nil if none fits.
	SelectedAsset *AssetInfo   `json:"selectedAsset"`
	Install       *InstallInfo `json:"install"`
}

// IndexResult is the result of an indexing job.
type IndexResult struct {
	Updated   int `json:"updated"`
	Refreshed int `json:"refreshed"`
	Unchanged int `json:"unchanged"`
	Removed   int `json:"removed"`
	Skipped   int `json:"skipped"`
	NotApps   int `json:"notApps"`
	Failed    int `json:"failed"`
}

// CheckItem is one line of a compatibility report.
type CheckItem struct {
	Status string `json:"status"` // "ok", "warning" or "fail"
	Item   string `json:"item"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

// CheckReport tells an app author what the store understands of a repository.
type CheckReport struct {
	Repo              string      `json:"repo"`
	Compatible        bool        `json:"compatible"`
	Name              string      `json:"name"`
	Summary           string      `json:"summary"`
	Category          string      `json:"category"`
	IconURL           string      `json:"iconUrl"`
	Screenshots       []string    `json:"screenshots"`
	Tag               string      `json:"tag"`
	Checks            []CheckItem `json:"checks"`
	SuggestedManifest string      `json:"suggestedManifest"`
	// LocalManifest: the author's unpublished manifest was tested; compatible
	// then means compatible once it is pushed.
	LocalManifest bool `json:"localManifest"`
}

// SysDep is a system dependency declared in the app's PKGBUILD/.SRCINFO.
type SysDep struct {
	Name     string `json:"name"`
	Spec     string `json:"spec"` // with the version constraint, if any
	Reason   string `json:"reason"`
	Optional bool   `json:"optional"`
	// Status: "installed", "available" (missing, in a pacman repository),
	// "unavailable" (missing, not in any repository: AUR) or "unknown" (no pacman).
	Status  string `json:"status"`
	Package string `json:"package"` // "repo/name" that would be installed
}

// DepsReport is the state of an app's system dependencies on this machine.
type DepsReport struct {
	Repo      string   `json:"repo"`
	Source    string   `json:"source"` // file in the repository ("" if none)
	Pacman    bool     `json:"pacman"` // false: this system has no pacman
	Deps      []SysDep `json:"deps"`
	Missing   int      `json:"missing"`   // not installed (available or not), libraries included
	ToInstall []string `json:"toInstall"` // what deps.install would install
	// Shared libraries the installed executable needs and this system lacks
	// (read from its ELF headers, never run).
	Libraries []sysdeps.LibState `json:"libraries"`
	// The installed executable was built for another machine ("" if not).
	WrongArch string `json:"wrongArch"`
}

func toDeps(repo string, r sysdeps.Report, pacman bool) DepsReport {
	out := DepsReport{Repo: repo, Source: r.Source, Pacman: pacman, Deps: []SysDep{}, ToInstall: nonNil(r.ToInstall()),
		Libraries: []sysdeps.LibState{}, WrongArch: r.WrongArch}
	out.Libraries = append(out.Libraries, r.Libraries...)
	out.Missing += len(r.Libraries)
	for _, d := range r.Deps {
		out.Deps = append(out.Deps, SysDep{Name: d.Name(), Spec: d.Spec, Reason: d.Reason, Optional: d.Optional,
			Status: d.Status, Package: d.Package})
		if d.Status == sysdeps.StatusAvailable || d.Status == sysdeps.StatusUnavailable {
			out.Missing++
		}
	}
	return out
}

func toReport(r *index.Report) CheckReport {
	out := CheckReport{Repo: r.Repo, Compatible: r.Compatible(), Name: r.Name, Summary: r.Summary,
		Category: r.Category, IconURL: r.IconURL, Screenshots: nonNil(r.Screenshots), Tag: r.Tag,
		Checks: []CheckItem{}, SuggestedManifest: r.SuggestedManifest, LocalManifest: r.LocalManifest}
	for _, c := range r.Checks {
		out.Checks = append(out.Checks, CheckItem{Status: c.Status, Item: c.Item, Detail: c.Detail, Fix: c.Fix})
	}
	return out
}

func toItem(it store.ListItem) AppItem {
	return AppItem{
		Repo: it.FullName, Name: it.Name, Summary: it.Summary, IconURL: it.IconURL,
		Screenshots: nonNil(it.Screenshots), Category: it.Category, Stars: it.Stars, Score: it.Score,
		Installable: it.Installable, LatestVersion: it.LatestTag, InstalledVersion: it.InstalledVersion,
		// A blocked app is never updated, so it offers no update.
		UpdateAvailable: !it.Blocked && it.InstalledVersion != "" && it.LatestTag != "" && it.InstalledVersion != it.LatestTag,
		Blocked:         it.Blocked, BlockedReason: it.BlockedReason,
	}
}

func toInstall(in store.Install) InstallInfo {
	out := InstallInfo{Repo: in.FullName, Version: in.Version, InstalledAt: in.InstalledAt,
		ExecPath: in.ExecPath, DesktopPath: in.DesktopPath, PreviousVersion: in.PreviousVersion,
		Broken: install.IsBroken(in), Services: in.Integrations}
	for _, event := range in.History {
		out.History = append(out.History, InstallEventInfo{Action: event.Action, FromVersion: event.FromVersion,
			ToVersion: event.ToVersion, At: event.At})
	}
	return out
}

func toDetail(d *store.AppDetail) AppDetail {
	it := store.ListItem{App: d.App, Stars: d.Repo.Stars, LatestTag: d.Repo.LatestTag,
		Blocked: d.Blocked, BlockedReason: d.BlockedReason}
	if d.Install != nil {
		it.InstalledVersion = d.Install.Version
	}
	out := AppDetail{
		AppItem: toItem(it), Readme: d.Readme, Changelog: d.Changelog, ReleaseNotes: d.Repo.ReleaseNotes,
		Description: d.Repo.Description, License: d.Repo.License,
		Topics: nonNil(d.Repo.Topics), HTMLURL: d.Repo.HTMLURL, PushedAt: d.Repo.PushedAt,
		IndexedAt: d.Repo.IndexedAt, Assets: []AssetInfo{},
	}
	for _, a := range d.Assets {
		out.Assets = append(out.Assets, toAsset(a))
	}
	if sel, ok := install.SelectAsset(d.Assets, runtime.GOARCH, manifest.Decode(d.Manifest)); ok {
		a := toAsset(sel)
		out.SelectedAsset = &a
	}
	if d.Install != nil {
		ii := toInstall(*d.Install)
		out.Install = &ii
	}
	return out
}

func toAsset(a store.Asset) AssetInfo {
	out := AssetInfo{Name: a.Name, Size: a.Size, Arch: a.Arch, Format: a.Format}
	switch {
	case install.Verifiable(store.Asset{Digest: a.Digest}):
		out.Checksum = "digest"
	case a.ChecksumURL != "":
		out.Checksum = "file"
	}
	if a.Provenance != "" {
		var p provenance.Result
		if json.Unmarshal([]byte(a.Provenance), &p) == nil && p.Workflow != "" {
			out.Provenance = &p
		}
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// SelfInfo is OmaStore's own update status.
type SelfInfo struct {
	Mode            string `json:"mode"`    // "self", "package" or "dev"
	Version         string `json:"version"` // running version, "" unless mode is "self"
	Latest          string `json:"latest"`  // latest release, "" if unknown
	UpdateAvailable bool   `json:"updateAvailable"`
	Notes           string `json:"notes"`
	CheckError      string `json:"checkError,omitempty"` // why the latest release is unknown
}

// SelfUpdateResult is the result of a self.update job.
type SelfUpdateResult struct {
	From string `json:"from"`
	To   string `json:"to"`
	GUI  string `json:"gui"` // launcher of the new interface, to restart into
}

// Method parameters.
type (
	listParams struct {
		Category  string `json:"category"`
		Query     string `json:"query"`
		Installed bool   `json:"installed"`
		All       bool   `json:"all"`
		Limit     int    `json:"limit"`
		Offset    int    `json:"offset"`
	}
	repoParams struct {
		Repo string `json:"repo"`
	}
	installParams struct {
		Repo            string `json:"repo"`
		AllowUnverified bool   `json:"allowUnverified"`
	}
	uninstallParams struct {
		Repo  string `json:"repo"`
		Force bool   `json:"force"`
	}
	similarParams struct {
		Repo  string `json:"repo"`
		Limit int    `json:"limit"`
	}
	indexParams struct {
		Force bool     `json:"force"`
		Repos []string `json:"repos"`
	}
	jobParams struct {
		Job string `json:"job"`
	}
	imageParams struct {
		URL string `json:"url"`
	}
	starParams struct {
		Repo    string `json:"repo"`
		Starred bool   `json:"starred"`
	}
	checkParams struct {
		Repo     string  `json:"repo"`
		Manifest *string `json:"manifest"`
	}
)

// decode reads params into dst; missing params count as {}.
func decode(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errInvalidParams("invalid params: %v", err)
	}
	return nil
}

func (p repoParams) validate() error {
	if err := repoid.Validate(p.Repo); err != nil {
		return errInvalidParams(`"repo" must be a valid "owner/repo", got %q`, p.Repo)
	}
	return nil
}

// callTimeout limits synchronous calls (long ones become jobs).
const callTimeout = 2 * time.Minute

// call dispatches a method.
func (s *Server) call(method string, raw json.RawMessage) (any, error) {
	ctx, cancel := context.WithTimeout(s.ctx, callTimeout)
	defer cancel()
	b := s.backend

	switch method {
	case "daemon.hello":
		return map[string]any{"protocol": ProtocolVersion, "name": "omastored", "methods": Methods}, nil

	case "catalog.list":
		var p listParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if p.Limit < 0 || p.Offset < 0 {
			return nil, errInvalidParams("negative limit/offset")
		}
		items, err := b.ListApps(ctx, store.Filter{Category: p.Category, Query: p.Query,
			InstalledOnly: p.Installed, All: p.All, Limit: p.Limit, Offset: p.Offset})
		if err != nil {
			return nil, err
		}
		out := make([]AppItem, 0, len(items))
		for _, it := range items {
			out = append(out, toItem(it))
		}
		return out, nil

	case "catalog.get":
		var p repoParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := p.validate(); err != nil {
			return nil, err
		}
		d, err := b.GetApp(ctx, p.Repo)
		if err != nil {
			return nil, err
		}
		return toDetail(d), nil

	case "catalog.similar":
		var p similarParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := (repoParams{Repo: p.Repo}).validate(); err != nil {
			return nil, err
		}
		if p.Limit <= 0 || p.Limit > 50 {
			p.Limit = 8
		}
		items, err := b.Similar(ctx, p.Repo, p.Limit)
		if err != nil {
			return nil, err
		}
		out := make([]AppItem, 0, len(items))
		for _, it := range items {
			out = append(out, toItem(it))
		}
		return out, nil

	case "catalog.categories":
		cats, err := b.Categories(ctx)
		if err != nil {
			return nil, err
		}
		type cat struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}
		out := make([]cat, 0, len(cats))
		for _, c := range cats {
			out = append(out, cat{c.Category, c.Count})
		}
		return out, nil

	case "installs.list":
		ins, err := b.ListInstalls(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]InstallInfo, 0, len(ins))
		for _, in := range ins {
			out = append(out, toInstall(in))
		}
		return out, nil

	case "index.start":
		var p indexParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		return s.startIndex(index.Options{Force: p.Force, Only: p.Repos}, nil)

	case "install.start", "update.start":
		var p installParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := (repoParams{Repo: p.Repo}).validate(); err != nil {
			return nil, err
		}
		kind := KindInstall
		if method == "update.start" {
			kind = KindUpdate
		}
		return s.startInstallJob(kind, p.Repo, p.AllowUnverified)

	case "settings.get":
		return s.settings(ctx, nil)

	case "settings.set":
		if len(raw) == 0 {
			raw = []byte("{}")
		}
		return s.settings(ctx, raw)

	case "install.uninstall":
		var p uninstallParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := (repoParams{Repo: p.Repo}).validate(); err != nil {
			return nil, err
		}
		// Cannot uninstall in the middle of an installation of the same app.
		for _, j := range s.jobs.list() {
			if j.State == StateRunning && j.Kind != KindIndex && strings.EqualFold(j.Repo, p.Repo) {
				return nil, ErrBusy
			}
		}
		if err := b.Uninstall(ctx, p.Repo, p.Force); err != nil {
			return nil, err
		}
		s.broadcast("catalog.changed", map[string]string{"repo": p.Repo})
		return struct{}{}, nil

	case "install.rollback":
		var p repoParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := p.validate(); err != nil {
			return nil, err
		}
		for _, j := range s.jobs.list() {
			if j.State == StateRunning && j.Kind != KindIndex && strings.EqualFold(j.Repo, p.Repo) {
				return nil, ErrBusy
			}
		}
		inst, err := b.Rollback(ctx, p.Repo)
		if err != nil {
			return nil, err
		}
		s.broadcast("catalog.changed", map[string]string{"repo": p.Repo})
		return toInstall(*inst), nil

	case "jobs.list":
		return s.jobs.list(), nil

	case "jobs.cancel":
		var p jobParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		return struct{}{}, s.jobs.cancel(p.Job)

	case "author.check":
		var p checkParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := (repoParams{Repo: p.Repo}).validate(); err != nil {
			return nil, err
		}
		r, err := b.Check(ctx, p.Repo, p.Manifest)
		if err != nil {
			return nil, err
		}
		return toReport(r), nil

	case "star.get":
		var p repoParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := p.validate(); err != nil {
			return nil, err
		}
		starred, err := b.Starred(ctx, p.Repo)
		if err != nil {
			return nil, err
		}
		return map[string]bool{"starred": starred}, nil

	case "star.set":
		var p starParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := (repoParams{Repo: p.Repo}).validate(); err != nil {
			return nil, err
		}
		stars, err := b.Star(ctx, p.Repo, p.Starred)
		if err != nil {
			return nil, err
		}
		s.broadcast("catalog.changed", map[string]string{"repo": p.Repo})
		return map[string]any{"starred": p.Starred, "stars": stars}, nil

	case "deps.check":
		var p repoParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := p.validate(); err != nil {
			return nil, err
		}
		r, err := b.SysDeps(ctx, p.Repo)
		noPacman := errors.Is(err, sysdeps.ErrNoPacman)
		if err != nil && !noPacman {
			return nil, err
		}
		return toDeps(p.Repo, r, !noPacman), nil

	case "deps.install":
		var p repoParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := p.validate(); err != nil {
			return nil, err
		}
		return s.jobs.start(s.ctx, KindDeps, p.Repo, func(ctx context.Context, report func(progress)) (any, error) {
			report(progress{Stage: "authorize"})
			r, err := b.InstallSysDeps(ctx, p.Repo)
			return toDeps(p.Repo, r, true), err
		}, nil)

	case "self.status":
		st, err := b.SelfStatus(ctx)
		info := SelfInfo{Mode: st.Mode, Version: st.Version, Latest: st.Latest,
			UpdateAvailable: st.UpdateAvailable, Notes: st.Notes}
		if err != nil {
			// The installation mode is still useful without GitHub.
			s.log.Warn("OmaStore release not checked", "err", err)
			info.CheckError = err.Error()
		}
		return info, nil

	case "self.update":
		return s.jobs.start(s.ctx, KindSelf, "", func(ctx context.Context, report func(progress)) (any, error) {
			r, err := b.SelfUpdate(ctx, func(ip install.Progress) {
				report(progress{Stage: ip.Stage, Done: ip.Done, Total: ip.Total})
			})
			if r != nil {
				return SelfUpdateResult{From: r.From, To: r.To, GUI: r.GUI}, err
			}
			return nil, err
		}, nil)

	case "self.restart":
		// The daemon exits so the next one starts from the new version.
		if s.jobs.running() > 0 {
			return nil, ErrBusy
		}
		s.requestRestart()
		return struct{}{}, nil

	case "image.get":
		var p imageParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		path, err := b.Image(ctx, p.URL)
		if err != nil {
			return nil, err
		}
		return map[string]string{"path": path}, nil
	}
	return nil, &Error{Code: CodeMethodNotFound, Message: "unknown method: " + method}
}
