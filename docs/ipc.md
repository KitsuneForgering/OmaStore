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
  description, license, htmlUrl: string
  topics: string[]
  pushedAt?, indexedAt?: string    // RFC 3339
  assets: {name, size, arch, format, verified}[]
  install: InstallInfo | null
}
InstallInfo { repo, version, installedAt, execPath, desktopPath }
Job {
  id, kind: "index" | "install" | "update", repo?: string
  state: "running" | "done" | "failed" | "canceled"
  stage?: string                   // install: download, verify, extract, integrate, done
  done, total: number              // bytes (install) or repos (index)
  message?: string                 // index: current repo
  error?: {code, message}
  result?: InstallInfo | IndexResult
  started, finished?: string
}
IndexResult { updated, refreshed, unchanged, removed, skipped, notApps, failed }
```

## Jobs and notifications

Long operations return a `Job` immediately and continue through notifications:

| Notification | When |
|---|---|
| `job.started` | job created |
| `job.progress` | stage change or progress (at most ~10/s per job) |
| `job.done` | finished successfully (`result` filled in) |
| `job.failed` | failed or was canceled (`state` says which; `error` filled in) |
| `catalog.changed` | after an index finishes or after a successful install/update/removal; params `{}` or `{repo}` |

Concurrency: **one index at a time** and **one operation per app** (installing,
updating and uninstalling the same repo are mutually exclusive); conflicts
return `-32002`. Different apps install in parallel.

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
