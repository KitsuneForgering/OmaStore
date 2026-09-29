package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"syscall"
	"time"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/install"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
)

func umask(m int) int { return syscall.Umask(m) }

// Backend são as operações que o servidor expõe.
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
}

// DTOs: o formato JSON exposto ao frontend, estável e em camelCase,
// independente das structs internas.

// AppItem é uma linha do catálogo.
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

// AssetInfo é um asset de release.
type AssetInfo struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Arch     string `json:"arch"`
	Format   string `json:"format"`
	Verified bool   `json:"verified"` // há digest ou arquivo de checksum
}

// InstallInfo é um app instalado.
type InstallInfo struct {
	Repo        string    `json:"repo"`
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installedAt"`
	ExecPath    string    `json:"execPath"`
	DesktopPath string    `json:"desktopPath"`
}

// AppDetail é o detalhe de um app.
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

// IndexResult é o resultado de um job de indexação.
type IndexResult struct {
	Updated   int `json:"updated"`
	Refreshed int `json:"refreshed"`
	Unchanged int `json:"unchanged"`
	Removed   int `json:"removed"`
	Skipped   int `json:"skipped"`
	NotApps   int `json:"notApps"`
	Failed    int `json:"failed"`
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

// Parâmetros dos métodos.
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
)

// decode lê params no destino; ausência de params equivale a {}.
func decode(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errInvalidParams("parâmetros inválidos: %v", err)
	}
	return nil
}

func (p repoParams) validate() error {
	owner, repo, ok := strings.Cut(p.Repo, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return errInvalidParams(`"repo" deve ser "owner/repo", recebido %q`, p.Repo)
	}
	return nil
}

// callTimeout limita chamadas síncronas (as longas viram jobs).
const callTimeout = 2 * time.Minute

// call despacha um método.
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
			return nil, errInvalidParams("limit/offset negativos")
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
			st, err := b.Index(ctx, index.Options{Force: p.Force, Only: p.Repos, Progress: func(ip index.Progress) {
				report(progress{Stage: "index", Done: int64(ip.Done), Total: int64(ip.Total), Message: ip.Current})
			}})
			return IndexResult{Updated: st.Updated, Refreshed: st.Refreshed, Unchanged: st.Unchanged,
				Removed: st.Removed, Skipped: st.Skipped, NotApps: st.NotApps, Failed: st.Failed}, err
		}, func(Job, error) {
			// Mesmo um índice cancelado pode ter gravado repos.
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
		// Não pode desinstalar no meio de uma instalação do mesmo app.
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
	return nil, &Error{Code: CodeMethodNotFound, Message: "método desconhecido: " + method}
}
