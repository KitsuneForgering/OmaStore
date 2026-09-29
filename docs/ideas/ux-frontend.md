# Interface ideas (Phase 8)

Notes for the frontend. Items marked **done** are already in Phase 8.

- **Keyboard first** (*partial: `/`, `Esc`, `Ctrl+R` and grid navigation done; `i`/`u` missing*): Omarchy users live on the keyboard. `/` focuses the
  search, `j/k` moves through the grid, `Enter` opens the detail, `i` installs, `u`
  updates, `Esc` goes back.
- **Verification badge** (*done*): `AssetInfo.verified` already exists in the protocol.
  Show "checksum verified" or "no checksum" before installing, and ask for
  confirmation in the second case (see [confianca.md](confianca.md)).
- **Release notes:** show the new version's changelog on the
  "Update" button. *Backend:* store the release `body` in the index (it is not
  stored today).
- **Open after installing:** an "Open" button that uses the generated `.desktop`
  (`gtk-launch omastore-owner-repo`). Installation still executes nothing;
  opening is an explicit user action.
- **Empty state** (*done*): on the first run the catalog is empty. Start
  `index.start` automatically and show the progress (`job.progress` carries the
  current repo in `message`).
- **Rate limit** (*done*): error `-32005` should suggest `gh auth login` and say
  when the limit resets.
- **Theme** (*done*): colors from `~/.local/state/omarchy/current/theme/colors.toml`,
  reloaded when the theme changes (Omarchy replaces the whole directory).
- **Install from a link:** register an `x-scheme-handler/omastore` handler
  for `omastore://owner/repo` links (e.g. in a README), which opens
  `omastore-gui --open owner/repo`. The `--open` option already exists.
- **Pagination:** `catalog.list` already accepts `limit`/`offset`; with hundreds of
  apps, load on demand while scrolling the GridView.
- **Translation:** all interface strings already go through `qsTr()` and are
  in English. Generate `.ts` files with `qt_add_translations` and add a `pt_BR`
  for Portuguese speakers; the CLI and the daemon's error messages are also
  in English and would need another mechanism (error codes are already
  stable, so the frontend can translate them through the table in `docs/ipc.md`).
