#!/bin/sh
set -eu

repo=https://github.com/KitsuneSemCalda/OmaStore

# --no-skills (anywhere in the arguments) leaves ~/.claude alone.
skills=yes
n=$#
while [ "$n" -gt 0 ]; do
  arg=$1
  shift
  n=$((n - 1))
  case $arg in
    --no-skills) skills=no ;;
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
# Claude Code skills for app authors (omastore-manifest, -release, -check).
# Copies installed here carry $skill_marker; any other directory is the user's.
claude_home=${CLAUDE_CONFIG_DIR:-"$HOME/.claude"}
claude_skills="$claude_home/skills"
skill_marker=.omastore-managed

# Removes the skill copies this script made (never the user's own skills).
remove_managed_skills() {
  for dir in "$claude_skills"/omastore-*; do
    if [ -d "$dir" ] && [ ! -L "$dir" ] && [ -f "$dir/$skill_marker" ]; then
      rm -rf "$dir"
    fi
  done
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
# icon, the Claude Code skills it copied, $root itself and the cache. The catalog database and the apps
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
  echo "Usage: sh $0 [--no-skills] [path/to/release-tarball] | --uninstall" >&2
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

if "$has_skills"; then
  rm -rf "$root/$version/share/skills"
  mkdir -p "$root/$version/share"
  cp -R "$tmp/usr/share/omastore/skills" "$root/$version/share/skills"
  # Only with Claude Code present (its directory exists) and not --no-skills.
  if [ "$skills" = yes ] && [ -d "$claude_home" ]; then
    remove_managed_skills # a skill dropped from a release does not linger
    mkdir -p "$claude_skills"
    done_skills=
    for src in "$root/$version/share/skills"/omastore-*; do
      name=${src##*/}
      dest="$claude_skills/$name"
      if [ -e "$dest" ] || [ -L "$dest" ]; then
        echo "Skill $name left as is: $dest was not installed by OmaStore." >&2
        continue
      fi
      cp -R "$src" "$dest"
      : > "$dest/$skill_marker"
      done_skills="$done_skills $name"
    done
    [ -z "$done_skills" ] || echo "Claude Code skills for app authors installed in $claude_skills:$done_skills"
  elif [ "$skills" = yes ]; then
    echo "Claude Code skills for app authors: $root/$version/share/skills (copy them into ~/.claude/skills)"
  fi
fi
