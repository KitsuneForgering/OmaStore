#!/bin/sh
# Installs (or reinstalls) OmaStore into your account straight from GitHub:
#
#   curl -fsSLO https://raw.githubusercontent.com/KitsuneSemCalda/OmaStore/dev/scripts/install.sh
#   sh install.sh
#
# With a published release, it runs that release's install.sh (prebuilt
# binaries, SHA-256 checked). Without one, or with --source, it clones the
# repository, builds the release tarball (make dist) and installs it with
# packaging/install.sh. Either way everything goes into your account
# (~/.local), without sudo.
#
# Options:
#   --source     build from source even if a release exists
#   --ref REF    branch or tag to build (implies --source; default: $OMASTORE_REF or dev)
#   --force      install even beside a system-wide OmaStore (pacman, make install);
#                the per-user copy takes precedence in PATH and in the menu
#   --no-skills  do not copy the skills for app authors into the coding agents' skill directories
#   --no-hooks   do not add the Omarchy post-update hook that reports app updates
set -eu

repo_url=https://github.com/KitsuneSemCalda/OmaStore
ref=${OMASTORE_REF:-dev}
from_source=false
force=false
install_opts=

usage() {
  echo "Usage: sh $0 [--source] [--ref BRANCH_OR_TAG] [--force] [--no-skills] [--no-hooks]" >&2
  exit "${1:-1}"
}

while [ "$#" -gt 0 ]; do
  case $1 in
    --source) from_source=true ;;
    --ref)
      [ "$#" -ge 2 ] || usage
      ref=$2
      from_source=true
      shift
      ;;
    --ref=*) ref=${1#--ref=}; from_source=true ;;
    --force) force=true ;;
    --no-skills) install_opts="$install_opts --no-skills" ;;
    --no-hooks) install_opts="$install_opts --no-hooks" ;;
    -h | --help) usage 0 ;;
    *) usage ;;
  esac
  shift
done

# The ref goes to git as an argument: no options, no "..", only name characters.
case $ref in -* | *..* | '') echo "Invalid ref: $ref" >&2; exit 1 ;; esac
printf '%s\n' "$ref" | grep -Eq '^[A-Za-z0-9._/-]+$' || { echo "Invalid ref: $ref" >&2; exit 1; }

if [ "$(id -u)" -eq 0 ]; then
  echo 'Run without sudo: the installation goes into your account.' >&2
  exit 1
fi
[ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || {
  echo 'OmaStore currently supports only Linux x86_64.' >&2
  exit 1
}

# A per-user installation from an earlier run is simply replaced (install.sh
# switches the version and closes the old OmaStore that is running). A system
# one (pacman, make install) is not this script's: installing beside it puts a
# second OmaStore first in PATH and in the menu, so that needs --force; the one
# running is then closed and the new one takes its place.
if ! "$force"; then
  for p in /usr/bin/omastore-gui /usr/local/bin/omastore-gui; do
    if [ -e "$p" ]; then
      echo "OmaStore is already installed system-wide: $p" >&2
      echo 'Update it the way it was installed (pacman, or make install from a newer checkout).' >&2
      echo 'To install a per-user copy that takes precedence over it instead, run again with --force.' >&2
      exit 1
    fi
  done
fi

need() {
  missing=
  for cmd in "$@"; do
    command -v "$cmd" >/dev/null 2>&1 || missing="$missing $cmd"
  done
  [ -z "$missing" ] || { echo "Required commands:$missing" >&2; exit 1; }
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

if ! "$from_source"; then
  need curl
  # Without releases, /releases/latest redirects to /releases.
  latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$repo_url/releases/latest" 2>/dev/null || :)
  tag=${latest##*/}
  if printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'; then
    echo "Installing release $tag."
    curl -fsSL --retry 3 -o "$tmp/install.sh" "$repo_url/releases/download/$tag/install.sh"
    sh "$tmp/install.sh" $install_opts
    exit 0
  fi
  echo "No release published yet; building from source ($ref)."
fi

need git go cmake ninja make c++ tar gzip sha256sum install
qt=
for name in Qt6Quick Qt6QuickControls2 Qt6Svg; do
  found=false
  for dir in /usr/lib/cmake /usr/lib64/cmake /usr/lib/x86_64-linux-gnu/cmake; do
    [ -d "$dir/$name" ] && found=true && break
  done
  "$found" || qt="$qt $name"
done
if [ -n "$qt" ]; then
  echo "Qt 6 modules not found:$qt" >&2
  echo 'On Arch/Omarchy: sudo pacman -S --needed git go cmake ninja gcc qt6-base qt6-declarative qt6-svg' >&2
  exit 1
fi

echo "Cloning $repo_url ($ref)..."
git clone --quiet --depth 1 --branch "$ref" "$repo_url.git" "$tmp/src"
sha=$(git -C "$tmp/src" rev-parse --short=12 HEAD)
version="0.0.0-git.$sha"

echo "Building OmaStore $version (this takes a few minutes)..."
log="${XDG_CACHE_HOME:-$HOME/.cache}/omastore-install.log"
mkdir -p "${log%/*}"
if ! make -C "$tmp/src" dist VERSION="v$version" >"$log" 2>&1; then
  tail -n 30 "$log" >&2
  echo "Build failed; full log: $log" >&2
  exit 1
fi

sh "$tmp/src/packaging/install.sh" $install_opts "$tmp/src/dist/omastore-$version-x86_64-linux.tar.gz"
rm -f "$log"
