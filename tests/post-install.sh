#!/bin/sh
# make install over a running older OmaStore (packaging/post-install.sh):
# - a per-user installation made by install.sh is replaced: removed (its daemon
#   stopped, the catalog kept), its Omarchy hook and agent skills set up again
#   from the system installation;
# - the daemon left running from a replaced binary is stopped, one from
#   anywhere else is not;
# - an unmanaged per-user copy, or OMASTORE_KEEP_USER_INSTALL=1, only warns.
set -eu

project=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
fixture=$(mktemp -d)
bg_pids=
trap 'kill $bg_pids 2>/dev/null || :; rm -rf "$fixture"' EXIT HUP INT TERM
export HOME="$fixture/home"
export XDG_DATA_HOME="$HOME/.local/share" XDG_CACHE_HOME="$HOME/.cache" XDG_CONFIG_HOME="$HOME/.config"
export XDG_RUNTIME_DIR="$fixture/run" # no systemd here: never touch the real user manager
unset OMASTORE_KEEP_USER_INSTALL
prefix="$fixture/prefix"
mkdir -p "$XDG_RUNTIME_DIR" "$prefix/bin" "$fixture/elsewhere" \
  "$prefix/share/omastore/omarchy" "$prefix/share/omastore/skills/omastore-check"
printf '#!/bin/bash\nexit 0\n' > "$prefix/share/omastore/omarchy/omastore.hook"
printf -- '---\nname: omastore-check\n---\n' > "$prefix/share/omastore/skills/omastore-check/SKILL.md"
sleep=$(command -v sleep)
fail() { echo "post-install: $*" >&2; echo "$out" >&2; exit 1; }
running() { readlink "/proc/$1/exe" >/dev/null 2>&1; } # a zombie has no exe

# A managed per-user installation (what install.sh leaves), with its daemon
# running, its hook and an agent skill.
self="$XDG_DATA_HOME/omastore/self"
mkdir -p "$self/0.2.0/bin" "$HOME/.local/bin" "$XDG_DATA_HOME/applications" \
  "$HOME/.config/omarchy/hooks/post-update.d" "$HOME/.claude/skills/omastore-check" "$XDG_DATA_HOME/omastore/apps/acme__app"
: > "$self/.managed"
: > "$XDG_DATA_HOME/omastore/omastore.db"
for name in omastore omastored omastore-gui; do
  cp "$sleep" "$self/0.2.0/bin/$name"
  ln -s "$self/0.2.0/bin/$name" "$HOME/.local/bin/$name"
done
printf '[Desktop Entry]\nExec="%s"\n' "$HOME/.local/bin/omastore-gui" > "$XDG_DATA_HOME/applications/omastore.desktop"
hook="$HOME/.config/omarchy/hooks/post-update.d/omastore.hook"
printf '#!/bin/bash\n# omastore-managed\n' > "$hook"
printf 'old\n' > "$HOME/.claude/skills/omastore-check/SKILL.md"
: > "$HOME/.claude/skills/omastore-check/.omastore-managed"
"$self/0.2.0/bin/omastored" 60 & user_daemon=$!
# The old system daemon, about to be replaced, and a daemon from elsewhere.
cp "$sleep" "$prefix/bin/omastored"
cp "$sleep" "$fixture/elsewhere/omastored"
"$prefix/bin/omastored" 60 & old=$!
"$fixture/elsewhere/omastored" 60 & other=$!
bg_pids="$user_daemon $old $other"
sleep 0.2
install -m755 "$sleep" "$prefix/bin/omastored" # make install replaces the binary

out=$(sh "$project/packaging/post-install.sh" "$prefix/bin" "$prefix/share" 2>&1)

for _ in 1 2 3 4 5 6 7 8 9 10; do running "$old" || running "$user_daemon" || break; sleep 0.2; done
running "$old" && fail "the replaced daemon is still running"
running "$user_daemon" && fail "the per-user installation's daemon is still running"
running "$other" || fail "stopped a daemon it did not replace"
case "$out" in *"replaced your per-user installation"*) ;; *) fail "no message about the replaced installation" ;; esac
case "$out" in *"stopped the previous omastored"*) ;; *) fail "no message about the stopped daemon" ;; esac
for p in "$HOME/.local/bin/omastore-gui" "$HOME/.local/bin/omastored" "$self" "$XDG_DATA_HOME/applications/omastore.desktop"; do
  { [ -e "$p" ] || [ -L "$p" ]; } && fail "left behind: $p"
done
[ -f "$XDG_DATA_HOME/omastore/omastore.db" ] || fail "the catalog was removed"
[ -d "$XDG_DATA_HOME/omastore/apps/acme__app" ] || fail "the installed apps were removed"
[ "$(readlink "$hook")" = "$prefix/share/omastore/omarchy/omastore.hook" ] || fail "the hook was not linked to the system one"
grep -q 'name: omastore-check' "$HOME/.claude/skills/omastore-check/SKILL.md" || fail "the skill was not set up again"
[ -f "$HOME/.claude/skills/omastore-check/.omastore-managed" ] || fail "the restored skill is not marked as OmaStore's"

# An unmanaged per-user copy (or OMASTORE_KEEP_USER_INSTALL=1): only a warning.
printf '#!/bin/sh\n' > "$HOME/.local/bin/omastore-gui"
out=$(sh "$project/packaging/post-install.sh" "$prefix/bin" "$prefix/share" 2>&1)
case "$out" in *"per-user installation is in"*) ;; *) fail "no warning about the unmanaged per-user copy" ;; esac
[ -f "$HOME/.local/bin/omastore-gui" ] || fail "removed an unmanaged file"
mkdir -p "$self" && : > "$self/.managed"
out=$(OMASTORE_KEEP_USER_INSTALL=1 sh "$project/packaging/post-install.sh" "$prefix/bin" "$prefix/share" 2>&1)
case "$out" in *"per-user installation is in"*) ;; *) fail "OMASTORE_KEEP_USER_INSTALL did not keep it" ;; esac
[ -f "$self/.managed" ] || fail "OMASTORE_KEEP_USER_INSTALL removed the installation"

# Nothing to do: quiet, and never an error.
rm -rf "$HOME/.local/bin/omastore-gui" "$self"
out=$(sh "$project/packaging/post-install.sh" "$prefix/bin" "$prefix/share" 2>&1)
[ -z "$out" ] || fail "unexpected output with nothing to hand over"
echo "post-install: ok"
