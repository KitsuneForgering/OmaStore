// Package app monta as dependências do backend (caminhos, banco, cliente do
// GitHub, indexador e instalador), compartilhadas pela CLI e pelo daemon.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/github"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/gitrepo"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/imagecache"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/install"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/xdg"
)

// App agrupa os serviços do backend.
type App struct {
	Paths     xdg.Paths
	Store     *store.Store
	GitHub    *github.Client
	Indexer   *index.Indexer
	Installer *install.Installer
	Images    *imagecache.Cache
	Log       *slog.Logger

	search searchCache
}

// Open resolve os caminhos XDG, abre o banco e cria os serviços.
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
		log.Info("sem token do GitHub; usando acesso anônimo (limite de 60 req/h)")
	}
	gh, err := github.New(github.Options{Token: token})
	if err != nil {
		st.Close()
		return nil, err
	}
	inst, err := install.New(st, paths)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("instalador: %w", err)
	}
	inst.Log = log
	return &App{
		Paths:  paths,
		Store:  st,
		GitHub: gh,
		Indexer: &index.Indexer{
			GH:    gh,
			Store: st,
			Repos: &gitrepo.Cache{Dir: paths.ReposDir},
			Log:   log,
		},
		Installer: inst,
		Images:    &imagecache.Cache{Dir: paths.ImagesDir},
		Log:       log,
	}, nil
}

// Close libera os recursos.
func (a *App) Close() error { return a.Store.Close() }
