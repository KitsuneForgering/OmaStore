# TODO — OmaStore

Implementation roadmap, in the order in which the pieces depend on each other.
Each phase must end with `go test ./...` (or `ctest`) passing.

## Phase 0 — Foundation

- [x] `backend/go.mod` (module, Go version) with `go-github`, `go-git/v5`, `go-sqlite3`
- [x] Backend directory structure as in CLAUDE.md
- [x] `internal/xdg` package: resolve `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_RUNTIME_DIR` with the specification defaults
- [x] Structured logging (`log/slog`) with configurable level
- [x] Minimal `frontend/CMakeLists.txt` (Qt 6 Quick, C++20, empty window opening) — *local build pending: cmake/ninja not installed*
- [x] Root `Makefile` (build, tests, vet, fmt, run, clean)
- [x] CI (GitHub Actions) calling the `Makefile` targets
- [x] Update `.gitignore` (`frontend/build/`, Go binaries)

## Phase 1 — Storage (`internal/store`)

- [x] Open SQLite with WAL, `foreign_keys=ON`, `busy_timeout`
- [x] Numbered migration runner (`schema_migrations` table, migrations embedded via `embed`)
- [x] `0001_init.sql`: tables `repos`, `apps`, `assets`, `installs` (+ indexes)
- [x] Data repository: upsert of repo/app/assets in a single transaction
- [x] Queries: list catalog (filter by category, text search, order by score), get app, list installed
- [x] Tests with a database in a temporary file

## Phase 2 — GitHub (`internal/github`)

- [x] Client authenticated by `GITHUB_TOKEN`, falling back to `gh auth token`, and anonymous if neither exists
- [x] Search by topic (`topic:omarchy`) with pagination
- [x] Curated list of seed repositories (embedded file, `internal/index/seeds.txt`)
- [x] Repo metadata: stars, topics, `pushed_at`, license, HEAD SHA
- [x] Latest release + assets (includes the sha256 `digest` the API already provides per asset)
- [x] README through the API (raw content)
- [x] Conditional requests with ETag / `If-None-Match` (store the ETag in `repos.etag`)
- [x] Rate limit handling (read headers, wait/abort with a clear error)
- [x] Tests with `httptest` + JSON fixtures in `testdata/`

## Phase 3 — Shallow clone (`internal/gitrepo`)

- [x] `Depth: 1` clone into `$XDG_CACHE_HOME/omastore/repos/<owner>__<repo>`
- [x] Update an existing clone (shallow fetch) instead of re-cloning
- [x] Locate the icon (conventions: `icon.png`, `assets/icon.*`, `*.svg` at the root, etc.)
- [x] Locate screenshots (`screenshots/`, `preview`, `demo`; README images are handled by the indexer)
- [x] Icon/screenshot lookup as a pure function over a list of paths; the indexer uses the Trees API and only clones if the tree comes back truncated
- [x] Tests with a local git repository created in the test itself

## Phase 4 — Indexer (`internal/index`)

- [x] Discovery: union of topic search + seeds, without duplicates
- [x] **Cache check**: compare `pushed_at`, `head_sha` and `latest_tag` with SQLite; skip if nothing changed
- [x] Filter out repos without a release with a Linux binary (not installable)
- [x] README extraction: short summary (first paragraph) + long description
- [x] Resolve relative README image URLs into absolute URLs (raw.githubusercontent)
- [x] Classification: topic → category map (e.g. `photo` → Graphics, `vm` → System)
- [x] Score: stars with recency (`pushed_at`) as tiebreaker
- [x] Asset arch/format detection (`x86_64`/`amd64`, `aarch64`/`arm64`; binary, `.tar.gz`/`.xz`/`.bz2`/`.zst`, `.zip`, AppImage, `.pkg.tar.zst`; discards `.deb`/`.rpm`, sources and other OSes)
- [x] Detect checksum asset (`*.sha256`, `checksums.txt`)
- [x] Transactional persistence + recording of processed SHA/tag
- [x] `index.Version` (migration 0002): changing indexer rules reprocesses old repos
- [x] Bounded concurrency (worker pool) and cancellation via `context`
- [x] Light update when only stars/description change (no README/tree)
- [x] Archived, removed (404) or renamed repos leave the catalog; optional `Prune`
- [x] Tests: unchanged repo is not reprocessed; new repo is indexed; repo without a binary is excluded

## Phase 5 — Installer (`internal/install`)

- [x] Asset selection by the machine architecture (`runtime.GOARCH`)
- [x] Download to a temporary directory with progress (bytes callback)
- [x] Checksum verification: the API `digest`, otherwise `*.sha256`/`checksums.txt` (GNU and BSD formats, sha256/sha512); fail on mismatch
- [x] Extraction of `.tar.gz`/`.xz`/`.bz2`/`.zst`, `.zip` and `.pkg.tar.zst` (only `usr/`, no install scripts) with protection against zip slip and symlinks leaving the destination
- [x] Install into `$XDG_DATA_HOME/omastore/apps/<owner>__<repo>/<version>/`
- [x] Identify the main executable (repo name, executable bit, single binary)
- [x] `chmod +x` and link in `~/.local/bin/`
- [x] Icon in `~/.local/share/icons/hicolor/<size>/apps/` (resize, or use `scalable/` for SVG)
- [x] Generate `omastore-<owner>-<repo>.desktop` with correct escaping of all fields
- [x] Run `update-desktop-database` / `gtk-update-icon-cache` if available (without failing if missing)
- [x] Record the installation in `installs` with the list of created files
- [x] Rollback if any step fails midway
- [x] Uninstallation: remove only registered paths
- [x] Update: install the new version, switch the link, remove the old version
- [x] Guarantee: never execute the binary, nothing outside `$HOME`, no `sudo` (system hooks by absolute path, never through `PATH`)
- [x] Never overwrite third-party files in `~/.local/bin` or `applications/` (`ErrConflict`)
- [x] Refuse launchers that would shadow system commands (`/usr/bin/<cmd>` exists) or OmaStore's own
- [x] `.desktop` validated with `desktop-file-validate` in the tests (when available)
- [x] Tests: zip slip, malicious symlink, `.desktop` escaping, invalid checksum, rollback
- [x] The launcher in `~/.local/bin` is an `exec` script (not a symlink): apps that use `dirname "$0"` broke through a link

## Phase 6 — Debug CLI (`cmd/omastore`)

- [x] `omastore index [--force] [--prune] [--max N] [owner/repo...]`
- [x] `omastore list [--category X] [--query Q] [--installed] [--all] [--json]` and `omastore categories`
- [x] `omastore show [--json] <owner/repo>`
- [x] `omastore install <owner/repo>`
- [x] `omastore uninstall <owner/repo>`
- [x] `omastore update [<owner/repo>]`
- [x] Services wired in `internal/app` (shared with the daemon)
- [x] Tested against the real GitHub (isolated HOME): 34 repos indexed, 2nd run without reprocessing; omadesign and rawmakase installed and removed

## Phase 7 — Daemon and IPC (`internal/rpc`, `cmd/omastored`)

- [x] JSON-RPC 2.0 server (NDJSON) on `$XDG_RUNTIME_DIR/omastore.sock` (mode `0600`, removes orphaned socket, refuses a 2nd daemon, clear error for paths > 107 bytes)
- [x] Methods documented in `docs/ipc.md`: `daemon.hello`, `catalog.list/get/categories`, `installs.list`, `index.start`, `install.start`, `update.start`, `install.uninstall`, `jobs.list/cancel`, `image.get`
- [x] Job manager (id, state, progress, cancellation)
- [x] Notifications (`job.started/progress/done/failed`, `catalog.changed` always after the job ends); progress limited to ~10/s
- [x] One indexing job at a time; installations of the same app serialized
- [x] Clean shutdown (SIGTERM, cancel jobs, close DB)
- [x] `--user` systemd units with socket activation (`packaging/systemd/`)
- [x] Server tests with an in-memory client
- [x] Stable camelCase DTOs, separate from the internal structs; backend errors mapped to fixed codes
- [x] Image cache (`internal/imagecache`, `image.get` method): https only, content validated, concurrent downloads shared
- [x] Tested with the real daemon (Python client over the socket): hello, catalog, image, index job, SIGTERM removes the socket

## Phase 8 — Frontend (`frontend/`)

- [x] C++ IPC client (`QLocalSocket`, JSON-RPC framing, reconnection)
- [x] Start `omastored` if the socket does not exist (never through `PATH`: `$OMASTORED`, app directory, `/usr/bin`)
- [x] `QAbstractListModel` models: catalog, installed, jobs
- [x] Catalog screen: grid with icon, name, summary, stars; search and category filter
- [x] Detail screen: long description, screenshots, license, version, Install/Remove/Update button
- [x] Progress bar bound to job notifications
- [x] Installed screen
- [x] Remote image cache (through the backend: `image.get`, done in Phase 7)
- [x] Active theme colors (`~/.local/state/omarchy/current/theme/colors.toml`, with fallback) and reload on theme change
- [x] Tests with `ctest` (models + IPC client against a fake server)
- [x] Frontend without network: the QML `QNetworkAccessManager` blocks remote URLs; README shown without images; images through `image://omastore/`
- [x] First run with an empty catalog triggers `index.start`; confirmation before installing a release without checksum
- [x] Shortcuts: `/` search, `Esc` back, `Ctrl+R` refresh catalog; `--open owner/repo` on the command line
- [x] Verified against the real daemon (`--screenshot` in offscreen mode)

## Phase 9 — Packaging and launch

- [x] `PKGBUILD` (`packaging/arch/`, package `omastore-git`) with PIE/trimpath build, `check()` and `make install DESTDIR`
- [x] OmaStore's own `.desktop` entry
- [x] README with screenshots, installation and how to publish a compatible app
- [x] Guide for app authors (`docs/authors.md`): `omarchy` topic, asset names, icon, checksums
- [x] `make release`/`install`/`uninstall`; the frontend reconfigures when `BUILD_TYPE` changes
- [ ] Publish on the AUR (depends on the first commit/push; see `docs/ideas/distribution.md`)

## Phase 10 — Search and recommendation (`internal/search`)

Local, deterministic search and "similar apps" (same input, same
result), with no external service or LLM.

- [x] Tokenization: lowercase, no accents, pt/en stopwords, simple plural and -ing/-ed (Porter step 1b)
- [x] BM25 ranking with per-field weights (name > topics > summary > README) and stars as tiebreaker
- [x] Typo tolerance: prefix and edit distance 1 (only for out-of-vocabulary terms; variants' idf capped at the exact term's); logical AND with fallback to OR
- [x] "Similar apps": TF-IDF (topics, category, summary, README) + cosine; ties broken by name
- [x] In-memory index in the daemon, rebuilt when the catalog changes
- [x] `catalog.list` with `query` uses the ranking; new `catalog.similar` method; `omastore similar` CLI
- [x] Frontend: "Similar apps" section in the detail page
- [x] Tests: expected ranking, typo, accents, determinism, similar apps
- [x] Portuguese queries: pt→en dictionary of app terms ("editor de fotos", "voz para texto", "calendário")
- [x] Calibrated on the real catalog (34 repos): similarity threshold 0.12 (real pairs > 0.2; noise 0.09–0.16)

## Phase 11 — `omastore.toml` manifest (see `docs/ideas/manifest.md`)

- [x] Parser and validation (optional fields; paths go through the same locks)
- [x] The indexer reads the manifest from the tree and overrides the heuristics (name, summary, categories, icon, screenshots, terminal)
- [x] Per-architecture asset with a `{version}` pattern and declared executable; fall back to heuristics if it does not match
- [x] `omastore lint-manifest <dir>`; document in `docs/authors.md`; bump `index.Version`
- [x] Migration 0003 (`apps.manifest`); the installer uses the declared asset/exec/terminal/categories; an `exec` that is a symlink pointing outside is refused
- [x] An invalid manifest never breaks indexing; it is only fetched when it shows up in the tree

## Phase 12 — Batch discovery (see `docs/ideas/graphql-discovery.md`)

- [x] Batched GraphQL query of the cache-check fields (with token); REST still used without a token
- [x] Repos without a release skip README/tree
- [x] Seeds from curated lists (`github.com/owner/repo` links in a README)
- [x] Measured with ~230 real repos: 1st indexing 863 → 173 requests (76 s → 45 s); reindexing 497 → 12 requests (44 s → 15 s, batches of 25 with 3 in parallel)
- [x] `omastore index` shows the number of requests; `--no-batch` forces REST; nonexistent repos outside the catalog count as "skipped"
- [x] `aorumbayev/awesome-omarchy` list in the seeds: installable catalog from 15 → 25 apps

## Phase 13 — On-demand daemon (see `docs/ideas/on-demand-daemon.md`)

- [x] Exit when idle (no connections and no jobs) when socket-activated
- [x] `omastore update --check` and desktop notification via D-Bus
- [x] Daily systemd timer (`omastore-index.timer`)
- [x] A job finishing counts as activity (without it, the daemon thought it had been idle "for hours" right after a long index)
- [x] Notify only when the set of repo@version changes; a failure to notify does not save the state (retries)
- [x] Tested: daemon with `-idle-timeout 1s` exits ~1 s after the last client and removes the socket; D-Bus without a notification server gives a clear error

## Phase 14 — Distribution (see `docs/ideas/distribution.md`)

- [x] Tag-triggered release workflow (`.github/workflows/release.yml`, validated with actionlint): tests, tarball, SLSA attestation, `-bin` PKGBUILD and upload
- [x] `omastore-bin` `PKGBUILD` generated from `packaging/arch-bin/PKGBUILD.in` with the real sha256 (`make pkgbuild-bin`); tested with `makepkg`
- [x] `make dist`: reproducible tarball (same bytes in two builds; `--sort=name`, owner 0, `SOURCE_DATE_EPOCH`, `gzip -n`, `-trimpath`, stripped binaries: 21 → 14 MB)
- [x] Tag validated before becoming a file name (passed through an environment variable, never interpolated)

## Phase 15 — Admission by manifest and provenance

- [ ] Require a valid `omastore.toml` to appear in **Discover**, with `asset` and `exec` declared for a supported architecture; the asset must exist in the latest stable release. Presentation fields stay optional.
- [ ] Concentrate the app and asset eligibility decision in a rule shared by indexing and installation; the database stores the result for queries, but install/update must check the rule again with current data. Test that catalog, CLI and daemon reach the same decision.
- [ ] Require the asset to have been built and published by a GitHub Actions workflow of the repository itself, with a verifiable provenance attestation bound to the digest, repository and commit/tag of the release.
- [ ] On install and update, verify the digest of the downloaded bytes and the provenance of the selected asset; refuse a missing, mismatched or unattested asset. Do not accept an arbitrary URL declared in the manifest.
- [ ] Revalidate the release assets on reindex even when the tag did not change: a file can be replaced under the same tag, and the current cache only compares the tag, HEAD and `pushed_at`.
- [ ] Keep already installed apps visible in **Installed** and allow uninstalling them even if they no longer meet the new rule; block updates without valid provenance.
- [ ] Generate, from the repository, release and file data, a diagnosis and a copyable prompt to adapt the project: manifest, build/release workflow, attestation and validation commands. Do not invent executable paths or build steps that cannot be confirmed.
- [ ] Update `omastore lint-manifest`, the authors guide and the cached index for the new policy; test admission, rejection, installation and the case of legacy apps already installed.

## Maintenance fixes — source code triage

- [ ] **High — reliable uninstallation:** `install.removeRegistered` only logs failures, but `Uninstall` deletes the database record even when a file was not removed (`backend/internal/install/install.go`). Distinguish a missing file, a file changed by third parties and an I/O failure; keep the pending paths in the database to allow a retry. On update, also handle failures when cleaning up files from the previous version.
- [ ] **Medium — stale responses in the interface:** `Backend::reloadDetail` and `loadSimilar` discard responses for another repository, but accept stale responses for the same repository (`frontend/src/backend.cpp`). Use a per-request generation id and test out-of-order responses, as `CatalogModel` already does.
- [ ] **Medium — checksum state name:** `AssetInfo.verified` only means a digest or checksum file is available before the download (`backend/internal/rpc/methods.go`, `frontend/qml/DetailPage.qml`). Rename the field and the displayed text so they do not suggest a completed verification; keep provenance as a separate state in Phase 15 and update `docs/ipc.md`.
- [ ] **Medium — repository identity:** `github.SplitFullName`, `rpc.repoParams.validate` and `install.splitName` accept different sets of `owner/repo` names. Use a single validation before API calls and local path operations; cover invalid and valid names in shared tests.
- [ ] **Medium — format boundary:** `install` imports `index` only for constants, architecture classification and format preference (`backend/internal/install/{install,extract}.go`). Move these asset concepts into a neutral module used by both, without making the installer depend on the indexing pipeline.
- [ ] **Medium — single entry point for use cases:** the CLI calls `Indexer` and `Installer` directly and changes indexer options, while the daemon uses `app.App` methods (`backend/cmd/omastore/main.go`, `backend/internal/app/backend.go`). Pass options per call and route both interfaces through the same application methods, especially before adding the Phase 15 policy.
- [ ] **Low — first indexing:** `Backend::maybeIndexOnFirstRun` marks the query as done before the response (`frontend/src/backend.cpp`). Allow a retry after a transient error so a fresh installation is not left with an empty catalog until the user acts.
- [ ] **Low — errors in the indexer tests:** replace the `stats, _ = ix.Run(...)` calls in `backend/internal/index/index_test.go` with explicit error checks; an indexing failure should not show up only as an unexpected statistic.

## Phase 15 — Apps with a manifest only + author skills

- [x] `omastore.toml` mandatory: without it the repo is not added (and is removed, if it was there); an empty file counts as opt-in
- [x] `kind` field (`app` default); `plugin`/`theme`/others are not indexed; `lint-manifest` warns
- [x] The manifest comes in the GraphQL batch (`object(expression: "HEAD:omastore.toml")`), with no extra request; REST distinguishes an empty file from a missing one
- [x] Discovery through the code search `filename:omastore.toml` (root), independent of topic
- [x] Bug: `index.Version` did not force reprocessing when the discovered name differed from the stored one (capitalization/rename) — the force flag is recomputed after resolving the name; regression test
- [x] Real catalog: 0 apps until authors adopt the manifest (explanatory message in the interface)
- [x] Skills in `skills/`: `omastore-manifest`, `omastore-release`, `omastore-check`

## Future ideas

Detailed proposals, with evidence and cost, live in [`docs/ideas/`](docs/ideas/README.md).


- [ ] Signature verification (minisign/cosign) in addition to checksums
- [ ] Automatic background updates (update notifications already exist: Phase 13; installing on its own is still missing, opt-in)
- [ ] Flatpak/AppImage support with sandbox integration
- [ ] Ratings/flagging of problematic apps
