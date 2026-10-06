# OmaStore IPC protocol

The frontend talks to `omastored` over **JSON-RPC 2.0** on a Unix socket at
`$XDG_RUNTIME_DIR/omastore.sock` (mode `0600`). Implementation:
`backend/internal/rpc/`. Protocol version: **2** (`daemon.hello`).

The version goes up whenever a method or a DTO changes. On connect the frontend
calls `daemon.hello` and compares `protocol` (and the `methods` it needs) with
what it was built for; an older daemon gets a banner with a way to restart it
(`self.restart`, when the daemon has it). Daemons before protocol 2 answer
without `methods`.

## Framing

- Each message is **one JSON object on one line**, terminated by `\n` (NDJSON).
  Batches (arrays) are not supported.
- Maximum size of a received message: 1 MiB. Above that the server
  answers `-32600` and closes the connection.
- Requests from the same client are processed **in parallel**; match
  responses by `id`, not by order.
- Server messages without an `id` are **notifications** and go to all
  connected clients.

```json
→ {"jsonrpc":"2.0","id":1,"method":"catalog.get","params":{"repo":"pch/rawmakase"}}
← {"jsonrpc":"2.0","id":1,"result":{"repo":"pch/rawmakase","name":"RAWmakase", ...}}
← {"jsonrpc":"2.0","method":"job.progress","params":{"id":"job-3","stage":"download","done":512,"total":1024, ...}}
```

Missing `params` is the same as `{}`. Unknown fields in `params` are an error
(`-32602`), to catch typos in the frontend.

## Methods

| Method | Parameters | Result |
|---|---|---|
| `daemon.hello` | — | `{name, protocol, methods}`: `methods` lists every method this daemon answers |
| `catalog.list` | `{category?, query?, installed?, all?, limit?, offset?}` | `AppItem[]`: by score; with `query`, by relevance |
| `catalog.get` | `{repo}` | `AppDetail` |
| `catalog.similar` | `{repo, limit?}` (default 8, max. 50) | most similar `AppItem[]`, installable only |
| `catalog.categories` | — | `[{name, count}]` |
| `installs.list` | — | `InstallInfo[]` |
| `index.start` | `{force?, repos?}` | `Job` (kind `index`) |
| `install.start` | `{repo, allowUnverified?}` | `Job` (kind `install`) |
| `update.start` | `{repo, allowUnverified?}` | `Job` (kind `update`) |
| `install.uninstall` | `{repo, force?}` | `{}` (synchronous); `-32015` while the app runs, unless `force` |
| `install.rollback` | `{repo}` | `InstallInfo` (synchronous): back to the version the last update replaced |
| `jobs.list` | — | `Job[]` (running ones and the last 50 finished) |
| `jobs.cancel` | `{job}` | `{}` |
| `image.get` | `{url}` | `{path}`: local path of the cached image |
| `author.check` | `{repo, manifest?}` | `CheckReport` (synchronous; writes nothing to the catalog) |
| `star.get` | `{repo}` | `{starred}`: whether the GitHub user starred the repository |
| `star.set` | `{repo, starred}` | `{starred, stars}`: stars/unstars it on GitHub; `stars` is the catalog's new count |
| `deps.check` | `{repo}` | `DepsReport` (synchronous; runs no privileged command) |
| `deps.install` | `{repo}` | `Job` (kind `deps`) |
| `self.status` | — | `SelfInfo` (synchronous; the latest release is cached for 30 min) |
| `self.update` | — | `Job` (kind `self`): installs OmaStore's latest release over this installation |
| `self.restart` | — | `{}`; the daemon exits right after replying (`-32002` while jobs run) |

`repo` is always `"owner/repo"`. `all: true` includes apps without an installable binary.

**Files without a checksum.** `install.start` and `update.start` refuse, before
downloading, a file that has neither a GitHub digest nor a published checksum
(`-32017`), unless `allowUnverified: true`: the user confirmed that file. The
same rule holds for the CLI (`--allow-unverified`). `AppDetail.selectedAsset`
is the file an install would download on this machine, so the interface asks
about that file and not about the release as a whole.

**Apps in use.** The daemon reads `/proc` (exe, cwd and mapped files; nothing
is run or signaled) to find processes running from an app's directory.
`install.uninstall` refuses while there are any (`-32015`, the message ends
with `name (pid), …`), unless `force`. An update keeps the version it replaces
on disk and recorded (`InstallInfo.previousVersion`): a running copy keeps its
files, and `install.rollback` points the launcher and the menu entry back to
it (calling it again returns to the newer one; nothing is downloaded). The
version before that one is removed by the update, or kept for a later update
while a process still runs from it.

OmaStore updates itself only when `install.sh` installed it (`mode: "self"`:
`$XDG_DATA_HOME/omastore/self/<version>/` with links in `~/.local/bin`). The
release tarball must have a checksum (API digest or `.sha256`); it is extracted
into a new version directory, then the links, the menu entry, the icon and the
Claude Code skill copies `install.sh` made are switched over, all or nothing.
The running version stays on disk until the next update. To finish, the
frontend calls `self.restart` and starts the `gui` of the job result, which
starts the new daemon. A package (`mode: "package"`) is updated by pacman and a
development build (`"dev"`) is never compared with releases.

Search and recommendation (`backend/internal/search`) are local and
deterministic: the same query over the same catalog always returns the
same order.
- **Search:** BM25 with per-field weights (name > topics/category > summary >
  README). Accepts prefixes ("rawmak") and corrects one typo
  ("lightrom") when the term does not exist in the catalog. Ignores accents,
  case, plurals and -ing/-ed, and translates common Portuguese terms
  ("editor de fotos", "voz para texto") into the English of the descriptions.
- **Similar:** cosine similarity between TF-IDF vectors of topics,
  name, summary and category, with the README at a low weight.
`image.get` only accepts `https://` and validates the content as an image; the frontend
never accesses the network directly.

`author.check` runs the indexing rules for one repository and tells its author
what the store understands and what to fix. `manifest`, when present (even
`""`), is used instead of the published `omastore.toml`, to test a file before
pushing it. Costs a handful of GitHub requests; a rate limit is error `-32005`.

`star.get`/`star.set` act as the owner of the daemon's GitHub token
(`GITHUB_TOKEN`, `GH_TOKEN` or `gh auth token`); without one, or with a token
that may not star (fine-grained without the "Starring" permission), they fail
with `-32011`. `star.set` sends `catalog.changed` with `{repo}`.

`deps.check` reads the system dependencies stored at index time from the app's
`PKGBUILD` or `.SRCINFO` (`depends`, `optdepends` and their `_<arch>` variants;
the file is parsed, never run) and asks pacman which are missing (`pacman -T`)
and which repository package satisfies each (`pacman -Sddp`). `deps.install`
installs every missing one that a pacman repository has, depends and
optdepends alike, with `pkexec pacman -S --needed`: polkit asks for the
administrator password. Dependencies found in no repository (AUR) are never
installed; they stay `unavailable` in the report, and when nothing else was
missing the job fails with `-32014`. Only one `deps` job runs at a time
(pacman has a single lock). When pacman cannot download a package the local
database lists (the mirrors moved on since the last system update), the job
fails with `-32018`: the fix is `omarchy update`, never `pacman -Sy`.

Once the app is installed, `deps.check` also reads the installed executable's
ELF headers (`debug/elf`; never run, not even `ldd`) and lists in `libraries`
the shared libraries it needs that neither the app's files nor the system's
library directories (`/etc/ld.so.conf`) have. The package that ships each one
comes from pacman's file database (`pacman -F`) when it exists (`pacman -Fy`
creates it); those packages join `toInstall`. A script launcher (Python, shell)
is not checked. `wrongArch` names the machine an executable was built for when
it is not this one.

### Types

```ts
AppItem {
  repo, name, summary, iconUrl, category: string
  screenshots: string[]            // never null
  stars: number, score: number, installable: boolean
  latestVersion: string            // tag of the latest release
  installedVersion: string         // "" if not installed
  updateAvailable: boolean
}
AppDetail extends AppItem {
  readme: string                   // markdown with URLs already absolute
  changelog: string                // root CHANGELOG.md, markdown with URLs absolute
  releaseNotes: string             // markdown body of the latest release, as the
                                   // author wrote it (up to ~16 KiB); "" if none
  description, license, htmlUrl: string
  topics: string[]
  pushedAt?, indexedAt?: string    // RFC 3339
  assets: AssetInfo[]
  selectedAsset: AssetInfo | null  // the file an install downloads on this machine
  install: InstallInfo | null
}
AssetInfo {
  name, arch, format: string, size: number
  checksum: "digest" | "file" | "" // what can check the download (GitHub's digest,
                                   // a checksum file in the release, nothing);
                                   // availability only: the check runs on install
}
InstallInfo {
  repo, version, installedAt, execPath, desktopPath: string
  previousVersion: string          // what install.rollback goes back to ("" if none)
  history?: InstallEventInfo[]     // catalog.get: newest 20 transitions, newest first
  broken: boolean                  // the executable is gone (removed outside OmaStore):
                                   // install.start again repairs it
  services?: ServiceInfo[]         // user units OmaStore manages for this app
}
ServiceInfo {
  type: "systemd-user"
  unit, path, exec, version: string  // path: the generated unit file
  digest: string                   // sha256 of the unit as written
  enable, start: boolean           // what the manifest asked for
  enabledByStore, startedByStore: boolean   // state OmaStore created (and removes)
  restart: string                  // "", "no" or "on-failure"
}
InstallEventInfo {
  action: "install" | "update" | "rollback"
  fromVersion, toVersion: string    // fromVersion is empty on initial install
  at: string                        // RFC 3339
}
SelfInfo {
  mode: "self" | "package" | "dev"
  version: string                  // running version ("" unless mode is "self")
  latest: string                   // latest release, without the "v" ("" if unknown)
  updateAvailable: boolean         // latest is newer and mode is "self"
  notes: string                    // release notes of latest (markdown)
  checkError?: string              // why latest is unknown (offline, rate limit)
}
SelfUpdateResult { from, to, gui: string }   // gui: ~/.local/bin/omastore-gui
Job {
  id, kind: "index" | "install" | "update" | "deps" | "self", repo?: string
  state: "running" | "done" | "failed" | "canceled"
  stage?: string                   // install: download, verify, extract, integrate,
                                   //   service (message: the unit being configured), done
                                   // index: discover (total 0), state, index
                                   // deps: authorize (waiting for polkit and pacman)
                                   // self: same stages as install
  done, total: number              // bytes (install) or repos (index)
  message?: string                 // index: current repo; install: current service step
  error?: {code, message}
  result?: InstallInfo | IndexResult | DepsReport | SelfUpdateResult
  started, finished?: string
}
IndexResult { updated, refreshed, unchanged, removed, skipped, notApps, failed }
CheckReport {
  repo: string                     // canonical name (follows renames)
  compatible: boolean              // no check failed
  name, summary, category, iconUrl, tag: string   // what the catalog would show
  screenshots: string[]
  checks: {status: "ok" | "warning" | "fail", item, detail, fix: string}[]
  suggestedManifest: string        // starter omastore.toml; "" when assets are declared
}
DepsReport {
  repo, source: string             // source: PKGBUILD/.SRCINFO path in the repository ("" if none)
  pacman: boolean                  // false: no pacman here, every status is "unknown"
  deps: {
    name, spec: string             // spec keeps the version constraint ("qt6-base>=6.5")
    reason: string                 // optdepends description
    optional: boolean
    status: "installed" | "available" | "unavailable" | "unknown"
    package: string                // "repo/name" pacman would install, when available
  }[]
  missing: number                  // available + unavailable deps, plus libraries
  toInstall: string[]              // what deps.install installs
  libraries: {                     // needed by the installed executable, missing here
    name: string                   // soname, e.g. "libwebkit2gtk-4.1.so.0"
    status: "available" | "unavailable" | "unknown"   // unknown: no pacman file database
    package?: string               // "repo/name" that ships it, when available
  }[]
  wrongArch: string                // machine the executable was built for, if not this one
}
```

## Jobs and notifications

Long operations return a `Job` immediately and continue through notifications:

| Notification | When |
|---|---|
| `job.started` | job created |
| `job.progress` | stage change or progress (at most ~10/s per job) |
| `job.done` | finished successfully (`result` filled in) |
| `job.failed` | failed or was canceled (`state` says which; `error` filled in) |
| `catalog.changed` | while an index runs (when it wrote repos, at most every ~2 s) and after it finishes, after a successful install/update/removal, or when another process (the CLI, the index timer) changed the database — checked every ~15 s; params `{}` or `{repo}` |

Concurrency: **one index at a time** and **one operation per app** (installing,
updating and uninstalling the same repo are mutually exclusive). Both hold
across processes too: an index started while `omastore index` runs, or an
install while `omastore install` changes the same app, fails with `-32002`.
Different apps install in parallel.

Repository names (`repo` params) follow GitHub's rules (`owner` with letters,
digits and hyphens; `repo` starting with a letter or digit) and are compared
ignoring case, as on GitHub: `PCH/Rawmakase` finds `pch/rawmakase`. Results
always carry the stored spelling.

Notifications never wait for a client: each connection has a queue of about a
thousand messages, and a client that stops reading until it fills up is
disconnected (it can reconnect and reload with `jobs.list`).

`catalog.changed` always arrives **after** the matching `job.done`/`job.failed`,
so when it receives it the frontend can reload the list knowing that the job
has already finished. A canceled job keeps in `result` what was done
up to the cancellation (e.g. repos already indexed).

## Error codes

| Code | Meaning |
|---|---|
| -32700 | invalid JSON |
| -32600 | invalid request (no `jsonrpc: "2.0"`/`method`, message too large) |
| -32601 | unknown method |
| -32602 | invalid parameters |
| -32603 | internal error |
| -32001 | not found (app, job) |
| -32002 | busy: a conflicting operation already exists |
| -32003 | conflict: existing file does not belong to OmaStore |
| -32004 | app has no binary for this architecture |
| -32005 | GitHub rate limit exhausted |
| -32006 | app is not installed |
| -32007 | app is already at the latest version |
| -32008 | checksum mismatch |
| -32009 | canceled |
| -32010 | uninstall incomplete: some files could not be removed; the installation stays recorded with only those files, so uninstalling again retries them |
| -32011 | GitHub authentication required (no token, or the token may not star) |
| -32012 | administrator authentication canceled or refused (polkit) |
| -32013 | unsupported system: no pacman |
| -32014 | missing dependencies are in no pacman repository (e.g. AUR) |
| -32015 | the app is running (uninstall without `force`, or reinstalling the running version) |
| -32016 | no previous version on disk to roll back to |
| -32017 | the file has no checksum and `allowUnverified` was not given |
| -32018 | pacman could not download a package: the package database is older than the mirrors |

## Running

- By hand: `omastored` (or `make run-daemon`). Refuses to start if another daemon
  already serves the socket; removes orphaned sockets.
- systemd (user): `packaging/systemd/omastored.{socket,service}` with
  socket activation. `systemctl --user enable --now omastored.socket`.
- Idleness: `-idle-timeout D` stops the daemon after `D` with no connected
  clients and no running jobs (a job finishing counts as activity).
  Default: 10 min when started by socket activation (systemd restarts it
  on the next connection), off when started by hand.
- `SIGTERM`/`SIGINT`: cancels the jobs, waits for them to finish (rolling back
  interrupted installations), closes the database and removes the socket.
