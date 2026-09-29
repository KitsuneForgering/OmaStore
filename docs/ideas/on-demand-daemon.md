# On-demand daemon, periodic index and update notifications

> **Status:** implemented in Phase 13: `-idle-timeout` in `omastored`,
> `omastore update --check --notify` (`internal/notify`, direct D-Bus) and
> `packaging/systemd/omastore-index.{service,timer}`.

## Evidence

- With socket activation (`packaging/systemd/omastored.socket`), systemd already
  starts the daemon on the first connection, but it keeps running forever.
- Today indexing only happens when someone asks for it (`index.start` or
  `omastore index`). A user who does not open the store never hears about
  updates to the installed apps.

## Proposal

1. **Exit when idle:** with no connections and no jobs for N minutes (default
   10), the daemon exits cleanly. With socket activation, the next connection
   starts it again. Only enable this when the socket comes from systemd
   (`ActivationListener() != nil`).
2. **systemd timer:** `omastore-index.timer` (daily, `Persistent=true`,
   `RandomizedDelaySec=1h`) runs `omastore index` and then
   `omastore update --check`.
3. **`update --check`:** lists the apps with a new version without installing and, if
   there are any, sends a desktop notification (`notify-send` via
   `org.freedesktop.Notifications` on D-Bus, without calling binaries from `PATH`).
   Updating on its own stays optional (a setting).

## Cost

Small: an activity counter in `Server`, the timer units, the `--check` flag
in the CLI and the D-Bus notification (`godbus/dbus`).

## Risks

- Updating automatically without the user seeing it is risky (new binary not
  reviewed); that is why the default is to only notify.
- Exiting when idle with long jobs: the counter must take running jobs into
  account, not just connections.
