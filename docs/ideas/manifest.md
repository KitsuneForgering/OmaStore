# `omastore.toml` manifest in the app repository

> **Status:** implemented in Phase 11 (`backend/internal/manifest`) and made
> **mandatory** in Phase 15: only repositories with an app `omastore.toml` are
> indexed. Format
> documented in [`../authors.md`](../authors.md#6-the-omastoretoml-manifest-required).

## Evidence

The heuristics get it right most of the time, but real tests showed
failures that only the app author can resolve with certainty:

- **Category:** `pch/rawmakase` has no topics; it fell into `Utility` until
  we added "lightroom" to the word list. `devmobasa/wayscriber` fell into
  `System` because of the `wayland` topic.
- **Asset:** `omacom/try-omarchy-windows` published `vmlinuz-linux` and a Windows
  zip, and both were accepted as a Linux binary.
- **Executable:** the RAWmakase tarball ships `usr/bin/rawmakase` (script) and
  `usr/lib/rawmakase/rawmakase` (ELF). We picked the right one by name, but by luck.
- **Terminal:** `Terminal=true` is inferred only from the `cli`/`tui` topics.
- **Icon:** in `erans/hyprmon`, the file `hyprmon.png` matches the repository
  name and becomes the icon, but it is a rectangular screenshot of the app.

Each of these fixes requires changing rules and bumping `index.Version`.

## Proposal

Read, if it exists, an `omastore.toml` at the repository root (through the Trees API,
which we already list). Everything optional; whatever is missing stays heuristic.

```toml
name = "RAWmakase"
summary = "Free alternative to Lightroom"
categories = ["Graphics", "Photography"]   # freedesktop
icon = "packaging/rawmakase.svg"
screenshots = ["docs/images/screenshot.png"]
terminal = false

[linux.x86_64]
asset = "rawmakase-{version}-x86_64-linux.tar.gz"   # pattern with {version}
exec = "usr/bin/rawmakase"                           # inside the package
[linux.aarch64]
asset = "rawmakase-{version}-aarch64-linux.tar.gz"
exec = "usr/bin/rawmakase"
```

Rules:

- The manifest **never** widens permissions: `exec` still has to stay
  inside the extracted directory; paths go through the same validations.
- Manifest values take priority over the heuristics, but the text
  fields go through the same `.desktop` escaping.
- Publish a guide (Phase 9) and a validator: `omastore lint-manifest <dir>`.

## Cost

Small to medium: a TOML parser (`github.com/pelletier/go-toml/v2` or
`BurntSushi/toml`), a column/JSON in the database, integration in `extract()` and
`SelectAsset()`, and tests. Bump `index.Version`.

## Risks

- Adoption depends on authors; that is why the manifest is optional.
- An outdated manifest (old asset name) leaves the app not installable.
  Mitigation: if the pattern matches no asset, fall back to the heuristics and
  log a warning.
