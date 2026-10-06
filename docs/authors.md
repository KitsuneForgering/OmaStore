# Publishing an app on OmaStore

OmaStore indexes **standalone** apps for Omarchy (not plugins or themes)
straight from GitHub. There is no sign-up, but there is one requirement: **the
repository must have an `omastore.toml` at its root** (section 6). Without it,
the repository is not added to the catalog, even with the `omarchy` topic. The
file may even be empty: its presence is the signal that you want the app in the store.

> Shortcut: the skills in [`skills/`](../skills/) do this with your coding
> agent (Claude Code, Codex, OpenCode, Copilot, Gemini, Cursor, Pi, Hermes…):
> `omastore-manifest` writes the manifest, `omastore-release` sets up the
> releases and `omastore-check` audits everything. OmaStore's `install.sh`
> already puts them in every installed agent's skills directory; the pacman
> package (and `make install`) only ships them in `/usr/share/omastore/skills`,
> so copy them from there.

## 1. Be found

- Have `omastore.toml` at the root of the default branch: the store finds
  repositories through GitHub code search for that file, even without a
  topic.

- Add the **`omarchy`** topic to the repository (Settings → Topics).
- Add topics that describe the app; they define its category in the catalog:

  | Category | Example topics |
  |---|---|
  | Graphics | `photo`, `image`, `design`, `drawing`, `annotation` |
  | AudioVideo | `audio`, `music`, `video`, `voice`, `speech`, `recorder` |
  | Development | `developer-tools`, `editor`, `git`, `database` |
  | Network | `email`, `browser`, `chat`, `vpn`, `bluetooth` |
  | Office | `notes`, `calendar`, `pdf`, `productivity` |
  | System | `virtualization`, `monitor`, `hyprland`, `backup` |
  | Game | `game`, `emulator` |

  Without topics, the category is inferred from the description and falls back
  to *Utility* if nothing matches.
- The `cli`, `tui` or `terminal` topics make the launcher open in a terminal
  (on Omarchy, through `omarchy-launch-or-focus-tui`).
- The body of your latest release is shown on the app's page as "What's new":
  write release notes for people, not only a list of commits.
- Archived repositories are left out; forks do not show up in the topic search.

## 2. Publish a release with a Linux binary

Only the **latest stable release** (not a pre-release) is considered. Name the
assets with the system and architecture:

```
myapp-1.2.0-x86_64-linux.tar.gz
myapp-1.2.0-aarch64-linux.tar.gz
```

- Recognized architectures: `x86_64`/`amd64`/`x64` and `aarch64`/`arm64`.
- Formats, in order of preference:
  1. `.tar.gz`, `.tar.xz`, `.tar.zst`, `.tar.bz2`;
  2. `.zip` (needs `linux` or the architecture in the name);
  3. plain binary (same);
  4. `.AppImage`;
  5. `.pkg.tar.zst`.
- Ignored: `.deb`, `.rpm`, `.dmg`, `.exe`, `.msi`, source tarballs
  (`source`, `src`) and assets with `darwin`, `macos`, `windows` etc. in the name.

**Prefer a portable tarball.** From `.pkg.tar.zst` packages only the
contents of `usr/` are extracted (`.INSTALL` scripts never run). Since
everything lives in `$HOME`, not in `/usr`, programs that look for data at
absolute paths such as `/usr/share/myapp` do not work.

### The executable

Inside the package, the main executable is chosen by, in this order:

1. having the same name as the repository;
2. being in `bin/` or `usr/bin/`;
3. being ELF (not a script).

`.so` libraries and files in `lib/` and `share/` are ignored. Scripts
need the executable bit and a shebang.

The command installed in `~/.local/bin` is a small launcher that calls the
absolute path of your binary, so `$0`/`argv[0]` point to the real file and
paths relative to it (`$(dirname "$0")/../lib`) work.

The command name cannot collide with a system command (`/usr/bin/...`):
the installation is refused so it does not shadow, for example, `ls` or `sudo`.

## 3. Publish checksums

OmaStore checks the sha256 that GitHub itself computes for each asset
(the `digest` field). If you prefer to publish your own, any of these works:

- `myapp-1.2.0-x86_64-linux.tar.gz.sha256` (just the hash, or `hash  name`);
- `checksums.txt` / `SHA256SUMS` in `sha256sum` format, or the BSD format
  `SHA256 (name) = hash`.

If the hash does not match, nothing is installed. Releases without any checksum
ask the user for confirmation.

## 4. Icon and screenshots

- Publish a stable GitHub release first. Until one exists, OmaStore does not
  fetch the README or repository images, so the app has no preview.
- **Icon:** an `icon.svg`/`icon.png` at the root, in `assets/`, `icons/`,
  `resources/` or `data/`. Names such as `<repo>.svg`, `logo.svg` and
  `app-icon.png` also work, as does the `icons/hicolor/<size>/apps/` layout. SVG is
  preferred; a PNG of at least 256×256 is resized for the theme.
- **Screenshots:** the README images (badges are ignored) and files in
  folders or with names containing `screenshot`, `preview` or `demo`. Up to 8 are
  shown in the app page's carousel. README images are not repeated inside the
  displayed README text.

For predictable results, commit the images and name them in the manifest:

```text
myapp/
  omastore.toml
  assets/icon.svg
  screenshots/main.png
```

```toml
icon = "assets/icon.svg"
screenshots = ["screenshots/main.png"]
```

Declared screenshots replace the automatically found ones. They may be paths
relative to the repository or `https://` URLs. Each image must be a supported
PNG, JPEG, WebP, GIF or SVG and no larger than 15 MiB; otherwise it cannot be
displayed. The carousel keeps the image's aspect ratio.

## 5. Description

- The summary is the repository description (the short sentence at the top on GitHub); if
  it is empty, the first paragraph of the README.
- The display name is the repository's, with the spelling of the README's first
  heading when they match (e.g. repo `omaphoto`, heading `# OmaPhoto`).
- The README is shown on the app page, with relative links converted to
  absolute ones.
- A root `CHANGELOG.md` is shown after the README, with relative links converted
  to absolute ones. Repositories without one need no extra file request.

## 5b. System dependencies

If your app needs system packages (Qt, GTK, ffmpeg...), keep a `PKGBUILD`, or
better its `.SRCINFO`, in the repository: at the root or up to three
directories deep (`packaging/arch/`, `aur/`...). OmaStore reads its `depends`
and `optdepends` (and `depends_x86_64`/`depends_aarch64`), shows them on the
app page and offers to install the missing ones with pacman after the app is
installed.

- The `PKGBUILD` is **never run**: only literal arrays are read. An element
  with `$var`, `$(...)` or a glob is dropped, so prefer committing a
  `.SRCINFO` (`makepkg --printsrcinfo > .SRCINFO`), which has the final values.
- With several files, a `.SRCINFO` wins over the `PKGBUILD` in the same
  directory, a directory with `bin` in its name (your `-bin` package) wins over
  others, and shallower wins over deeper. For split packages, the one named
  after the repository is used (or `<repo>-*`).
- `makedepends` and `checkdepends` are ignored: the store installs your
  release binary, it does not build it.
- Packages only in the AUR are shown as unavailable and never installed by the
  store; users install them themselves.

## 6. The `omastore.toml` manifest (required)

The file must exist at the root of the default branch. All fields are
optional; whatever is not declared is inferred by the rules above. Declare
what the heuristics would get wrong (icon, executable, asset).

```toml
kind = "app"                               # default; "plugin" and "theme" are not indexed
name = "RAWmakase"                         # display name
summary = "Free alternative to Lightroom"  # summary (up to 300 characters)
categories = ["Graphics", "Photography"]   # freedesktop; the 1st main one becomes the category
icon = "packaging/rawmakase.svg"           # PNG or SVG, path in the repository
screenshots = ["docs/images/screenshot.png", "https://example.com/screen.png"]
terminal = false                           # open in a terminal?

[linux.x86_64]                             # or aarch64
asset = "rawmakase-{version}-x86_64-linux.tar.gz"   # {version} = tag without "v"; {tag}; *
exec = "usr/bin/rawmakase"                 # executable inside the package ({version}/{tag} work here too)
```

Rules:

- **Paths:** must be relative and stay inside the repository (or the
  package, for `exec`). An `exec` that is a symlink pointing outside the package
  is refused.
- **Asset:** the declared one takes priority over the automatic choice and is accepted
  even with a name the heuristics would not understand. If the pattern does not match
  any asset of the release, the app is not installable on that architecture (the store
  does not guess, so it never installs a file you did not mean). Architectures the
  manifest does not declare use the automatic choice.
- **Invalid fields:** are ignored, with a warning; the rest of the manifest
  still applies. A file that is not valid TOML removes the repository from the
  catalog, so validate it before publishing.
- **Apps only:** `kind = "plugin"` or `"theme"` (or any value other than
  `app`) keeps the repository out of the store.

### User services

An app with a background part (a daemon, a session helper) declares it and
OmaStore generates a `systemd --user` unit for it in
`~/.config/systemd/user`, pointing at the executable of the installed version:

```toml
[services.sessiond]                        # ID: letters, digits, _ or -
type = "systemd-user"                      # the only type
unit = "myapp-sessiond.service"            # unit name, no path
exec = "usr/bin/myapp-sessiond"            # executable inside the package
enable = true                              # start at login (default false)
start = true                               # start now, after installing (default false)
restart = "on-failure"                     # or "no" (default)
```

- The packaged `.service` file, if any, is neither copied nor run: its
  `/usr/bin` path would be wrong for an installation in `$HOME`. There are no
  install or uninstall commands.
- Unlike other fields, an invalid service declaration (unknown field or type,
  bad unit name, `exec` leaving the package) **keeps the app out of the
  catalog** until it is fixed: a service is never half applied.
- A unit with the same name that OmaStore did not create is a conflict, not
  overwritten. An update restarts the service only if it was running;
  uninstalling stops and disables only what OmaStore enabled or started.
- The service runs with the user's privileges and needs a working user
  manager (`systemctl --user`).

Validate before publishing:

```sh
omastore lint-manifest .        # in the repository directory
```

`lint` mode is strict: a misnamed field (e.g. `icone`) is an error.
Indexing is lenient, so that fields from future versions do not break anything.

## Testing before publishing

The quickest way is the **Publish your app** page in OmaStore (or
`omastore-gui --check you/myapp`): type the repository and it lists what passes,
what fails and how to fix it, previews the catalog card and suggests an
`omastore.toml` built from your latest release. Tick *Test an omastore.toml
before pushing it* to try a local file. The same report is on the command line:

```sh
omastore check you/myapp                        # published manifest
omastore check --manifest omastore.toml you/myapp   # local file, before pushing
omastore check --json you/myapp                 # for CI; exit code 1 = not compatible
```

The check writes nothing to your catalog. To see the full path, including a
real installation:

```sh
omastore index you/myapp        # indexes only your repository
omastore show you/myapp         # shows what was understood (assets, icon, category)
omastore install you/myapp
```

If something was detected wrongly, declare it in `omastore.toml` (section 6) or open
an issue.
