// Package store guarda o índice e as instalações do OmaStore em SQLite.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ErrNotFound é retornado quando o registro pedido não existe.
var ErrNotFound = errors.New("não encontrado")

// Store é o acesso ao banco SQLite.
type Store struct {
	db *sql.DB
}

// Open abre (criando se necessário) o banco em path e aplica as migrações.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("criar diretório do banco: %w", err)
	}
	q := url.Values{}
	q.Set("_journal_mode", "WAL")
	q.Set("_foreign_keys", "on")
	q.Set("_busy_timeout", "5000")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite3", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("abrir banco: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("abrir banco %s: %w", path, err)
	}
	if err := migrate(ctx, db, migrationsFS); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close fecha o banco.
func (s *Store) Close() error { return s.db.Close() }

// Repo são os metadados de um repositório do GitHub.
type Repo struct {
	FullName      string
	Description   string
	Stars         int
	Topics        []string
	License       string
	HTMLURL       string
	DefaultBranch string
	PushedAt      time.Time
	HeadSHA       string
	LatestTag     string
	ETag          string
	IndexedAt     time.Time
	IndexVersion  int
}

// RepoState é o que a verificação de cache compara para decidir se um
// repositório precisa ser reprocessado.
type RepoState struct {
	PushedAt     time.Time
	HeadSHA      string
	LatestTag    string
	ETag         string
	IndexVersion int
	Stars        int
	Description  string
}

// App são os dados de exibição derivados de um repositório.
type App struct {
	FullName    string
	Name        string
	Summary     string
	Readme      string
	IconURL     string
	Screenshots []string
	Category    string
	Score       float64
	Installable bool
	// Manifest é o omastore.toml validado, em JSON (ver internal/manifest).
	Manifest string
}

// Asset é um arquivo de uma release.
type Asset struct {
	Tag         string
	Name        string
	URL         string
	Size        int64
	Arch        string
	Format      string
	Digest      string // "sha256:<hex>" quando a API informa
	ChecksumURL string
}

// Install é um app instalado.
type Install struct {
	FullName    string
	Version     string
	InstalledAt time.Time
	ExecPath    string
	DesktopPath string
	Files       []string
}

// AppDetail junta tudo o que se sabe sobre um app.
type AppDetail struct {
	App
	Repo    Repo
	Assets  []Asset
	Install *Install
}

// ListItem é uma linha do catálogo.
type ListItem struct {
	App
	Stars            int
	LatestTag        string
	InstalledVersion string
}

// Filter restringe ListApps.
type Filter struct {
	Category      string
	Query         string // busca em nome, resumo e full_name
	InstalledOnly bool
	All           bool // inclui apps não instaláveis
	Limit         int
	Offset        int
}

// CategoryCount é uma categoria e quantos apps ela tem.
type CategoryCount struct {
	Category string
	Count    int
}

func encodeList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func decodeList(s string) []string {
	var v []string
	if err := json.Unmarshal([]byte(s), &v); err != nil || v == nil {
		return []string{}
	}
	return v
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

func timeOf(n sql.NullTime) time.Time {
	if !n.Valid {
		return time.Time{}
	}
	return n.Time
}

// RepoState retorna o estado gravado de um repositório, ou ErrNotFound.
func (s *Store) RepoState(ctx context.Context, fullName string) (RepoState, error) {
	var st RepoState
	var pushed sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT pushed_at, head_sha, latest_tag, etag, index_version, stars, description
		 FROM repos WHERE full_name = ?`, fullName).
		Scan(&pushed, &st.HeadSHA, &st.LatestTag, &st.ETag, &st.IndexVersion, &st.Stars, &st.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return st, ErrNotFound
	}
	if err != nil {
		return st, fmt.Errorf("ler estado de %s: %w", fullName, err)
	}
	st.PushedAt = timeOf(pushed)
	return st, nil
}

// TouchRepo marca um repositório como verificado sem reprocessá-lo.
func (s *Store) TouchRepo(ctx context.Context, fullName string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE repos SET indexed_at = ? WHERE full_name = ?`, at.UTC(), fullName)
	if err != nil {
		return fmt.Errorf("atualizar indexed_at de %s: %w", fullName, err)
	}
	return nil
}

// SaveIndexed grava repo, app e assets de uma vez, numa única transação.
// Os assets antigos do repositório são substituídos.
func (s *Store) SaveIndexed(ctx context.Context, r Repo, a App, assets []Asset) (err error) {
	if a.FullName == "" {
		a.FullName = r.FullName
	}
	if a.FullName != r.FullName {
		return fmt.Errorf("app %q não corresponde ao repo %q", a.FullName, r.FullName)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("iniciar transação: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO repos (full_name, description, stars, topics, license, html_url, default_branch,
		                   pushed_at, head_sha, latest_tag, etag, indexed_at, index_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(full_name) DO UPDATE SET
			description = excluded.description, stars = excluded.stars, topics = excluded.topics,
			license = excluded.license, html_url = excluded.html_url,
			default_branch = excluded.default_branch, pushed_at = excluded.pushed_at,
			head_sha = excluded.head_sha, latest_tag = excluded.latest_tag, etag = excluded.etag,
			indexed_at = excluded.indexed_at, index_version = excluded.index_version`,
		r.FullName, r.Description, r.Stars, encodeList(r.Topics), r.License, r.HTMLURL, r.DefaultBranch,
		nullTime(r.PushedAt), r.HeadSHA, r.LatestTag, r.ETag, nullTime(r.IndexedAt), r.IndexVersion); err != nil {
		return fmt.Errorf("gravar repo %s: %w", r.FullName, err)
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO apps (full_name, name, summary, readme, icon_url, screenshots, category, score, installable, manifest)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(full_name) DO UPDATE SET
			name = excluded.name, summary = excluded.summary, readme = excluded.readme,
			icon_url = excluded.icon_url, screenshots = excluded.screenshots,
			category = excluded.category, score = excluded.score, installable = excluded.installable,
			manifest = excluded.manifest`,
		a.FullName, a.Name, a.Summary, a.Readme, a.IconURL, encodeList(a.Screenshots),
		a.Category, a.Score, a.Installable, a.Manifest); err != nil {
		return fmt.Errorf("gravar app %s: %w", a.FullName, err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM assets WHERE full_name = ?`, r.FullName); err != nil {
		return fmt.Errorf("limpar assets de %s: %w", r.FullName, err)
	}
	for _, as := range assets {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO assets (full_name, tag, name, url, size, arch, format, digest, checksum_url)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.FullName, as.Tag, as.Name, as.URL, as.Size, as.Arch, as.Format, as.Digest, as.ChecksumURL); err != nil {
			return fmt.Errorf("gravar asset %s de %s: %w", as.Name, r.FullName, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit de %s: %w", r.FullName, err)
	}
	return nil
}

// RemoveRepo apaga um repositório (e, em cascata, app e assets) do catálogo.
func (s *Store) RemoveRepo(ctx context.Context, fullName string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM repos WHERE full_name = ?`, fullName); err != nil {
		return fmt.Errorf("remover %s: %w", fullName, err)
	}
	return nil
}

// ListApps lista o catálogo ordenado por score (e nome como desempate).
func (s *Store) ListApps(ctx context.Context, f Filter) ([]ListItem, error) {
	var where []string
	var args []any
	if !f.All {
		where = append(where, "a.installable = 1")
	}
	if f.Category != "" {
		where = append(where, "a.category = ?")
		args = append(args, f.Category)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + escapeLike(strings.ToLower(q)) + "%"
		where = append(where, `(lower(a.name) LIKE ? ESCAPE '\' OR lower(a.summary) LIKE ? ESCAPE '\' OR lower(a.full_name) LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like)
	}
	if f.InstalledOnly {
		where = append(where, "i.full_name IS NOT NULL")
	}
	query := `
		SELECT a.full_name, a.name, a.summary, a.icon_url, a.screenshots, a.category, a.score, a.installable,
		       r.stars, r.latest_tag, COALESCE(i.version, '')
		FROM apps a
		JOIN repos r ON r.full_name = a.full_name
		LEFT JOIN installs i ON i.full_name = a.full_name`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY a.score DESC, lower(a.name)"
	if f.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, f.Limit, f.Offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listar apps: %w", err)
	}
	defer rows.Close()
	items := []ListItem{}
	for rows.Next() {
		var it ListItem
		var shots string
		if err := rows.Scan(&it.FullName, &it.Name, &it.Summary, &it.IconURL, &shots, &it.Category,
			&it.Score, &it.Installable, &it.Stars, &it.LatestTag, &it.InstalledVersion); err != nil {
			return nil, fmt.Errorf("ler app: %w", err)
		}
		it.Screenshots = decodeList(shots)
		items = append(items, it)
	}
	return items, rows.Err()
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// Categories lista as categorias com apps instaláveis.
func (s *Store) Categories(ctx context.Context) ([]CategoryCount, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT category, COUNT(*) FROM apps WHERE installable = 1 GROUP BY category ORDER BY category`)
	if err != nil {
		return nil, fmt.Errorf("listar categorias: %w", err)
	}
	defer rows.Close()
	out := []CategoryCount{}
	for rows.Next() {
		var c CategoryCount
		if err := rows.Scan(&c.Category, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetApp retorna o detalhe de um app, ou ErrNotFound.
func (s *Store) GetApp(ctx context.Context, fullName string) (*AppDetail, error) {
	var d AppDetail
	var shots, topics string
	var pushed, indexed sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT a.full_name, a.name, a.summary, a.readme, a.icon_url, a.screenshots, a.category, a.score,
		       a.installable, a.manifest, r.description, r.stars, r.topics, r.license, r.html_url, r.default_branch,
		       r.pushed_at, r.head_sha, r.latest_tag, r.etag, r.indexed_at
		FROM apps a JOIN repos r ON r.full_name = a.full_name
		WHERE a.full_name = ?`, fullName).
		Scan(&d.FullName, &d.Name, &d.Summary, &d.Readme, &d.IconURL, &shots, &d.Category, &d.Score,
			&d.Installable, &d.Manifest, &d.Repo.Description, &d.Repo.Stars, &topics, &d.Repo.License, &d.Repo.HTMLURL,
			&d.Repo.DefaultBranch, &pushed, &d.Repo.HeadSHA, &d.Repo.LatestTag, &d.Repo.ETag, &indexed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ler app %s: %w", fullName, err)
	}
	d.Screenshots = decodeList(shots)
	d.Repo.FullName = d.FullName
	d.Repo.Topics = decodeList(topics)
	d.Repo.PushedAt = timeOf(pushed)
	d.Repo.IndexedAt = timeOf(indexed)

	d.Assets, err = s.Assets(ctx, fullName, d.Repo.LatestTag)
	if err != nil {
		return nil, err
	}
	inst, err := s.GetInstall(ctx, fullName)
	switch {
	case err == nil:
		d.Install = inst
	case !errors.Is(err, ErrNotFound):
		return nil, err
	}
	return &d, nil
}

// Assets lista os assets de uma release.
func (s *Store) Assets(ctx context.Context, fullName, tag string) ([]Asset, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tag, name, url, size, arch, format, digest, checksum_url
		FROM assets WHERE full_name = ? AND tag = ? ORDER BY name`, fullName, tag)
	if err != nil {
		return nil, fmt.Errorf("listar assets de %s: %w", fullName, err)
	}
	defer rows.Close()
	out := []Asset{}
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.Tag, &a.Name, &a.URL, &a.Size, &a.Arch, &a.Format, &a.Digest, &a.ChecksumURL); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SaveInstall registra (ou substitui) a instalação de um app.
func (s *Store) SaveInstall(ctx context.Context, in Install) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO installs (full_name, version, installed_at, exec_path, desktop_path, files)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(full_name) DO UPDATE SET
			version = excluded.version, installed_at = excluded.installed_at,
			exec_path = excluded.exec_path, desktop_path = excluded.desktop_path, files = excluded.files`,
		in.FullName, in.Version, in.InstalledAt.UTC(), in.ExecPath, in.DesktopPath, encodeList(in.Files))
	if err != nil {
		return fmt.Errorf("registrar instalação de %s: %w", in.FullName, err)
	}
	return nil
}

// GetInstall retorna a instalação de um app, ou ErrNotFound.
func (s *Store) GetInstall(ctx context.Context, fullName string) (*Install, error) {
	var in Install
	var files string
	err := s.db.QueryRowContext(ctx, `
		SELECT full_name, version, installed_at, exec_path, desktop_path, files
		FROM installs WHERE full_name = ?`, fullName).
		Scan(&in.FullName, &in.Version, &in.InstalledAt, &in.ExecPath, &in.DesktopPath, &files)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ler instalação de %s: %w", fullName, err)
	}
	in.Files = decodeList(files)
	return &in, nil
}

// ListInstalls lista os apps instalados por nome.
func (s *Store) ListInstalls(ctx context.Context) ([]Install, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT full_name, version, installed_at, exec_path, desktop_path, files
		FROM installs ORDER BY full_name`)
	if err != nil {
		return nil, fmt.Errorf("listar instalações: %w", err)
	}
	defer rows.Close()
	out := []Install{}
	for rows.Next() {
		var in Install
		var files string
		if err := rows.Scan(&in.FullName, &in.Version, &in.InstalledAt, &in.ExecPath, &in.DesktopPath, &files); err != nil {
			return nil, err
		}
		in.Files = decodeList(files)
		out = append(out, in)
	}
	return out, rows.Err()
}

// DeleteInstall remove o registro de instalação de um app.
func (s *Store) DeleteInstall(ctx context.Context, fullName string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM installs WHERE full_name = ?`, fullName); err != nil {
		return fmt.Errorf("remover instalação de %s: %w", fullName, err)
	}
	return nil
}

// UpdateStats atualiza os dados voláteis de um repositório (stars, descrição,
// topics, ETag e score) sem reprocessá-lo.
func (s *Store) UpdateStats(ctx context.Context, fullName string, stars int, description string,
	topics []string, etag string, score float64, at time.Time) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("iniciar transação: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, `
		UPDATE repos SET stars = ?, description = ?, topics = ?, etag = ?, indexed_at = ?
		WHERE full_name = ?`, stars, description, encodeList(topics), etag, at.UTC(), fullName); err != nil {
		return fmt.Errorf("atualizar repo %s: %w", fullName, err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE apps SET score = ? WHERE full_name = ?`, score, fullName); err != nil {
		return fmt.Errorf("atualizar score de %s: %w", fullName, err)
	}
	return tx.Commit()
}

// RepoNames lista todos os repositórios do catálogo.
func (s *Store) RepoNames(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT full_name FROM repos ORDER BY full_name`)
	if err != nil {
		return nil, fmt.Errorf("listar repos: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// SearchDoc são os dados de um app usados pela busca.
type SearchDoc struct {
	FullName string
	Name     string
	Summary  string
	Readme   string
	Topics   []string
	Category string
	Stars    int
}

// SearchDocs lista todos os apps (instaláveis ou não) com os campos da busca.
func (s *Store) SearchDocs(ctx context.Context) ([]SearchDoc, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.full_name, a.name, a.summary, a.readme, r.topics, a.category, r.stars
		FROM apps a JOIN repos r ON r.full_name = a.full_name`)
	if err != nil {
		return nil, fmt.Errorf("ler documentos de busca: %w", err)
	}
	defer rows.Close()
	var out []SearchDoc
	for rows.Next() {
		var d SearchDoc
		var topics string
		if err := rows.Scan(&d.FullName, &d.Name, &d.Summary, &d.Readme, &topics, &d.Category, &d.Stars); err != nil {
			return nil, err
		}
		d.Topics = decodeList(topics)
		out = append(out, d)
	}
	return out, rows.Err()
}

// CatalogStamp muda sempre que o catálogo muda (repo gravado, atualizado ou
// removido), inclusive por outro processo (ex.: a CLI com o daemon rodando).
func (s *Store) CatalogStamp(ctx context.Context) (string, error) {
	var n int
	var last sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), MAX(indexed_at) FROM repos`).Scan(&n, &last)
	if err != nil {
		return "", fmt.Errorf("ler marca do catálogo: %w", err)
	}
	return fmt.Sprintf("%d|%s", n, last.String), nil
}
