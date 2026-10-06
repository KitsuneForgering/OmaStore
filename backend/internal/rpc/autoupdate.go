package rpc

import (
	"context"
	"time"

	"github.com/KitsuneForgering/OmaStore/backend/internal/index"
	"github.com/KitsuneForgering/OmaStore/backend/internal/install"
	"github.com/KitsuneForgering/OmaStore/backend/internal/store"
)

// AutoUpdater is implemented by backends with automatic updates. Without it,
// settings.* fail and nothing updates on its own.
type AutoUpdater interface {
	AutoUpdate(ctx context.Context) (bool, error)
	SetAutoUpdate(ctx context.Context, on bool) error
	AutoUpdates(ctx context.Context) (ready, waiting []store.ListItem, err error)
	AutoCheckDue(ctx context.Context, every time.Duration) (bool, error)
	MarkAutoCheck(ctx context.Context) error
	RequireProvenance(ctx context.Context) (bool, error)
	SetRequireProvenance(ctx context.Context, on bool) error
}

// Settings are the user's preferences (settings.get, settings.set).
type Settings struct {
	AutoUpdate        bool `json:"autoUpdate"`
	RequireProvenance bool `json:"requireProvenance"`
}

type settingsParams struct {
	AutoUpdate        *bool `json:"autoUpdate"`
	RequireProvenance *bool `json:"requireProvenance"`
}

func (s *Server) settings(ctx context.Context, raw []byte) (any, error) {
	au, ok := s.backend.(AutoUpdater)
	if !ok {
		return nil, &Error{Code: CodeMethodNotFound, Message: "settings are not available"}
	}
	if raw != nil {
		var p settingsParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if p.AutoUpdate != nil {
			if err := au.SetAutoUpdate(ctx, *p.AutoUpdate); err != nil {
				return nil, err
			}
			if *p.AutoUpdate {
				// Turning it on catches up with what is already waiting.
				go s.startAutoUpdates()
			}
		}
		if p.RequireProvenance != nil {
			if err := au.SetRequireProvenance(ctx, *p.RequireProvenance); err != nil {
				return nil, err
			}
		}
	}
	on, err := au.AutoUpdate(ctx)
	if err != nil {
		return nil, err
	}
	strict, err := au.RequireProvenance(ctx)
	if err != nil {
		return nil, err
	}
	return Settings{AutoUpdate: on, RequireProvenance: strict}, nil
}

// startIndex runs an index job; afterwards the catalog changed and, with
// automatic updates on, the verifiable updates start.
func (s *Server) startIndex(opts index.Options, after func(error)) (*Job, error) {
	b := s.backend
	return s.jobs.start(s.ctx, KindIndex, "", func(ctx context.Context, report func(progress)) (any, error) {
		// Apps show up while the index runs: catalog.changed goes out when
		// repos were written, at most once per catalogInterval.
		var changed int
		var last time.Time
		opts.Progress = func(ip index.Progress) {
			report(progress{Stage: ip.Stage, Done: int64(ip.Done), Total: int64(ip.Total), Message: ip.Current})
			if n := ip.Updated + ip.Removed; n > changed && time.Since(last) >= s.catalogInterval {
				changed, last = n, time.Now()
				s.broadcast("catalog.changed", struct{}{})
			}
		}
		st, err := b.Index(ctx, opts)
		if after != nil {
			after(err)
		}
		return IndexResult{Updated: st.Updated, Refreshed: st.Refreshed, Unchanged: st.Unchanged,
			Removed: st.Removed, Skipped: st.Skipped, NotApps: st.NotApps, Failed: st.Failed}, err
	}, func(_ Job, err error) {
		// Even a canceled index may have written repos.
		s.broadcast("catalog.changed", struct{}{})
		if err == nil {
			s.startAutoUpdates()
		}
	})
}

// startInstallJob runs install.start or update.start.
func (s *Server) startInstallJob(kind, repo string, allowUnverified bool) (*Job, error) {
	op := s.backend.Install
	if kind == KindUpdate {
		op = s.backend.Update
	}
	return s.jobs.start(s.ctx, kind, repo, func(ctx context.Context, report func(progress)) (any, error) {
		inst, err := op(ctx, repo, install.Options{AllowUnverified: allowUnverified, Progress: func(ip install.Progress) {
			report(progress{Stage: ip.Stage, Done: ip.Done, Total: ip.Total, Message: ip.Current})
		}})
		if inst != nil {
			return toInstall(*inst), err
		}
		return nil, err
	}, func(_ Job, err error) {
		if err == nil {
			s.broadcast("catalog.changed", map[string]string{"repo": repo})
		}
	})
}

// startAutoUpdates starts an update job for every installed app whose new
// release can be verified, when automatic updates are on. Apps already busy
// are left for the next time.
func (s *Server) startAutoUpdates() {
	au, ok := s.backend.(AutoUpdater)
	if !ok || s.ctx.Err() != nil {
		return
	}
	if on, err := au.AutoUpdate(s.ctx); err != nil || !on {
		return
	}
	ready, _, err := au.AutoUpdates(s.ctx)
	if err != nil {
		s.log.Warn("automatic updates: listing failed", "err", err)
		return
	}
	for _, it := range ready {
		if _, err := s.startInstallJob(KindUpdate, it.FullName, false); err != nil {
			s.log.Debug("automatic update not started", "repo", it.FullName, "err", err)
		}
	}
}

// AutoUpdateOnStart refreshes the installed apps and installs their updates
// when automatic updates are on and the last refresh is older than every, so
// opening the store keeps apps current even without the timer.
func (s *Server) AutoUpdateOnStart(every time.Duration) {
	au, ok := s.backend.(AutoUpdater)
	if !ok {
		return
	}
	if due, err := au.AutoCheckDue(s.ctx, every); err != nil || !due {
		return
	}
	insts, err := s.backend.ListInstalls(s.ctx)
	if err != nil || len(insts) == 0 {
		return
	}
	names := make([]string, 0, len(insts))
	for _, in := range insts {
		names = append(names, in.FullName)
	}
	if _, err := s.startIndex(index.Options{Only: names}, func(err error) {
		if err == nil {
			if err := au.MarkAutoCheck(s.ctx); err != nil {
				s.log.Warn("automatic updates: could not record the check", "err", err)
			}
		}
	}); err != nil {
		s.log.Debug("automatic update check not started", "err", err)
	}
}
