# Interface ideas

Open items for the frontend. Search, grid navigation, `/`, `Esc`, `Ctrl+R`,
the checksum badge, the empty state, the rate limit message and the theme are
done (Phase 8); release notes and the "Open" button are done (Phase 18);
`h/j/k/l`, `i`, `u`, the `?` list, `omastore://` links and
pagination are done.

- **Translation:** all interface strings already go through `qsTr()` and are
  in English. Generate `.ts` files with `qt_add_translations` and add a `pt_BR`
  for Portuguese speakers; the CLI and the daemon's error messages are also
  in English and would need another mechanism (error codes are already
  stable, so the frontend can translate them through the table in `docs/ipc.md`).
