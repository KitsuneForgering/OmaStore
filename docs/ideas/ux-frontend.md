# Interface ideas

Open items for the frontend. Search, grid navigation, `/`, `Esc`, `Ctrl+R`,
the checksum badge, the empty state, the rate limit message and the theme are
done (Phase 8); release notes and the "Open" button are done (Phase 18);
`h/j/k/l`, `i`, `u` and the `?` list are done.

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
