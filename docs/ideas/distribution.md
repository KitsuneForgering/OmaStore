# Distribution: the AUR and who manages the store

Phase 14 delivered the release workflow, the reproducible tarball and the
`omastore-bin` PKGBUILD (`packaging/arch-bin/`). Two items are still open.

## Evidence

- Neither `omastore-bin` nor `omastore-git` is on the AUR (AUR RPC,
  2026-09-30). Users install through `install.sh` or `make install`.
- One machine can end up with two copies: `make install PREFIX=/usr/local`
  plus `install.sh` in `~/.local`. `/usr/local/bin` comes first in `PATH`, so
  the terminal keeps running the old copy after `install.sh` updates the other.
- The store refuses to install itself on purpose: the `omastore*` commands
  are reserved names, so OmaStore cannot appear in its own catalog and update
  itself.

## Proposal

1. **Publish on the AUR** `omastore-bin` (generated per release by
   `make pkgbuild-bin`, with the tarball's sha256) and `omastore-git`.
2. **One owner for the installation:** pacman when installed from a package,
   `install.sh` otherwise. `install.sh` and the interface's About should
   detect another copy (`/usr/bin`, `/usr/local/bin`, `~/.local/bin`) and say
   which one runs, instead of silently installing a second one. Self-update
   through the catalog stays out.

## Cost

Small: an AUR account and pushing the generated PKGBUILDs; a `command -v -a`
check in `install.sh` and one line in the interface.

## Risks

- An AUR package needs someone to keep it in sync with each release; the
  release workflow already produces the PKGBUILD, so this is one push.
