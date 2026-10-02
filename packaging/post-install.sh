#!/bin/sh
# Run by `make install` on a live system (no DESTDIR): makes the binaries just
# installed in <bindir> the OmaStore the user runs, for the user who ran make
# (also under sudo). Never fails the install.
#
#   sh packaging/post-install.sh <bindir> <datadir>
#
# - reloads the user's systemd units (the service file may have changed);
# - takes over from a per-user installation made by install.sh: it comes first
#   in PATH and its menu entry hides the system one, so the menu kept opening
#   the old OmaStore. It is removed with install.sh --uninstall (which also stops
#   its processes; the catalog and the installed apps are kept), and what it
#   had set up comes back from this installation: the Omarchy hook (a link to
#   <datadir>/omastore/omarchy/omastore.hook) and the coding agents' skills.
#   OMASTORE_KEEP_USER_INSTALL=1 keeps it (and only warns);
# - stops an omastored still running from a binary this install replaced: it
#   would keep the socket. SIGTERM is a clean shutdown; the socket unit or the
#   interface starts the new one on the next connection.
set -u
[ "$#" -ge 2 ] || { echo "usage: sh $0 <bindir> <datadir>" >&2; exit 0; }
bindir=$1 datadir=$2
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

if [ "$(id -u)" -eq 0 ]; then
  # Root: act for the user behind sudo; without one (a chroot, a package
  # build), there is no session to hand over.
  user=${SUDO_USER:-}
  [ -n "$user" ] && [ "$user" != root ] || exit 0
  uid=$(id -u "$user") || exit 0
  exec sudo -u "$user" -H env XDG_RUNTIME_DIR="/run/user/$uid" \
    ${OMASTORE_KEEP_USER_INSTALL:+OMASTORE_KEEP_USER_INSTALL=$OMASTORE_KEEP_USER_INSTALL} \
    sh "$0" "$bindir" "$datadir"
fi

say() { printf 'omastore: %s\n' "$*" >&2; }

if command -v systemctl >/dev/null 2>&1 && [ -n "${XDG_RUNTIME_DIR:-}" ] && [ -d "$XDG_RUNTIME_DIR/systemd" ]; then
  systemctl --user daemon-reload 2>/dev/null || :
fi

# The per-user installation (same paths as install.sh).
data_home=${XDG_DATA_HOME:-"$HOME/.local/share"}
self_root="$data_home/omastore/self"
home_bin="$HOME/.local/bin"
hook="$HOME/.config/omarchy/hooks/post-update.d/omastore.hook"
if [ "$home_bin" != "$bindir" ] && [ -e "$home_bin/omastore-gui" ]; then
  if [ -f "$self_root/.managed" ] && [ "${OMASTORE_KEEP_USER_INSTALL:-}" != 1 ] && [ -f "$here/install.sh" ]; then
    # What install.sh set up, to set it up again from this installation.
    had_hook=false
    [ -f "$hook" ] && [ ! -L "$hook" ] && grep -qxF '# omastore-managed' "$hook" && had_hook=true
    skill_dirs=
    for marker in "$HOME"/.claude/skills/omastore-*/.omastore-managed "$HOME"/.codex/skills/omastore-*/.omastore-managed \
      "$HOME"/.agents/skills/omastore-*/.omastore-managed "$HOME"/.pi/agent/skills/omastore-*/.omastore-managed \
      "$HOME"/.hermes/skills/omastore-*/.omastore-managed "$HOME"/.hermes/profiles/*/skills/omastore-*/.omastore-managed; do
      [ -f "$marker" ] || continue
      dir=${marker%/*}
      dir=${dir%/*}
      case " $skill_dirs " in *" $dir "*) ;; *) skill_dirs="$skill_dirs $dir" ;; esac
    done

    if sh "$here/install.sh" --uninstall >/dev/null 2>&1; then
      say "replaced your per-user installation (install.sh) with this one; your catalog and apps are kept."
      if "$had_hook" && [ -f "$datadir/omastore/omarchy/omastore.hook" ] && [ ! -e "$hook" ]; then
        ln -s "$datadir/omastore/omarchy/omastore.hook" "$hook" && say "omarchy-update keeps reporting app updates ($hook)."
      fi
      restored=
      for dir in $skill_dirs; do
        for src in "$datadir"/omastore/skills/omastore-*; do
          [ -d "$src" ] || continue
          dest="$dir/${src##*/}"
          [ -e "$dest" ] || [ -L "$dest" ] && continue
          mkdir -p "$dir" && cp -R "$src" "$dest" && : > "$dest/.omastore-managed" && restored=yes
        done
      done
      [ -n "$restored" ] && say "the skills for app authors were copied again for your coding agents."
    else
      say "could not remove the per-user installation in $home_bin; it still comes first."
    fi
  else
    say "a per-user installation is in $home_bin: it comes first in PATH and its menu entry"
    say "hides this one, so you would keep running it. To use this installation, remove it with:"
    say "  sh packaging/install.sh --uninstall   (your catalog and installed apps are kept)"
  fi
fi

# The daemons of this account running a replaced copy of $bindir/omastored
# (the kernel marks the old file " (deleted)").
pids=
for proc in /proc/[0-9]*; do
  exe=$(readlink "$proc/exe" 2>/dev/null) || continue
  [ "$exe" = "$bindir/omastored (deleted)" ] || continue
  if kill "${proc#/proc/}" 2>/dev/null; then
    pids="$pids ${proc#/proc/}"
  fi
done
if [ -n "$pids" ]; then
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    alive=
    for pid in $pids; do readlink "/proc/$pid/exe" >/dev/null 2>&1 && alive="$alive $pid"; done
    pids=$alive
    [ -z "$pids" ] && break
    sleep 0.3
  done
  if [ -n "$pids" ]; then
    say "the previous omastored (pid$pids) is still finishing; it exits once its jobs end."
  else
    say "stopped the previous omastored; the new one starts with OmaStore. Reopen OmaStore if it is open."
  fi
fi
exit 0
