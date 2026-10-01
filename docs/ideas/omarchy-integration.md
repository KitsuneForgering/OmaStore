# Omarchy integration: a menu entry

The `post-update` hook, clickable update notifications and terminal apps
through `omarchy-launch-or-focus-tui` are done (Phase 18 of `TODO.md`). The
menu entry is what is left.

## Evidence

Checked on an Omarchy install (`/usr/share/omarchy`, 2026-09-30):

- The shell menu reads the defaults plus **one** user file,
  `~/.config/omarchy/extensions/omarchy-menu.jsonc`
  (`shell/plugins/menu/Menu.qml:50-51`). Rows can have an `action` or a
  `provider` that returns JSON rows at runtime. There is no drop-in
  directory, and the file is the user's, with comments.

## Proposal

Do not edit `omarchy-menu.jsonc`: merging JSONC with comments is fragile, and
a broken file breaks the whole menu. Instead:

- document a snippet the user pastes (an "OmaStore" submenu with "Open",
  "Updates" through a provider running `omastore list --installed --json`,
  and "Refresh catalog");
- propose upstream a drop-in directory (`extensions/omarchy-menu.d/*.jsonc`);
  if Omarchy adds it, `packaging/install.sh` installs the file like the hook
  (with the `# omastore-managed` line, removed by `--uninstall`).

## Cost

Documentation now; a file in `packaging/install.sh` once Omarchy has the
drop-in directory.

## Risks

- The menu's JSON format and the provider protocol are Omarchy's and can
  change between releases; a pasted snippet goes stale silently.
