package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"syscall"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/install"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/repoid"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
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
	Install(ctx context.Context, fullName string, progress func(install.Progress)) (*store.Install, error)
	Update(ctx context.Context, fullName string, progress func(install.Progress)) (*store.Install, error)
	Uninstall(ctx context.Context, fullName string) error
	Image(ctx context.Context, url string) (string, error)
	Check(ctx context.Context, fullName string, manifest *string) (*index.Report, error)
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
}

// AssetInfo is a release asset.
type AssetInfo struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Arch     string `json:"arch"`
	Format   string `json:"format"`
	Verified bool   `json:"verified"` // there is a digest or a checksum file
}

// InstallInfo is an installed app.
type InstallInfo struct {
	Repo        string    `json:"repo"`
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installedAt"`
	ExecPath    string    `json:"execPath"`
	DesktopPath string    `json:"desktopPath"`
}

// AppDetail is an app's detail.
type AppDetail struct {
	AppItem
	Readme      string       `json:"readme"`
	Description string       `json:"description"`
	License     string       `json:"license"`
	Topics      []string     `json:"topics"`
	HTMLURL     string       `json:"htmlUrl"`
	PushedAt    time.Time    `json:"pushedAt,omitzero"`
	IndexedAt   time.Time    `json:"indexedAt,omitzero"`
	Assets      []AssetInfo  `json:"assets"`
	Install     *InstallInfo `json:"install"`
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
}

func toReport(r *index.Report) CheckReport {
	out := CheckReport{Repo: r.Repo, Compatible: r.Compatible(), Name: r.Name, Summary: r.Summary,
		Category: r.Category, IconURL: r.IconURL, Screenshots: nonNil(r.Screenshots), Tag: r.Tag,
		Checks: []CheckItem{}, SuggestedManifest: r.SuggestedManifest}
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
		UpdateAvailable: it.InstalledVersion != "" && it.LatestTag != "" && it.InstalledVersion != it.LatestTag,
	}
}

func toInstall(in store.Install) InstallInfo {
	return InstallInfo{Repo: in.FullName, Version: in.Version, InstalledAt: in.InstalledAt,
		ExecPath: in.ExecPath, DesktopPath: in.DesktopPath}
}

func toDetail(d *store.AppDetail) AppDetail {
	it := store.ListItem{App: d.App, Stars: d.Repo.Stars, LatestTag: d.Repo.LatestTag}
	if d.Install != nil {
		it.InstalledVersion = d.Install.Version
	}
	out := AppDetail{
		AppItem: toItem(it), Readme: d.Readme, Description: d.Repo.Description, License: d.Repo.License,
		Topics: nonNil(d.Repo.Topics), HTMLURL: d.Repo.HTMLURL, PushedAt: d.Repo.PushedAt,
		IndexedAt: d.Repo.IndexedAt, Assets: []AssetInfo{},
	}
	for _, a := range d.Assets {
		out.Assets = append(out.Assets, AssetInfo{Name: a.Name, Size: a.Size, Arch: a.Arch, Format: a.Format,
			Verified: a.Digest != "" || a.ChecksumURL != ""})
	}
	if d.Install != nil {
		ii := toInstall(*d.Install)
		out.Install = &ii
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
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
		return map[string]any{"protocol": ProtocolVersion, "name": "omastored"}, nil

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
		return s.jobs.start(s.ctx, KindIndex, "", func(ctx context.Context, report func(progress)) (any, error) {
			// Apps show up while the index runs: catalog.changed goes out when
			// repos were written, at most once per catalogInterval.
			var changed int
			var last time.Time
			st, err := b.Index(ctx, index.Options{Force: p.Force, Only: p.Repos, Progress: func(ip index.Progress) {
				report(progress{Stage: ip.Stage, Done: int64(ip.Done), Total: int64(ip.Total), Message: ip.Current})
				if n := ip.Updated + ip.Removed; n > changed && time.Since(last) >= s.catalogInterval {
					changed, last = n, time.Now()
					s.broadcast("catalog.changed", struct{}{})
				}
			}})
			return IndexResult{Updated: st.Updated, Refreshed: st.Refreshed, Unchanged: st.Unchanged,
				Removed: st.Removed, Skipped: st.Skipped, NotApps: st.NotApps, Failed: st.Failed}, err
		}, func(Job, error) {
			// Even a canceled index may have written repos.
			s.broadcast("catalog.changed", struct{}{})
		})

	case "install.start", "update.start":
		var p repoParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := p.validate(); err != nil {
			return nil, err
		}
		kind, op := KindInstall, b.Install
		if method == "update.start" {
			kind, op = KindUpdate, b.Update
		}
		return s.jobs.start(s.ctx, kind, p.Repo, func(ctx context.Context, report func(progress)) (any, error) {
			inst, err := op(ctx, p.Repo, func(ip install.Progress) {
				report(progress{Stage: ip.Stage, Done: ip.Done, Total: ip.Total})
			})
			if inst != nil {
				return toInstall(*inst), err
			}
			return nil, err
		}, func(_ Job, err error) {
			if err == nil {
				s.broadcast("catalog.changed", map[string]string{"repo": p.Repo})
			}
		})

	case "install.uninstall":
		var p repoParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := p.validate(); err != nil {
			return nil, err
		}
		// Cannot uninstall in the middle of an installation of the same app.
		for _, j := range s.jobs.list() {
			if j.State == StateRunning && j.Kind != KindIndex && strings.EqualFold(j.Repo, p.Repo) {
				return nil, ErrBusy
			}
		}
		if err := b.Uninstall(ctx, p.Repo); err != nil {
			return nil, err
		}
		s.broadcast("catalog.changed", map[string]string{"repo": p.Repo})
		return struct{}{}, nil

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
