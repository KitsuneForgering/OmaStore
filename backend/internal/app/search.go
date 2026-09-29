package app

import (
	"context"
	"strings"
	"sync"

	"github.com/KitsuneSemCalda/OmaStore/backend/internal/search"
	"github.com/KitsuneSemCalda/OmaStore/backend/internal/store"
)

// searchCache guarda o índice de busca e o reconstrói quando a marca do
// catálogo muda.
type searchCache struct {
	mu    sync.Mutex
	stamp string
	ix    *search.Index
}

func (a *App) searchIndex(ctx context.Context) (*search.Index, error) {
	stamp, err := a.Store.CatalogStamp(ctx)
	if err != nil {
		return nil, err
	}
	a.search.mu.Lock()
	defer a.search.mu.Unlock()
	if a.search.ix != nil && a.search.stamp == stamp {
		return a.search.ix, nil
	}
	docs, err := a.Store.SearchDocs(ctx)
	if err != nil {
		return nil, err
	}
	sd := make([]search.Doc, len(docs))
	for i, d := range docs {
		sd[i] = search.Doc{Repo: d.FullName, Name: d.Name, Summary: d.Summary, Readme: d.Readme,
			Topics: d.Topics, Category: d.Category, Stars: d.Stars}
	}
	a.search.ix = search.Build(sd)
	a.search.stamp = stamp
	return a.search.ix, nil
}

// ListApps lista o catálogo. Com f.Query, a ordem vem do ranking da busca
// (e não do score de popularidade); os demais filtros continuam valendo.
func (a *App) ListApps(ctx context.Context, f store.Filter) ([]store.ListItem, error) {
	q := strings.TrimSpace(f.Query)
	if q == "" {
		return a.Store.ListApps(ctx, f)
	}
	ix, err := a.searchIndex(ctx)
	if err != nil {
		return nil, err
	}
	ranked := ix.Search(q, 0)
	if len(ranked) == 0 {
		return []store.ListItem{}, nil
	}
	base := f
	base.Query, base.Limit, base.Offset = "", 0, 0
	return a.pick(ctx, base, ranked, f.Offset, f.Limit)
}

// Similar retorna os apps mais parecidos com fullName, respeitando o filtro
// (por padrão, só instaláveis).
func (a *App) Similar(ctx context.Context, fullName string, limit int) ([]store.ListItem, error) {
	if _, err := a.Store.GetApp(ctx, fullName); err != nil {
		return nil, err
	}
	ix, err := a.searchIndex(ctx)
	if err != nil {
		return nil, err
	}
	return a.pick(ctx, store.Filter{}, ix.Similar(fullName, 0), 0, limit)
}

// pick devolve, na ordem de ranked, os itens que passam no filtro.
func (a *App) pick(ctx context.Context, f store.Filter, ranked []search.Result, offset, limit int) ([]store.ListItem, error) {
	items, err := a.Store.ListApps(ctx, f)
	if err != nil {
		return nil, err
	}
	byRepo := make(map[string]store.ListItem, len(items))
	for _, it := range items {
		byRepo[it.FullName] = it
	}
	out := []store.ListItem{}
	for _, r := range ranked {
		it, ok := byRepo[r.Repo]
		if !ok {
			continue
		}
		if offset > 0 {
			offset--
			continue
		}
		out = append(out, it)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}
