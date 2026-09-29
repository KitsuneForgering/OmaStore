#!/bin/sh
set -eu

repo=https://github.com/KitsuneSemCalda/OmaStore

if [ "$(id -u)" -eq 0 ]; then
  echo 'Run without sudo: the installation goes into your account.' >&2
  exit 1
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
  echo "Usage: sh $0 [path/to/release-tarball]" >&2
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

data_home=${XDG_DATA_HOME:-"$HOME/.local/share"}
root="$data_home/omastore/self"
bin="$HOME/.local/bin"
desktop="$data_home/applications/omastore.desktop"
icon="$data_home/icons/hicolor/scalable/apps/omastore.svg"
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
echo "OmaStore $version installed. Open it from the menu or run $bin/omastore-gui"
