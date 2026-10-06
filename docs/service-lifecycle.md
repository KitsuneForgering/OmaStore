# Installation lifecycle and user services

## Existing flow (reviewed before the service change)

`omastore.toml` is parsed in `backend/internal/manifest`, sanitized, and saved
as JSON on the catalog app. The indexer accepts unknown ordinary fields for
forward compatibility; the linter reports them. `install.Install` reads that
JSON, chooses a release asset, downloads and verifies it, then extracts it
under `$XDG_DATA_HOME/omastore/apps/<owner>__<repo>/.staging-*`. It resolves
the executable inside that tree, moves the tree into a version directory,
and writes a launcher in `~/.local/bin`, icon and desktop entry. The `tx`
object backs up replaced files and restores them if integration or the
SQLite save fails. The `installs` row stores the version, paths and owned
files; `install_history` stores version transitions. An update calls Install,
keeps the previous version for rollback, and prunes older registered files.
Rollback switches launchers and desktop entries to the retained version.
Uninstall removes only registered files and retains failed removals in SQLite
for retry. The GUI receives install job stages via RPC; uninstall is a
synchronous RPC. The process runs without sudo and checks destinations are
inside the user's home.

## Service design

The manifest declares a service name, an executable **inside the release**,
and desired enable/start state. OmaStore writes a minimal user unit in the
user's XDG config directory. It does not execute a packaged unit or a
manifest command line. State in the existing `installs` row records each
unit path and whether OmaStore enabled or started it. An existing unit with
the same name is a conflict. Only units whose path still matches OmaStore's
record may be changed or removed. This keeps the integration alongside the
existing install transaction and gives future integration types a place in
the installation record without a second lifecycle database.

The generated unit points to a versioned executable. A version update reloads
systemd and restarts the service only if it was active; a service manually
stopped since the previous install stays stopped. Version rollback restores
the prior declaration and executable path from a second integration snapshot
in the same installation row. Failure before the installation record is saved
restores files and attempts to restore previous service state. Service setup
failure (missing executable, unavailable user manager, reload, enable, start
or restart) aborts installation or update. A failed uninstall service step
keeps the installation record for retry. Service removal succeeds before app
file removal; if app file removal then fails, only the remaining paths stay
recorded. If restoration itself fails (for example, the user manager disappears
mid-operation), the returned error includes the rollback failure; the prior
database record remains and the user can retry after the manager recovers.
`systemctl --user` is called with argument arrays and no shell.

The SQLite migrations add JSON columns to `installs` with `[]` defaults, so
existing installations keep working. Existing manifests need no changes.
Future capabilities can use the same manifest validation, installation record,
transaction and progress path, with a backend specific to their integration
type. There is no generic command hook interface.

## Omakade compatibility case

Omakade's package build installs `omakade-sessiond` at `usr/bin` and its
packaged unit at `usr/lib/systemd/user`. The packaged unit names
`/usr/bin/omakade-sessiond`, which is wrong for OmaStore's versioned home
install. A declaration with `exec = "usr/bin/omakade-sessiond"` lets OmaStore
generate a valid user unit. Omakade's profile loader checks a path relative
to its executable (`../share/omakade/sessiond-profiles.json`) before its
compiled `/usr/share` fallback, matching the extracted package layout.
The upstream published manifest currently has no service declaration; the
app publisher must add one before real OmaStore installs receive the service.
The lifecycle tests use a fake user manager; no host service was changed.

References: [systemd user unit directories](https://www.freedesktop.org/software/systemd/man/latest/systemd.unit.html),
[systemctl user operations](https://www.freedesktop.org/software/systemd/man/latest/systemctl.html),
[Omakade package install rules](https://github.com/btsouth/omakade/blob/main/CMakeLists.txt),
[Omakade profile lookup](https://github.com/btsouth/omakade/blob/main/src/tracking/ProcessMatcher.cpp).
