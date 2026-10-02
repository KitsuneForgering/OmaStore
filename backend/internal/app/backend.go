package app

import (
	"context"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/index"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/install"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
)

// Methods that make *App an rpc.Backend.

func (a *App) GetApp(ctx context.Context, fullName string) (*store.AppDetail, error) {
	return a.Store.GetApp(ctx, fullName)
}

func (a *App) Categories(ctx context.Context) ([]store.CategoryCount, error) {
	return a.Store.Categories(ctx)
}

func (a *App) ListInstalls(ctx context.Context) ([]store.Install, error) {
	return a.Store.ListInstalls(ctx)
}

func (a *App) Index(ctx context.Context, opts index.Options) (index.Stats, error) {
	return a.Indexer.Run(ctx, opts)
}

func (a *App) Install(ctx context.Context, fullName string, opts install.Options) (*store.Install, error) {
	return a.Installer.Install(ctx, fullName, opts)
}

func (a *App) Update(ctx context.Context, fullName string, opts install.Options) (*store.Install, error) {
	return a.Installer.Update(ctx, fullName, opts)
}

// Uninstall removes the app; while it runs, only with force.
func (a *App) Uninstall(ctx context.Context, fullName string, force bool) error {
	return a.Installer.Uninstall(ctx, fullName, force)
}

// Rollback goes back to the version the last update replaced.
func (a *App) Rollback(ctx context.Context, fullName string) (*store.Install, error) {
	return a.Installer.Rollback(ctx, fullName)
}

// Check diagnoses a repository for its author without touching the catalog.
func (a *App) Check(ctx context.Context, fullName string, manifest *string) (*index.Report, error) {
	return a.Indexer.Check(ctx, fullName, manifest)
}

func (a *App) Image(ctx context.Context, url string) (string, error) {
	return a.Images.Get(ctx, url)
}

// ChangeStamp changes whenever the catalog or the installations change, even
// when another process (the CLI or the index timer) made the change.
func (a *App) ChangeStamp(ctx context.Context) (string, error) {
	c, err := a.Store.CatalogStamp(ctx)
	if err != nil {
		return "", err
	}
	i, err := a.Store.InstallsStamp(ctx)
	if err != nil {
		return "", err
	}
	return c + "#" + i, nil
}
