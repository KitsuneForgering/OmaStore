# Omarchy shell plugins in the catalog

## Decision

Declined for now (2026-09-30): OmaStore stays a store for standalone apps, and
`kind = "plugin"` keeps being left out. The proposal stays here in case that
changes.

## Evidence

- Omarchy has its own plugin system for the Quickshell shell: bar widgets,
  panels, overlays, menus and services, each described by a `manifest.json`
  (`schemaVersion`, `id`, `kinds`, `entryPoints`). They are managed with
  `omarchy-plugin-add <git-url>`, `-enable`, `-update`, `-remove` and
  `-validate`, and live in `~/.config/omarchy/plugins/<id>/`.
- The same people who write Omarchy apps write plugins: the user of this
  store has three installed, one of them their own
  (`io.github.kitsunesemcalda.feader-rss`).
- OmaStore deliberately leaves them out: `omastore.toml` with
  `kind = "plugin"` is not indexed (CLAUDE.md, Phase 15b). There is no
  catalog to discover plugins today; `omarchy-plugin-catalog` only lists the
  installed ones.

## Proposal

A separate "Plugins" section, not mixed with apps:

1. **Discovery:** repositories with a root `manifest.json` that passes
   `omarchy-plugin-validate`'s schema, plus `omastore.toml` with
   `kind = "plugin"` as the opt-in (the same rule as apps).
2. **Installation delegated to Omarchy:** OmaStore does not copy files; it
   runs `omarchy-plugin-add <https clone url> --yes` (by absolute path, never
   through `PATH`) and `omarchy-plugin-enable`, and reads the installed state
   back from `omarchy-plugin-list --json`. Updates and removal also go through
   the `omarchy-plugin-*` commands.
3. **Display:** name, description, kinds and screenshots from the manifest
   and the README, like apps; no "Install" button when Omarchy is absent.

## Cost

Medium to large: a second item type across index, store, RPC, CLI and
interface; a validator for the plugin manifest (or calling Omarchy's); and a
decision on how plugins show in search and "similar apps".

## Risks

- **This changes what OmaStore is.** CLAUDE.md defines the catalog as
  standalone apps; plugins need that decision first.
- Plugins are source code (QML/JS) that runs inside the shell with the
  user's permissions, cloned from a branch: there is no release, checksum or
  provenance to check, unlike app binaries. The store would be recommending
  code it cannot verify, so it must say so clearly.
- The plugin manifest and the `omarchy-plugin-*` commands are Omarchy's and
  can change between releases.
