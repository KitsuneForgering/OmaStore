// Package app wires the backend dependencies (paths, database, GitHub client,
// indexer and installer), shared by the CLI and the daemon.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/github"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/gitrepo"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/imagecache"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/install"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/sysdeps"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/xdg"
)

// App groups the backend services.
type App struct {
	Paths     xdg.Paths
	Store     *store.Store
	GitHub    *github.Client
	Indexer   *index.Indexer
	Installer *install.Installer
	Images    *imagecache.Cache
	Pacman    *sysdeps.Pacman
	Log       *slog.Logger

	search searchCache
}

// Open resolves the XDG paths, opens the database and creates the services.
func Open(ctx context.Context, log *slog.Logger) (*App, error) {
	if log == nil {
		log = slog.Default()
	}
	paths, err := xdg.Resolve()
	if err != nil {
		return nil, err
	}
	if err := paths.Ensure(); err != nil {
		return nil, err
	}
	st, err := store.Open(ctx, paths.DB)
	if err != nil {
		return nil, err
	}
	token := github.TokenFromEnv(ctx)
	if token == "" {
		log.Info("no GitHub token; using anonymous access (limit of 60 req/h)")
	}
	gh, err := github.New(github.Options{Token: token})
	if err != nil {
		st.Close()
		return nil, err
	}
	inst, err := install.New(st, paths)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("installer: %w", err)
	}
	inst.Log = log
	return &App{
		Paths:  paths,
		Store:  st,
		GitHub: gh,
		Indexer: &index.Indexer{
			GH:       gh,
			Store:    st,
			Repos:    &gitrepo.Cache{Dir: paths.ReposDir},
			LockPath: filepath.Join(paths.DataDir, "index.lock"),
			Log:      log,
		},
		Installer: inst,
		Images:    &imagecache.Cache{Dir: paths.ImagesDir},
		Pacman:    &sysdeps.Pacman{},
		Log:       log,
	}, nil
}

// Close releases the resources.
func (a *App) Close() error { return a.Store.Close() }
