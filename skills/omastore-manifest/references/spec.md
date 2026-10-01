# `omastore.toml` specification

Source of truth: `backend/internal/manifest` in the OmaStore repository.
This file summarizes the rules that `omastore lint-manifest` applies.

## Fields

| Field | Type | Rule |
|---|---|---|
| `kind` | string | `"app"` (default). `"plugin"` and `"theme"` are accepted but **not indexed**; other values are an error. |
| `name` | string | Display name. Whitespace normalized; truncated at 80 characters (warning). |
| `summary` | string | One-sentence summary. Truncated at 300 characters (warning). |
| `categories` | list | freedesktop categories (table below). The first **main** one becomes the store category; all of them go to the `.desktop`. Max. 4. |
| `icon` | string | Path relative to the repository, `.png` or `.svg`. |
| `screenshots` | list | Relative paths (images) or `https://` URLs. Max. 8. |
| `terminal` | bool | `true` for CLI/TUI: the launcher opens in a terminal. |
| `[linux.<arch>]` | table | `<arch>`: `x86_64`/`amd64`/`x64` or `aarch64`/`arm64`. |
| `linux.<arch>.asset` | string | Release asset name. Placeholders: `{version}` (tag without `v`), `{tag}` (tag as is), `*` (any sequence). No `/`. Cannot be just `*`. |
| `linux.<arch>.exec` | string | Path of the executable **inside the extracted package**, relative, without `..`. Accepts `{version}` and `{tag}` (e.g. `app-{version}-x86_64-linux/bin/app`). A symlink pointing outside the package is refused. |

Any other field is an error in the lint (`strict`), but is ignored when indexing.

## freedesktop categories

Main (at least one, otherwise the app does not show up in menus):
`AudioVideo`, `Audio`, `Video`, `Development`, `Education`, `Game`,
`Graphics`, `Network`, `Office`, `Science`, `Settings`, `System`, `Utility`.

Additional ones (combine with a main one): any registered in the
[Desktop Menu Specification](https://specifications.freedesktop.org/menu-spec/latest/category-registry.html)
(appendix A), such as `Photography`, `RasterGraphics`, `VectorGraphics`,
`2DGraphics`, `Viewer`, `Recorder`, `AudioVideoEditing`, `Player`, `TextEditor`,
`IDE`, `TerminalEmulator`, `FileManager`, `Monitor`, `Calendar`, `Email`, `Chat`,
`WebBrowser`, `Emulator`, `PackageManager`, `Security`, `Archiving`, `Calculator`.
Names are case-sensitive. Your own extensions start with `X-` (`X-Omarchy`).
Anything else is an error, as in `desktop-file-validate`; the reserved
`Screensaver`, `TrayIcon`, `Applet` and `Shell` are not accepted.

## How the store picks the asset and the executable

1. If `[linux.<machine arch>].asset` exists and matches an asset of the latest
   stable release, that is the one. The name does not need to follow any convention.
2. Otherwise, heuristics: architecture in the name (`x86_64`/`amd64`, `aarch64`/`arm64`),
   formats in the order `.tar.gz`/`.tar.xz`/`.tar.zst`/`.tar.bz2` > `.zip` >
   plain binary > `.AppImage` > `.pkg.tar.zst`. `.deb`, `.rpm`, sources and
   builds for other systems are ignored.
3. Executable: the manifest's `exec`, if it exists in the package; otherwise the ELF
   file (or script with a shebang and the executable bit) named after the repository,
   preferably in `bin/` or `usr/bin/`.

## Examples

Minimal (everything inferred):

```toml
kind = "app"
```

Terminal app with a release following the pattern:

```toml
kind = "app"
categories = ["System", "Monitor"]
icon = "assets/icon.svg"
terminal = true
```

Release with several files per architecture and a non-standard executable:

```toml
kind = "app"
name = "RAWmakase"
categories = ["Graphics", "Photography"]
icon = "packaging/icons/rawmakase.svg"
screenshots = ["docs/images/screenshot.png"]

[linux.x86_64]
asset = "rawmakase-{version}-x86_64-linux.tar.gz"
exec = "usr/bin/rawmakase"
```
