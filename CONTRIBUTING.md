# Contributing to OmaStore

Thanks for helping. This guide covers the store itself. If you want **your app
listed in the store**, you do not need to change this repository: follow
[`docs/authors.md`](docs/authors.md) and run `omastore check owner/repo`.

## Ways to help

- **Report a bug** or an app that installs incorrectly: open an issue with the
  output of `omastore check owner/repo` or the steps to reproduce.
- **Suggest an idea**: small ones as an issue; larger ones as a file in
  [`docs/ideas/`](docs/ideas/README.md), with the evidence and the cost.
- **Send a pull request**: fixes, tests, documentation, translations of the
  Portuguese search synonyms, or the interface in your language: run
  `make translations`, copy `frontend/i18n/omastore_pt_BR.ts` to
  `omastore_<locale>.ts`, translate it in Qt Linguist (`linguist6`) and run
  `make translations` again.

For security problems, do not open a public issue: see [SECURITY.md](SECURITY.md).

## Development setup

Requirements: Go 1.26+, a C compiler (CGO, for SQLite), Qt 6.5+
(`qt6-base`, `qt6-declarative`, `qt6-svg`; `qt6-tools` for the translations),
CMake and Ninja. On Arch/Omarchy:

```sh
sudo pacman -S --needed go base-devel cmake ninja qt6-base qt6-declarative qt6-svg qt6-tools
```

Everything goes through the `Makefile`:

```sh
make              # builds the backend (bin/) and the frontend (frontend/build/)
make check        # gofmt + vet + backend tests + installer test (what CI runs)
make test         # every test: backend, frontend, skills, installer
make run ARGS=index   # the CLI against your real catalog
make run-gui      # the interface with the daemon from bin/
make help         # all targets
```

A single backend test: `make test-backend TESTFLAGS='-run TestName ./internal/index'`.

Set `GITHUB_TOKEN` (or run `gh auth login`) before indexing: anonymous access
allows 60 GitHub requests per hour.

## The release build is pinned

The **Release** workflow and the frontend CI job build inside a fixed Arch
snapshot (`archlinux:base-20260315.0.500537`, i.e. 2026-03-15: Go 1.26 and
Qt 6.10.2), with pacman pointed at the Arch Linux Archive for that date, never
the rolling `archlinux:latest`. The shipped `omastore-gui` is dynamically
linked against Qt, so a build made with a newer Qt than the users' systems have
refuses to load there (`version 'Qt_6.12' not found`, on a system at Qt 6.11).
`make check-qt-floor` fails the build if the GUI needs Qt symbols above the
floor (`QT_FLOOR`, default 6.10). Raise the floor only deliberately, in the
same change that bumps the snapshot and the floor check; the check is what
catches a silent drift.

## How the code is organized

The Go daemon (`backend/`) holds all the logic; the Qt interface (`frontend/`)
only presents it and never touches the network. They talk JSON-RPC over a Unix
socket, documented in [`docs/ipc.md`](docs/ipc.md). [`CLAUDE.md`](CLAUDE.md)
describes the architecture, the indexing pipeline and the installation rules
in detail; read it before a larger change.

## Rules that reviews check

- **Everything in English**: code, comments, messages, UI strings, docs and
  commits. The only exception is the Portuguese search data in `internal/search`.
- **Tests do not access the network.** Simulate GitHub with fixtures and
  `httptest`; the frontend tests use a fake daemon.
- **Installation safety is not negotiable**: never run a downloaded file, never
  write outside `$HOME`, guard every extracted path, escape every repository
  field written to a `.desktop`.
- **Schema changes are new migrations** in `backend/internal/store/migrations/`;
  never edit one that was released.
- **Changing what the indexer stores** (assets, categories, README handling)
  requires bumping `index.Version`, or stored repositories are never
  reprocessed.
- **Changing an RPC method or DTO** requires updating `docs/ipc.md`.
- Go code is `gofmt`ed, errors carry context (`fmt.Errorf("...: %w", err)`)
  and long operations take a `context.Context`.

## Commits and pull requests

Commits follow [Conventional Commits](https://www.conventionalcommits.org/)
with the area as scope, as in the history: `feat(index): ...`,
`fix(install): ...`, `docs: ...`, `ci: ...`. Explain *why* in the body when it
is not obvious.

Before opening a pull request:

1. `make check` passes (and `make test-frontend` if you touched `frontend/`).
2. New behavior has a test; a bug fix has a regression test.
3. User-visible changes are noted under **Unreleased** in
   [CHANGELOG.md](CHANGELOG.md).

Contributions are released under the [MIT license](LICENSE).

## Cutting a release

The **Release** workflow publishes everything: the reproducible tarball, its
`.sha256`, the `omastore-bin` PKGBUILD and the provenance attestation. It runs
on a tag push, so the release *is* the tag:

```sh
make check && make test      # the workflow runs them again
git tag v1.2.3 && git push origin v1.2.3
gh run watch                 # the Release workflow
gh release view v1.2.3 --json assets   # the four assets must be there
```

Never create the release by hand (`gh release create`): published before the
workflow, it advertises itself as the latest release with no tarball, and
`install.sh` and `omastore self-update` fail on it until the workflow catches
up. Also rename the **Unreleased** section of [CHANGELOG.md](CHANGELOG.md) to
the version being tagged, in the same commit.
