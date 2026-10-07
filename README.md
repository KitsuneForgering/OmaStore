<p align="center"><img src="packaging/desktop/omastore-mark.svg" width="112" alt="OmaStore icon"></p>

<h1 align="center">OmaStore</h1>

<p align="center">Standalone apps for Omarchy, all in one place.</p>

<p align="center">
  <a href="https://github.com/KitsuneForgering/OmaStore/actions/workflows/ci.yml"><img src="https://github.com/KitsuneForgering/OmaStore/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/KitsuneForgering/OmaStore/releases/latest"><img src="https://img.shields.io/github/v/release/KitsuneForgering/OmaStore?sort=semver" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/KitsuneForgering/OmaStore" alt="MIT license"></a>
</p>

<p align="center"><a href="#install">Install</a> · <a href="#using-it">Use</a> · <a href="#for-app-authors">Publish an app</a> · <a href="#how-it-works">How it works</a></p>

OmaStore is an app store for [Omarchy](https://omarchy.org). It finds community
apps published on GitHub, shows what each one does, and installs the app's
latest release into your account with a menu entry, without `sudo`. It keeps
the apps up to date, lets you go back to the previous version, and removes
them cleanly.

<p align="center"><img src="docs/screenshots/demo.gif" width="880" alt="OmaStore demo: searching for &quot;virtual machine&quot;, opening OmaVM's page, installing it with one click and launching it"></p>

| Discover | See the details before installing |
|---|---|
| ![OmaStore catalog screen](docs/screenshots/catalog.png) | ![An app's detail screen in OmaStore](docs/screenshots/detail.png) |

> **Early days:** an app joins the catalog when its author adds an
> `omastore.toml` to the repository, so the catalog is still small.
> [Publishing takes three steps](#for-app-authors).

## What you get

- **One place to find apps.** Search, categories, screenshots, and each app's
  README, release notes, changelog and similar apps.
- **Install without touching the system.** Apps live in
  `~/.local/share/omastore/apps/`, with a command in `~/.local/bin` and a menu
  entry. Uninstalling removes exactly what was installed.
- **Apps that stay current.** Updates whose download can be verified install
  on their own after `omarchy update`, when you open the store (at most every
  12 hours) and from the daily timer of a package install. You get a
  notification of what changed. Updates without a checksum wait for you. Turn
  it off in **Settings**.
- **Updates you can undo.** An update keeps the version it replaced: an open
  app keeps running, and **Go back** returns to the previous version without
  downloading anything. Each app shows its install and update history.
- **Know what you install.** Every download is checked against GitHub's sha256
  or the release's checksum; a file with neither needs your confirmation. When
  the author attests the build, the app page shows **Built by this
  repository's GitHub Actions** (verified with Sigstore). In **Settings** you
  can allow only apps that have this provenance.
- **The whole app, not only the binary.** The system packages an app declares
  in its `PKGBUILD`, and the libraries its executable needs and your system
  lacks, are listed on its page. The missing ones from the pacman repositories
  install with one click (pacman asks for your password; AUR packages are never
  installed for you). Apps with a background service get their `systemd --user`
  unit set up, kept across updates and removed with the app.
- **Problem apps leave the catalog.** **Report this app to OmaStore** on an
  app's page opens a form; apps the maintainers confirm as harmful or
  misleading are blocked for everyone: they leave the catalog, are never
  installed or updated, and people who have one see why. Stars on GitHub are
  the ratings (the ★ button on each page).
- **Feels like Omarchy.** Follows the active theme's colors and fonts and
  switches with it, works from the keyboard, and speaks English and Brazilian
  Portuguese (it follows the system language).

## Install

On Omarchy (or Arch), without `sudo`:

```sh
curl -fsSLO https://github.com/KitsuneForgering/OmaStore/releases/latest/download/install.sh
sh install.sh
```

Then open **OmaStore** from the menu. The first run builds the catalog from
GitHub, which takes a minute.

**Recommended:** `gh auth login` (or `export GITHUB_TOKEN=...`). Without a
token, GitHub allows 60 requests per hour, which is not enough to index the
whole catalog, and starring apps needs it.

What the script does:

- downloads the latest release for Linux x86_64, checks it against the
  published SHA-256 (the check covers the tarball, not `install.sh` itself) and
  installs `omastore`, `omastored` and `omastore-gui` into your account;
- needs `curl`, `tar`, `sha256sum` and Qt 6 (`qt6-base`, `qt6-declarative`,
  `qt6-svg`);
- on Omarchy, adds a hook so `omarchy update` also updates your OmaStore apps
  (`--no-hooks` skips it);
- copies the [skills for app authors](skills/README.md) into each coding agent
  you have (`--no-skills` skips it). An older copy of the same skill is moved
  to `~/.local/share/omastore/skill-backups/` and replaced by the current one.

OmaStore updates itself: the sidebar offers **Update OmaStore** when a release
is out (or run `omastore self-update`). To remove it, with its skills and hook
(your catalog and the apps you installed stay):

```sh
sh install.sh --uninstall
```

<details>
<summary>Other ways to install</summary>

**pacman:** download the `PKGBUILD` attached to the
[latest release](https://github.com/KitsuneForgering/OmaStore/releases/latest)
(package `omastore-bin`) and run `makepkg -si`. To build `master` instead:

```sh
git clone https://github.com/KitsuneForgering/OmaStore
cd OmaStore/packaging/arch
makepkg -si
systemctl --user enable --now omastored.socket       # optional: on-demand daemon
systemctl --user enable --now omastore-index.timer   # optional: daily catalog refresh and updates
```

A package cannot write to your home, so link the Omarchy hook yourself:
`ln -s /usr/share/omastore/omarchy/omastore.hook ~/.config/omarchy/hooks/post-update.d/`.
Without the socket unit, the interface starts the daemon when it needs it.

**From source:** with the [development requirements](#development),

```sh
git clone https://github.com/KitsuneForgering/OmaStore
cd OmaStore
make dist VERSION=v0.4.0-dev
sh packaging/install.sh dist/omastore-0.4.0-dev-x86_64-linux.tar.gz
```

`make release && sudo make install` (default `PREFIX=/usr/local`) also works
and takes over from a per-user installation.

</details>

## Using it

Open an app's page, press **Install**, then **Open**. The **Installed** page
lists your apps and their updates; **Settings** in the sidebar turns automatic
updates and the provenance requirement on or off.

| Key | Action |
|---|---|
| `/` | search |
| `h` `j` `k` `l` or arrows, `Enter` | move through the apps, open one |
| `i` / `u` | install / update the open app |
| `Esc` | go back |
| `Ctrl+R` | look for new apps and versions |
| `?` or `F1` | list every shortcut |

Links of the form `omastore://owner/repo` (in a README, a chat) open the app's
page; a window already open shows it instead of opening another.

Everything also works from the command line:

```sh
omastore list --category Graphics
omastore show pch/rawmakase       # details, files and their build provenance
omastore install pch/rawmakase
omastore update                   # every installed app (or the ones you name)
omastore rollback pch/rawmakase   # back to the version the last update replaced
omastore uninstall pch/rawmakase  # refuses while it runs; --force removes it anyway
omastore deps pch/rawmakase       # system dependencies; --install installs them
omastore auto-update off          # automatic updates (on by default)
omastore require-provenance on    # only attested builds (off by default)
omastore help                     # every command
```

## For app authors

Your app joins the catalog in three steps:

1. **Add an `omastore.toml`** at the root of the default branch. It can be
   empty: fields fix what the store would guess wrong (icon, executable, which
   file to install) and declare a background service if the app has one.
2. **Publish a release with a Linux binary**, a tarball per architecture,
   ideally built by a GitHub Actions workflow with
   `actions/attest-build-provenance` so users see that your repository built it.
3. **Check it:** open **Publish your app** in the sidebar, or run
   `omastore check owner/repo`. It shows what the store sees, what blocks it,
   and a ready-to-commit `omastore.toml`. Nothing is installed.

![Checking a repository in OmaStore](docs/screenshots/publish.png)

Using a coding agent? The [author skills](skills/README.md) write the manifest,
set up the release workflow and audit the app with OmaStore itself. The full
rules are in [`docs/authors.md`](docs/authors.md).

## How it works

```
omastore-gui (C++/Qt Quick)  ── JSON-RPC 2.0 / Unix socket ──▶  omastored (Go)
  presentation only                                               indexer, SQLite cache,
                                                                  installer, image cache
```

- **Discovery:** repositories with an app `omastore.toml`, found through GitHub
  code search, the `omarchy` topic and curated lists. An SQLite cache with
  conditional requests means a refresh only reprocesses what changed.
- **Downloads** come only from the repository's latest stable release, never
  from a URL in the manifest. The digest is checked, and build attestations are
  verified with Sigstore and must be signed by a workflow of the app's own
  repository.
- **Installation** never runs anything from the package, guards against path
  traversal and symlinks leaving the app's folder, never overwrites files that
  are not OmaStore's or shadows system commands, and is undone if something
  fails midway. System packages are the only thing installed as root: only when
  you accept, through `pkexec pacman -S --needed`, from the configured
  repositories.
- **The interface never touches the network**: images and data come through
  the daemon. Protocol: [`docs/ipc.md`](docs/ipc.md). Ideas under discussion:
  [`docs/ideas/`](docs/ideas/README.md).

## Development

Requirements: Go 1.26+, a C compiler (CGO, for SQLite), Qt 6.5+ (`qt6-base`,
`qt6-declarative`, `qt6-svg`; `qt6-tools` for the translations), CMake and Ninja.

```sh
make            # builds everything (bin/ and frontend/build/)
make check      # gofmt + vet + backend tests (what CI runs)
make test       # all tests, backend and frontend
make run-gui    # opens the interface with the daemon from bin/
make help       # every target
```

Tests do not access the network: the GitHub API is simulated with `httptest`,
the daemon with a fake `QLocalServer`, and build attestations with a recorded
bundle and trust root.

## Contributing

Bug reports, ideas, translations and pull requests are welcome: see
[CONTRIBUTING.md](CONTRIBUTING.md), and [CHANGELOG.md](CHANGELOG.md) for what
changed in each release. Report security problems privately, as described in
[SECURITY.md](SECURITY.md).

## License

MIT — see [LICENSE](LICENSE).
