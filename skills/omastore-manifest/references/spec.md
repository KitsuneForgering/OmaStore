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
| `[services.<id>]` | table | Optional managed user service. `<id>` uses letters, digits, `_` or `-` and starts with a letter. Multiple tables are allowed. |
| `services.<id>.type` | string | Required: `"systemd-user"`. Unknown types fail validation. |
| `services.<id>.unit` | string | Required `.service` unit name, with letters, digits, `_`, `-` and `.` only. No path or system unit. |
| `services.<id>.exec` | string | Required executable path inside the extracted release. Plain relative path without placeholders, traversal or control characters. It must be executable. |
| `services.<id>.enable` | bool | Enable at login if true. Default false. |
| `services.<id>.start` | bool | Start in this user session if true. Default false. |
| `services.<id>.restart` | string | `"no"` (default) or `"on-failure"`; controls systemd's restart policy. |

Any other field is an error in the lint (`strict`), but is ignored when indexing.
Unknown fields inside `services` and invalid service definitions fail indexing;
they are never partially applied. Manifests without `services` retain their
existing behavior.

## User service lifecycle and ownership

OmaStore generates a user unit in `$XDG_CONFIG_HOME/systemd/user` (normally
`~/.config/systemd/user`) that runs the declared release executable at its
versioned path. The packaged `.service` file, if any, is not copied or run.
The unit is made available with `systemctl --user daemon-reload`, then enabled
and/or started only when requested. No sudo, system manager, shell hook or
manifest command line is used. A working systemd user manager is required.

The installation record stores the unit path, version, exact file digest,
and whether OmaStore enabled or started it. A pre-existing unit or a changed
managed unit causes a conflict instead of being overwritten. On update,
OmaStore replaces its own unit and restarts it only if it was active; an
inactive service stays inactive unless the new declaration requests `start`.
Removed declarations stop/disable only the state OmaStore created. Uninstall
stops/disables owned state, removes the owned unit, reloads the user manager,
and then removes app files. An externally enabled or started service is a
conflict until the user changes that state. A failed setup rolls back the
file transaction and attempts to restore the previous user-service state;
an incomplete removal remains in the installation record for retry.

Service binaries are code from the release and run with the user's own
privileges. Authors should publish checksums and review what their service
does. Unit names and paths are validated, and there are no arbitrary install
or uninstall commands.

Example for a release that contains `usr/bin/omakade-sessiond` (the same
pattern works for any app; Omakade's packaged `/usr/bin` unit cannot be used
directly in a home installation):

```toml
[services.sessiond]
type = "systemd-user"
unit = "omakade-sessiond.service"
exec = "usr/bin/omakade-sessiond"
enable = true
start = true
restart = "on-failure"
```

This declaration belongs in the app repository's `omastore.toml`; adding
support to OmaStore alone does not change an already published manifest.
Apps with other compiled-in `/usr` resource paths must separately support
user-local installation for all features to work.

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
   If it matches none, the app is not installable on that architecture: the store
   does not guess. Declaring the asset is how to keep it from installing, say, a
   plugin backend shipped beside the app before there is a build of the app itself.
2. Without a declared asset for the machine's architecture, heuristics: architecture in the name (`x86_64`/`amd64`, `aarch64`/`arm64`),
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
