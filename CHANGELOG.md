# Changelog

All notable changes to OmaStore are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- OmaStore updates itself: an installation made by `install.sh` sees new
  releases in the sidebar ("Update OmaStore", then "Restart OmaStore") and with
  `omastore self-update [--check]`; `omastore update --check --notify` (and the
  Omarchy hook) reports it too. The release tarball must match its published
  SHA-256, the switch is all or nothing, and the previous version stays until
  the next update. Package installations keep updating through pacman.
- Demo GIF in the README.
- Accessibility: every text color meets WCAG AAA (7:1) on any Omarchy theme,
  light or dark. The theme's own colors are adjusted when needed, keeping
  their hue; borders and the focus ring get at least 3:1. Without Omarchy the
  palette follows the system's light/dark preference. Links and the similar
  apps are reachable from the keyboard with a visible focus ring, cards and
  screenshots have names for screen readers, and error messages stay until
  dismissed.
- Star button on an app's page: liking an app stars its repository on GitHub
  (and removes the star when pressed again). Needs `gh auth login` or
  `GITHUB_TOKEN`; CLI: `omastore star`/`omastore unstar`.
- System dependencies: the `depends` and `optdepends` of the app's `PKGBUILD`
  or `.SRCINFO` are shown on its page, and after installing the app OmaStore
  offers to install the missing ones with pacman (`pkexec`, asks for the
  administrator password). Packages only in the AUR are listed but never
  installed. CLI: `omastore deps [--install]`.
- Release notes: an app's page shows what changed in its latest release
  ("What's new in v1.3"), and `omastore show` and `omastore update --check`
  print them.
- "Report a problem" on an app's page, and after a failed install or update:
  opens a new issue on the app's repository prefilled with the versions, the
  architecture, the release files and the error. Nothing is sent until you
  submit it on GitHub.
- "Open" button for installed apps (through their menu entry, `gtk-launch`).
- Omarchy integration: `install.sh` adds a `post-update` hook, so
  `omarchy-update` also tells you about updates to your OmaStore apps
  (`--no-hooks` skips it; packages ship it in `/usr/share/omastore/omarchy/`).
  Clicking the update notification opens the app, or the Installed page when
  there are several (`omastore-gui --page installed`). Terminal apps open
  through `omarchy-launch-or-focus-tui`: Omarchy's terminal, and an already
  open window is focused instead of starting a second one.

### Fixed

- `omastore.toml` categories follow the freedesktop registry: `2DGraphics` and
  `3DGraphics` are accepted, as are `X-` extensions, and names the
  specification does not register (which `desktop-file-validate` rejects) are
  reported instead of going into the menu entry.

- Publish page: an empty `omastore.toml` can be tested (the checkbox, not the
  text, decides whether the local file is sent), and a pass with a local file
  says "works with this omastore.toml; push it" instead of "ready".
- The checker no longer promises the app "shows up for everyone on the next
  refresh": it warns when the `omarchy` topic is missing (anonymous clients
  only find repositories by topic), mentions the up-to-7-day wait for
  repositories seen before they had a manifest, points to
  `omastore index owner/repo`, and says that it does not download or run the app.
- `omastore-check` audit script: a GitHub access failure (bad token, rate
  limit, network) is no longer reported as a missing `omastore.toml`; it no
  longer needs `gh` to read the manifest.
- `omastore-release` templates use the same action versions as OmaStore's own
  release and attest the tarballs' build provenance.

- Indexing without a GitHub token now makes progress across runs: repositories
  without an app `omastore.toml` are remembered for 7 days instead of being
  checked again on every run, and a new repository costs one request instead
  of two.
- Repository names are case-insensitive, as on GitHub: `omastore show PCH/Rawmakase`
  finds `pch/rawmakase` (also `install`, `uninstall`, `update` and the RPC methods).
- The CLI and the daemon can no longer change the same app at the same time;
  the second one fails with a "busy" error instead of racing.
- A client that stops reading the daemon's socket is disconnected instead of
  slowing down indexing and the other clients.
- An index run that changed nothing no longer tells the interface to reload.

### Changed

- Repository names are validated by a single rule everywhere (owner with
  letters, digits and hyphens; repository starting with a letter or digit).

## [0.1.0] - 2026-09-30

First public release.

### Added

- Catalog of standalone Omarchy apps from GitHub repositories that publish an
  `omastore.toml`, found through code search, the `omarchy` topic and curated
  lists, with descriptions, icons, screenshots, categories and search
  (BM25, typo tolerance, Portuguese queries).
- One-click installation of the latest release into your account: checksum
  verification when published, protected extraction, launcher in
  `~/.local/bin`, menu entry and icon; updates, update notifications and clean
  uninstallation, without `sudo`.
- Qt Quick interface that follows the active Omarchy theme, with the catalog
  streaming in while it is indexed.
- **Publish your app** page and `omastore check` for authors, plus Claude Code
  skills to write the manifest, set up releases and audit an app.
- `install.sh`, `omastore-bin` PKGBUILD, reproducible release tarball with
  build provenance attestation, and optional systemd socket and daily timer.

[Unreleased]: https://github.com/KitsuneSemCalda/OmaStore/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/KitsuneSemCalda/OmaStore/releases/tag/v0.1.0
