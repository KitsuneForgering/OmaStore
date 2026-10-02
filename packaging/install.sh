#!/bin/sh
set -eu

repo=https://github.com/KitsuneSemCalda/OmaStore

# --no-skills (anywhere in the arguments) leaves the coding agents' skill
# directories alone; --no-hooks leaves Omarchy's hooks alone.
skills=yes
hooks=yes
n=$#
while [ "$n" -gt 0 ]; do
  arg=$1
  shift
  n=$((n - 1))
  case $arg in
    --no-skills) skills=no ;;
    --no-hooks) hooks=no ;;
    *) set -- "$@" "$arg" ;;
  esac
done

if [ "$(id -u)" -eq 0 ]; then
  echo 'Run without sudo: the installation goes into your account.' >&2
  exit 1
fi

data_home=${XDG_DATA_HOME:-"$HOME/.local/share"}
cache_home=${XDG_CACHE_HOME:-"$HOME/.cache"}
root="$data_home/omastore/self"
bin="$HOME/.local/bin"
desktop="$data_home/applications/omastore.desktop"
icon="$data_home/icons/hicolor/scalable/apps/omastore.svg"
# Skills for app authors (omastore-manifest, -release, -check), for every coding
# agent Omarchy offers. Same directories as Omarchy's own skills: ~/.agents/skills
# is the shared one (OpenCode, Copilot, Gemini, Cursor, Crush, Oh My Pi, Grok,
# Muse, OpenClaw...), plus the agents with a directory of their own. A directory
# is used only when its agent's home exists. Copies installed here carry
# $skill_marker; any other directory is the user's.
skill_marker=.omastore-managed
agent_homes="${CLAUDE_CONFIG_DIR:-$HOME/.claude}
${CODEX_HOME:-$HOME/.codex}
$HOME/.agents
$HOME/.pi/agent
$HOME/.hermes"
# Omarchy's post-update hook: omarchy-update runs every file in this directory
# with bash, so the ownership mark lives inside the file, not next to it.
# Omarchy uses $HOME/.config literally, not $XDG_CONFIG_HOME.
omarchy_config="$HOME/.config/omarchy"
hook="$omarchy_config/hooks/post-update.d/omastore.hook"
hook_marker='# omastore-managed'

# Removes the hook this script installed (never one the user wrote).
remove_managed_hook() {
  if [ -f "$hook" ] && [ ! -L "$hook" ] && grep -qxF "$hook_marker" "$hook"; then
    rm -f "$hook"
  fi
}

# Prints the agents' skill directories, one per line: <home>/skills for each
# agent home, plus Hermes' profiles. With "present", only the agents installed
# here (whose home exists).
skill_dirs() {
  while IFS= read -r home; do
    if [ "${1:-}" = present ] && [ ! -d "$home" ]; then
      continue
    fi
    printf '%s\n' "$home/skills"
    case $home in
      */.hermes)
        for profile in "$home"/profiles/*/; do
          [ -d "$profile" ] && printf '%s\n' "${profile%/}/skills"
        done
        ;;
    esac
  done <<EOF
$agent_homes
EOF
}

# Removes the skill copies this script made in dir (never the user's own skills).
remove_managed_skills_in() {
  for skill in "$1"/omastore-*; do
    if [ -d "$skill" ] && [ ! -L "$skill" ] && [ -f "$skill/$skill_marker" ]; then
      rm -rf "$skill"
    fi
  done
}

remove_managed_skills() {
  while IFS= read -r dir; do
    [ -n "$dir" ] && remove_managed_skills_in "$dir"
  done <<EOF
$(skill_dirs)
EOF
}

# Refresh the menu and icon caches, when the tools exist.
refresh_caches() {
  if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database -q "${desktop%/*}" 2>/dev/null || :
  fi
  if command -v gtk-update-icon-cache >/dev/null 2>&1 && [ -f "$data_home/icons/hicolor/index.theme" ]; then
    gtk-update-icon-cache -q -t -f "$data_home/icons/hicolor" 2>/dev/null || :
  fi
}

# --uninstall stops OmaStore's processes and removes what this
# script installed: the launchers that point into $root, the menu entry, the
# icon, the agent skills it copied, the Omarchy hook, $root itself and the cache. The catalog database and the apps
# installed through OmaStore are kept.
if [ "${1:-}" = --uninstall ]; then
  [ "$#" -eq 1 ] || { echo "Usage: sh $0 --uninstall" >&2; exit 1; }
  # systemd units are left alone: this script never installs them, and the ones
  # present belong to a package or make install (they run /usr/bin/omastored).
  # Stop every OmaStore process of this account running from $root or from
  # anywhere in $HOME (e.g. a development build, even if its file was replaced):
  # a stale daemon would keep serving the old catalog rules.
  pids=
  for proc in /proc/[0-9]*; do
    exe=$(readlink "$proc/exe" 2>/dev/null || :)
    exe=${exe% (deleted)}
    case "${exe##*/}" in omastored|omastore-gui) ;; *) continue ;; esac
    case "$exe" in
      "$root"/*|"$HOME"/*) kill "${proc#/proc/}" 2>/dev/null && pids="$pids ${proc#/proc/}" ;;
    esac
  done
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    alive=
    for pid in $pids; do readlink "/proc/$pid/exe" >/dev/null 2>&1 && alive="$alive $pid"; done
    pids=$alive
    [ -z "$pids" ] && break
    sleep 0.3
  done
  for pid in $pids; do kill -9 "$pid" 2>/dev/null || :; done
  for name in omastore omastored omastore-gui; do
    case "$(readlink "$bin/$name" 2>/dev/null || :)" in
      "$root"/*/bin/"$name") rm -f "$bin/$name" ;;
    esac
  done
  if [ -f "$root/.managed" ]; then
    rm -f "$desktop" "$icon"
  fi
  remove_managed_skills
  remove_managed_hook
  rm -rf "$root" "$cache_home/omastore" "$cache_home/OmaStore"
  refresh_caches
  echo 'OmaStore removed. Your catalog and the apps installed through it were kept.'
  exit 0
fi
for cmd in sha256sum tar mktemp install; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "Required command: $cmd" >&2; exit 1; }
done
[ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || {
  echo 'This release only provides Linux x86_64.' >&2
  exit 1
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

if [ "$#" -eq 0 ]; then
  command -v curl >/dev/null 2>&1 || { echo 'Required command: curl' >&2; exit 1; }
  latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$repo/releases/latest")
  tag=${latest##*/}
  printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$' || {
    echo 'Could not find a stable release.' >&2
    exit 1
  }
  version=${tag#v}
  archive="omastore-$version-x86_64-linux.tar.gz"
  curl -fsSL --retry 3 -o "$tmp/$archive" "$repo/releases/download/$tag/$archive"
  curl -fsSL --retry 3 -o "$tmp/$archive.sha256" "$repo/releases/download/$tag/$archive.sha256"
elif [ "$#" -eq 1 ]; then
  archive=${1##*/}
  version=$(printf '%s\n' "$archive" | sed -n 's/^omastore-\(.*\)-x86_64-linux\.tar\.gz$/\1/p')
  printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$' || {
    echo 'Invalid tarball name.' >&2
    exit 1
  }
  cp "$1" "$tmp/$archive"
  cp "$1.sha256" "$tmp/$archive.sha256"
else
  echo "Usage: sh $0 [--no-skills] [--no-hooks] [path/to/release-tarball] | --uninstall" >&2
  exit 1
fi
digest=$(sed -n "s/^\([0-9a-fA-F]\{64\}\)  \{0,1\}$archive$/\1/p" "$tmp/$archive.sha256")
[ -n "$digest" ] || { echo 'Invalid release checksum.' >&2; exit 1; }
printf '%s  %s\n' "$digest" "$tmp/$archive" | sha256sum -c - >/dev/null || {
  echo 'Release checksum mismatch.' >&2
  exit 1
}

tar -xzf "$tmp/$archive" -C "$tmp" --no-same-owner --no-same-permissions \
  usr/bin/omastore usr/bin/omastored usr/bin/omastore-gui \
  usr/share/applications/omastore.desktop \
  usr/share/icons/hicolor/scalable/apps/omastore.svg
for name in omastore omastored omastore-gui; do
  [ -f "$tmp/usr/bin/$name" ] && [ ! -L "$tmp/usr/bin/$name" ] || {
    echo "Binary missing from the release: $name" >&2; exit 1
  }
done
# Releases before the skills were packaged do not have them.
has_skills=false
if tar -tzf "$tmp/$archive" | grep -q '^usr/share/omastore/skills/omastore-[^/]*/SKILL\.md$'; then
  tar -xzf "$tmp/$archive" -C "$tmp" --no-same-owner --no-same-permissions usr/share/omastore/skills
  if [ -n "$(find "$tmp/usr/share/omastore/skills" ! -type d ! -type f)" ]; then
    echo 'Unexpected link or special file among the skills.' >&2
    exit 1
  fi
  has_skills=true
fi

for name in omastore omastored omastore-gui; do
  link="$bin/$name"
  if [ -e "$link" ] || [ -L "$link" ]; then
    case "$(readlink "$link" 2>/dev/null || :)" in
      "$root"/*/bin/"$name") ;;
      *) echo "Existing unmanaged file: $link" >&2; exit 1 ;;
    esac
  fi
done
if [ -e "$desktop" ] && [ ! -f "$root/.managed" ]; then
  echo "Existing unmanaged shortcut: $desktop" >&2
  exit 1
fi

mkdir -p "$root/$version/bin" "$bin" "${desktop%/*}" "${icon%/*}"
touch "$root/.managed"
for name in omastore omastored omastore-gui; do
  install -m755 "$tmp/usr/bin/$name" "$root/$version/bin/$name"
  ln -sfn "$root/$version/bin/$name" "$bin/$name"
done
exec_path=$(printf '%s' "$bin/omastore-gui" | sed 's/\\/\\\\/g; s/"/\\"/g; s/\$/\\$/g; s/`/\\`/g')
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    Exec=omastore-gui) printf 'Exec="%s"\n' "$exec_path" ;;
    *) printf '%s\n' "$line" ;;
  esac
done < "$tmp/usr/share/applications/omastore.desktop" > "$tmp/omastore.desktop"
install -m644 "$tmp/omastore.desktop" "$desktop"
install -m644 "$tmp/usr/share/icons/hicolor/scalable/apps/omastore.svg" "$icon"
refresh_caches
echo "OmaStore $version installed. Open it from the menu or run $bin/omastore-gui"

# Hand over from the OmaStore that ran before (an older version of this
# installation, a make install, a development build): its daemon keeps the
# socket, so the new interface would talk to the old daemon, and an old window
# would start its own daemon again. Both get SIGTERM, a clean shutdown
# (interrupted installs roll back). Only processes of this session, the ones
# sharing the socket (same XDG_RUNTIME_DIR, or HOME without one): never the
# OmaStore of another login or of a test fixture.
session_of() {
  tr '\0' '\n' < "/proc/$1/environ" 2>/dev/null | sed -n "s/^$2=//p" | head -n 1
}
if [ -n "${XDG_RUNTIME_DIR:-}" ]; then session_var=XDG_RUNTIME_DIR session=$XDG_RUNTIME_DIR
else session_var=HOME session=$HOME
fi
stopped=
for proc in /proc/[0-9]*; do
  exe=$(readlink "$proc/exe" 2>/dev/null) || continue
  exe=${exe% (deleted)}
  name=${exe##*/}
  case "$name" in omastored|omastore-gui) ;; *) continue ;; esac
  [ "$exe" = "$root/$version/bin/$name" ] && continue
  [ "$(session_of "${proc#/proc/}" "$session_var")" = "$session" ] || continue
  kill "${proc#/proc/}" 2>/dev/null && stopped="$stopped ${proc#/proc/}"
done
if [ -n "$stopped" ]; then
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    alive=
    for pid in $stopped; do readlink "/proc/$pid/exe" >/dev/null 2>&1 && alive="$alive $pid"; done
    [ -z "$alive" ] && break
    sleep 0.3
  done
  echo "Closed the OmaStore that was running (it was an older one); open it again from the menu."
fi
# A systemd socket from a package or make install starts its own omastored on
# the next connection, not this one.
if command -v systemctl >/dev/null 2>&1 && [ -n "${XDG_RUNTIME_DIR:-}" ] && [ -d "$XDG_RUNTIME_DIR/systemd" ] &&
  systemctl --user is-enabled omastored.socket >/dev/null 2>&1; then
  unit_exec=$(systemctl --user show -p ExecStart omastored.service 2>/dev/null | sed -n 's/.*path=\([^ ;]*\).*/\1/p')
  if [ -n "$unit_exec" ] && [ "$unit_exec" != "$bin/omastored" ]; then
    echo "Note: your omastored.socket starts $unit_exec, not this installation. To use this one:" >&2
    echo "  systemctl --user disable --now omastored.socket" >&2
  fi
fi

# On Omarchy (its config directory exists), omarchy-update also reports the
# updates of the apps installed through OmaStore. The hook only reads the
# catalog and sends a notification, and never fails the system update.
if [ "$hooks" = yes ] && [ -d "$omarchy_config" ]; then
  # A link to a system install's hook (make install links it) is OmaStore's too.
  case "$(readlink "$hook" 2>/dev/null || :)" in
    */share/omastore/omarchy/omastore.hook) rm -f "$hook" ;;
  esac
  if { [ -e "$hook" ] || [ -L "$hook" ]; } && { [ -L "$hook" ] || ! grep -qxF "$hook_marker" "$hook"; }; then
    echo "Omarchy hook left as is: $hook was not installed by OmaStore." >&2
  else
    quoted_cli=$(printf '%s' "$bin/omastore" | sed "s/'/'\\\\''/g")
    mkdir -p "${hook%/*}"
    cat > "$tmp/omastore.hook" <<HOOK
#!/bin/bash
$hook_marker
# Installed by OmaStore's install.sh and removed by install.sh --uninstall.
# After omarchy-update, notifies about updates to the apps installed through
# OmaStore. It only reads OmaStore's catalog and never fails the update.
cli='$quoted_cli'
[[ -x \$cli ]] && timeout 20 "\$cli" update --check --notify >/dev/null 2>&1
exit 0
HOOK
    install -m644 "$tmp/omastore.hook" "$hook"
    echo "omarchy-update now reports OmaStore app updates ($hook; --no-hooks skips it)"
  fi
fi

if "$has_skills"; then
  rm -rf "$root/$version/share/skills"
  mkdir -p "$root/$version/share"
  cp -R "$tmp/usr/share/omastore/skills" "$root/$version/share/skills"
  # Into every installed agent's skill directory, unless --no-skills (which is
  # remembered, so OmaStore's self-update leaves the agents alone too).
  installed_in=
  if [ "$skills" = no ]; then
    : > "$root/.no-skills"
  else
    rm -f "$root/.no-skills"
    while IFS= read -r dir; do
      [ -n "$dir" ] || continue
      remove_managed_skills_in "$dir" # a skill dropped from a release does not linger
      mkdir -p "$dir"
      for src in "$root/$version/share/skills"/omastore-*; do
        name=${src##*/}
        dest="$dir/$name"
        if [ -e "$dest" ] || [ -L "$dest" ]; then
          echo "Skill $name left as is: $dest was not installed by OmaStore." >&2
          continue
        fi
        cp -R "$src" "$dest"
        : > "$dest/$skill_marker"
      done
      installed_in="$installed_in
  $dir"
    done <<EOF
$(skill_dirs present)
EOF
  fi
  if [ -n "$installed_in" ]; then
    echo "Skills for app authors installed for your coding agents in:$installed_in"
  elif [ "$skills" = yes ]; then
    echo "Skills for app authors: $root/$version/share/skills (no coding agent found; copy them into its skills directory)"
  fi
fi
