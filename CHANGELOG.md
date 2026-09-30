# Changelog

All notable changes to OmaStore are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed

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
