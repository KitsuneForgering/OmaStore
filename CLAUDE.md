# CLAUDE.md

This file guides Claude Code (claude.ai/code) when working in this repository.

## Overview

OmaStore is an app store for [Omarchy](https://omarchy.org) (Arch Linux + Hyprland).
It indexes apps published on GitHub (e.g. OmaVM, OmaDesign, OmaPhoto, Rawmakase), shows each
one with a description, icon and screenshots taken from its own repository, and installs the binary of the latest
release with one click, generating the matching `.desktop`.

Catalog items are **standalone apps**, not Omarchy plugins or themes. **Only repositories with an
`omastore.toml` at the root** declaring an app get into the catalog (`kind = "app"`, the default; `plugin` and
`theme` are left out). The file's presence is the author's opt-in; its content may be empty and the rest is
inferred by heuristics. Format in `docs/authors.md`; skills for authors in `skills/`.

## Architecture

Two processes with separate responsibilities:

```
┌──────────────────────────┐   JSON-RPC over Unix socket    ┌─────────────────────────────┐
│ frontend (C++ / Qt Quick)│ ─────────────────────────────▶ │ backend (Go, omastored)     │
│ QML + QObject models     │ ◀───────────────────────────── │ indexer, cache, installer   │
└──────────────────────────┘   progress events              └──────────────┬──────────────┘
                                                                           │
                                            go-github (API) · go-git (shallow clone) · SQLite
```

- **Backend (Go)** holds all the logic: discovery, indexing, cache, download, installation and
  uninstallation. It must work on its own (there is a CLI for testing without the GUI).
- **Frontend (C++/QML)** is presentation only. It does not access the network, GitHub or the database directly;
  everything goes through the backend.
- **IPC**: JSON-RPC 2.0 (one JSON message per line) on `$XDG_RUNTIME_DIR/omastore.sock`.
  Long operations (indexing, installation) return a job id and emit progress notifications
  over the same socket. The protocol is in `docs/ipc.md`; keep it up to date when changing methods or DTOs.
- **Ideas** not yet accepted live in `docs/ideas/` (one per file, with evidence and cost);
  when one is accepted, turn it into items in `TODO.md`.

### Backend

- `github.com/google/go-github` — repository search, metadata (stars, topics, `pushed_at`),
  releases and assets. Optional authentication through `GITHUB_TOKEN` (or `gh auth token`) to avoid the rate limit.
  Use conditional requests (ETag / `If-None-Match`) whenever possible.
- `github.com/go-git/go-git/v5` — shallow clone (`Depth: 1`) when the API is not enough: extract icons,
  screenshots and metadata files from the repository. Clones live in `$XDG_CACHE_HOME/omastore/repos/`.
- `github.com/mattn/go-sqlite3` — cache/index in `$XDG_DATA_HOME/omastore/omastore.db`.
  Requires CGO (`CGO_ENABLED=1`).

### Frontend

- Qt 6, Qt Quick + QML, built with CMake.
- The C++ layer exposes to QML: an IPC client (`QLocalSocket`) and `QAbstractListModel` models
  (catalog, installed, jobs). No business logic in QML beyond presentation and simple filters.
- The look follows the active Omarchy theme: `colors.toml` in `~/.local/state/omarchy/current/theme/` (older
  installations: `~/.config/omarchy/current/theme/`), reloaded when the theme changes. `Theme` adjusts the
  theme's colors to WCAG AAA (text 7:1 on background/surface/selection/hover; borders and focus 3:1): QML uses
  only `theme.*` colors, never `opacity` or `Qt.lighter/darker` on text or its background, and text on a
  fill uses the matching pair (`accentFill`/`onAccent`, `dangerFill`/`onDanger`, `focus`/`onFocus`).
  Animation durations come from `theme.durationShort`/`durationMedium`, which are 0 with reduced motion.
- The frontend never accesses the network: images come from the daemon (`image.get`, provider `image://omastore/`), the
  QML engine uses a `QNetworkAccessManager` that blocks remote URLs and the README is shown without images.
- The frontend never looks for `omastored` in `PATH` (which includes `~/.local/bin`, where downloaded apps live):
  it uses `$OMASTORED`, the executable's directory or `/usr/bin`.

## Indexing pipeline

1. **Discovery** — code search for `filename:omastore.toml`, topic search (`topic:omarchy`),
   seeds and curated lists (`list:owner/repo` in `seeds.txt`), **plus every repository already stored**, so one
   that is no longer discovered is still checked. Without an app `omastore.toml`, the repository
   does not get in (and is removed, if it was there). Without a release with a Linux binary, it gets in but is not installable.
   `--prune` is skipped when a discovery source failed (a partial list would delete healthy repos).
2. **Cache check** — for each repository, compare `pushed_at`, the HEAD commit SHA and the latest
   release tag with what is in SQLite. **If nothing changed, do not reprocess.** That is why the
   database exists; never remove this check for convenience. As a consequence, **when changing
   extraction/classification rules (assets, categories, README), bump `index.Version`**; otherwise repos
   already stored are never reclassified.
3. **Extraction** — README (rendered as short + long description), icon, screenshots, license,
   topics, stars and the system dependencies of the repository's PKGBUILD/.SRCINFO. Prefer the API; clone only if you need files the API does not deliver well.
4. **Classification** — category from the topics; ordering by stars (with recency as tiebreaker).
5. **Persistence** — write everything in one transaction and record the processed SHA/tag.

## Installing an app

1. Pick the release asset by architecture (`x86_64`/`amd64`, `aarch64`/`arm64`) and format
   (plain binary, `.tar.gz`, `.zip`, AppImage).
2. Download to a temporary directory and validate the checksum when the release publishes one (`*.sha256`, `checksums.txt`).
3. Extract into `$XDG_DATA_HOME/omastore/apps/<owner>__<repo>/<version>/` and create a link in `~/.local/bin/`.
4. Save the icon in `~/.local/share/icons/hicolor/<size>/apps/` and generate
   `~/.local/share/applications/omastore-<owner>-<repo>.desktop` (Name, Comment, absolute Exec,
   Icon, Categories derived from the topics).
5. Record the installation in SQLite (version, created paths) to allow updates and clean uninstallation.

Rules:
- Never execute the downloaded binary during installation.
- Guard against path traversal when extracting files (zip slip) and against symlinks leaving the destination directory.
- Escape every field coming from the repository before writing it into the `.desktop`.
- Nothing is installed outside the user's `$HOME`; no `sudo`. The single exception is an app's system
  dependencies (`internal/sysdeps`): only on the user's request, only through `pkexec /usr/bin/pacman -S --needed`
  (polkit asks for the password), only package names validated and resolved by pacman from the configured
  repositories (never AUR, never a helper such as yay). The PKGBUILD/.SRCINFO they come from is parsed, never run.
- OmaStore updates itself (`install/self.go`, `self.*` methods) only for the per-user installation made by
  `install.sh` (`$XDG_DATA_HOME/omastore/self/<version>/` + links in `~/.local/bin`); the version comes from the
  running executable's path. The tarball must have a checksum, nothing from it is run, the links/menu entry/icon
  switch with rollback, and the running version is kept until the next update. Packages are left to pacman.
- Uninstalling removes only the paths registered in the database. A path that could not be removed stays
  registered (uninstall returns `ErrIncomplete`), never silently forgotten.

## Database schema (summary)

- `repos` — `full_name` (PK), description, stars, topics, `pushed_at`, `head_sha`, `latest_tag`, `etag`,
  `indexed_at` (last checked), `changed_at` (last change; drives `catalog.changed` and the search index).
- `apps` — display data derived from the repo: name, summary, README, icon, category, score.
- `assets` — release assets per repo/tag (name, url, arch, format, checksum).
- `installs` — installed app, version, date, list of created files.
- `not_apps` — repositories recently found without an app `omastore.toml`; skipped without requests for 7 days.
- `apps.sysdeps` — `depends`/`optdepends` read from the repository's PKGBUILD/.SRCINFO at index time (JSON).
- `repos.release_notes` — body of the latest release (markdown, capped at 16 KiB), shown as "What's new".

Repository names are case-insensitive, as on GitHub: lookups by a name the user typed use `COLLATE NOCASE`
and return the stored spelling; names are validated in one place (`internal/repoid`).

Schema changes go through numbered migrations in `backend/internal/store/migrations/`; never edit a migration that has already been published.

## Directory layout

```
backend/
  cmd/omastored/        # daemon (IPC server)
  cmd/omastore/         # debug CLI: index, list, show, install, uninstall, update, self-update, check, deps, star
  internal/app/         # wires the services; the single entry point for the CLI and the daemon
  internal/github/      # go-github wrapper
  internal/gitrepo/     # shallow clones via go-git + icon/screenshot lookup
  internal/index/       # discovery, extraction, classification (index.Version)
  internal/asset/       # release file format/architecture/checksum rules (index and install)
  internal/repoid/      # owner/repo validation shared by every entry point
  internal/flock/       # cross-process file locks (index, per-app install)
  internal/manifest/    # the app's omastore.toml (strict validation in lint)
  internal/store/       # SQLite + migrations
  internal/install/     # download, verification, extraction, launcher, .desktop
  internal/imagecache/  # remote images for the frontend
  internal/notify/      # desktop notifications via D-Bus (without running notify-send)
  internal/search/      # BM25 search and "similar apps" (TF-IDF), deterministic
  internal/sysdeps/     # PKGBUILD/.SRCINFO dependencies (parsed, never run) + pacman check/install via pkexec
  internal/rpc/         # JSON-RPC server, jobs, socket activation
frontend/
  CMakeLists.txt        # omastore-core lib + app + tests (ctest)
  src/                  # main.cpp, IPC client, models, theme, image provider
  qml/                  # screens and components
  tests/                # Qt Test with a fake daemon (QLocalServer)
packaging/              # PKGBUILD, systemd units, the store's .desktop and icon
docs/                   # ipc.md, authors.md, ideas/, screenshots/
skills/                 # agent skills for app authors (install.sh copies them for every agent)
```

## Commands

Everything goes through the root `Makefile` (`make help` lists the targets):

```sh
make                 # builds the backend (bin/omastore, bin/omastored) and the frontend
make backend         # Go binaries only
make frontend        # frontend only (cmake + ninja in frontend/build)
make test            # all tests
make test-backend    # go test -race ./...
make test-backend TESTFLAGS='-run TestName ./internal/index'   # a single test
make test-frontend   # ctest (QT_QPA_PLATFORM=offscreen)
make test-skills     # Go lint vs. the skills' Python validator over skills/tests/manifests
make check           # gofmt + vet + backend tests (what CI runs)
make fmt             # gofmt -w
make run ARGS=index  # index without the GUI
make run-gui         # builds and opens the Qt interface
make clean
make release && make install DESTDIR=... PREFIX=/usr   # what the PKGBUILD does
make dist VERSION=v1.2.3   # reproducible tarball + .sha256 in dist/ (used by the release workflow)
make pkgbuild-bin VERSION=v1.2.3   # omastore-bin PKGBUILD with the tarball's sha256
```

Useful variables: `BUILD_TYPE` (default `Debug`), `GENERATOR` (default `Ninja`), `TESTFLAGS`, `ARGS`.
The Makefile exports `CGO_ENABLED=1` (required by go-sqlite3). CI calls the same targets.

## Conventions

- All project text is in English: code, comments, log/error/CLI messages, UI strings, docs and commit
  messages. The exception is the Portuguese search data in `internal/search` (stopwords, pt→en synonyms),
  which exists to understand Portuguese queries.
- Go: `gofmt`, errors with context (`fmt.Errorf("...: %w", err)`), `context.Context` on every long network/IO operation.
- Backend tests do not access the network: use fixtures and `httptest` to simulate the GitHub API.
- C++: C++20, no network logic in the frontend, Qt signals/slots to update models.
- Honor the XDG variables (`XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_RUNTIME_DIR`) with the specification defaults.
