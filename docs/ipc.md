# OmaStore IPC protocol

The frontend talks to `omastored` over **JSON-RPC 2.0** on a Unix socket at
`$XDG_RUNTIME_DIR/omastore.sock` (mode `0600`). Implementation:
`backend/internal/rpc/`. Protocol version: **1** (`daemon.hello`).

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
| `daemon.hello` | — | `{name, protocol}` |
| `catalog.list` | `{category?, query?, installed?, all?, limit?, offset?}` | `AppItem[]`: by score; with `query`, by relevance |
| `catalog.get` | `{repo}` | `AppDetail` |
| `catalog.similar` | `{repo, limit?}` (default 8, max. 50) | most similar `AppItem[]`, installable only |
| `catalog.categories` | — | `[{name, count}]` |
| `installs.list` | — | `InstallInfo[]` |
| `index.start` | `{force?, repos?}` | `Job` (kind `index`) |
| `install.start` | `{repo}` | `Job` (kind `install`) |
| `update.start` | `{repo}` | `Job` (kind `update`) |
| `install.uninstall` | `{repo}` | `{}` (synchronous) |
| `jobs.list` | — | `Job[]` (running ones and the last 50 finished) |
| `jobs.cancel` | `{job}` | `{}` |
| `image.get` | `{url}` | `{path}`: local path of the cached image |
| `author.check` | `{repo, manifest?}` | `CheckReport` (synchronous; writes nothing to the catalog) |
| `star.get` | `{repo}` | `{starred}`: whether the GitHub user starred the repository |
| `star.set` | `{repo, starred}` | `{starred, stars}`: stars/unstars it on GitHub; `stars` is the catalog's new count |
| `deps.check` | `{repo}` | `DepsReport` (synchronous; runs no privileged command) |
| `deps.install` | `{repo}` | `Job` (kind `deps`) |

`repo` is always `"owner/repo"`. `all: true` includes apps without an installable binary.

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
(pacman has a single lock).

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
  releaseNotes: string             // markdown body of the latest release, as the
                                   // author wrote it (up to ~16 KiB); "" if none
  description, license, htmlUrl: string
  topics: string[]
  pushedAt?, indexedAt?: string    // RFC 3339
  assets: {name, size, arch, format, verified}[]
  install: InstallInfo | null
}
InstallInfo { repo, version, installedAt, execPath, desktopPath }
Job {
  id, kind: "index" | "install" | "update" | "deps", repo?: string
  state: "running" | "done" | "failed" | "canceled"
  stage?: string                   // install: download, verify, extract, integrate, done
                                   // index: discover (total 0), state, index
                                   // deps: authorize (waiting for polkit and pacman)
  done, total: number              // bytes (install) or repos (index)
  message?: string                 // index: current repo
  error?: {code, message}
  result?: InstallInfo | IndexResult | DepsReport
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
  missing: number                  // available + unavailable
  toInstall: string[]              // what deps.install installs
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
