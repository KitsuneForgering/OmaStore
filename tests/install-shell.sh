#!/bin/sh
set -eu

project=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
fixture=$(mktemp -d)
bg_pids=
trap 'kill $bg_pids 2>/dev/null || :; rm -rf "$fixture"' EXIT HUP INT TERM
export HOME="$fixture/home with space"
export XDG_DATA_HOME="$HOME/.local/share"
# Never touch the real user's directories (--uninstall deletes the cache).
export XDG_CACHE_HOME="$HOME/.cache" XDG_CONFIG_HOME="$HOME/.config" XDG_STATE_HOME="$HOME/.local/state"
export FIXTURE="$fixture"
mkdir -p "$HOME" "$fixture/mock-bin" "$fixture/usr/bin" \
  "$fixture/usr/share/applications" "$fixture/usr/share/icons/hicolor/scalable/apps"
for name in omastore omastored omastore-gui; do
  printf '#!/bin/sh\nexit 0\n' > "$fixture/usr/bin/$name"
  chmod +x "$fixture/usr/bin/$name"
done
cp "$project/packaging/desktop/omastore.desktop" "$fixture/usr/share/applications/"
cp "$project/packaging/desktop/omastore-mark.svg" \
  "$fixture/usr/share/icons/hicolor/scalable/apps/omastore.svg"
tar -czf "$fixture/omastore-1.2.3-x86_64-linux.tar.gz" -C "$fixture" usr
sha256sum "$fixture/omastore-1.2.3-x86_64-linux.tar.gz" | \
  sed 's| .*/|  |' > "$fixture/checksum"

cat > "$fixture/mock-bin/curl" <<'MOCK'
#!/bin/sh
case " $* " in
  *url_effective*) printf '%s\n' 'https://github.com/KitsuneSemCalda/OmaStore/releases/tag/v1.2.3'; exit ;;
esac
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then output=$2; shift 2; else url=$1; shift; fi
done
case "$url" in
  *.sha256) cp "$FIXTURE/checksum" "$output" ;;
  *) cp "$FIXTURE/omastore-1.2.3-x86_64-linux.tar.gz" "$output" ;;
esac
MOCK
chmod +x "$fixture/mock-bin/curl"
PATH="$fixture/mock-bin:$PATH"
export PATH

printf '%064d  omastore-1.2.3-x86_64-linux.tar.gz\n' 0 > "$fixture/checksum"
if sh "$project/packaging/install.sh" > "$fixture/log" 2>&1; then
  echo 'Wrong checksum was accepted' >&2
  exit 1
fi
[ ! -e "$HOME/.local/bin/omastore-gui" ]

sha256sum "$fixture/omastore-1.2.3-x86_64-linux.tar.gz" | \
  sed 's| .*/|  |' > "$fixture/checksum"
cp "$fixture/checksum" "$fixture/omastore-1.2.3-x86_64-linux.tar.gz.sha256"
sh "$project/packaging/install.sh"
sh "$project/packaging/install.sh" > "$fixture/reinstall-log"
bash -o pipefail -c 'cat "$1" | bash' _ "$project/packaging/install.sh" \
  > "$fixture/piped-log"
sh "$project/packaging/install.sh" "$fixture/omastore-1.2.3-x86_64-linux.tar.gz" \
  > "$fixture/local-log"
[ -x "$HOME/.local/bin/omastore-gui" ]
grep -F "Exec=\"$HOME/.local/bin/omastore-gui\"" \
  "$XDG_DATA_HOME/applications/omastore.desktop" >/dev/null
if command -v desktop-file-validate >/dev/null 2>&1; then
  desktop-file-validate "$XDG_DATA_HOME/applications/omastore.desktop"
fi

# --uninstall removes what install.sh created (and stops its daemon), but keeps
# the catalog, the apps installed through OmaStore and files it does not own.
root="$XDG_DATA_HOME/omastore/self"
mkdir -p "$XDG_DATA_HOME/omastore/apps/acme__app" "$XDG_CACHE_HOME/omastore/repos"
: > "$XDG_DATA_HOME/omastore/omastore.db"
cp "$(command -v sleep)" "$root/1.2.3/bin/omastored"
"$root/1.2.3/bin/omastored" 60 &
daemon=$!
bg_pids="$bg_pids $daemon"
# A development daemon in $HOME whose binary was already replaced on disk.
mkdir -p "$HOME/src/bin" "$fixture/outside"
cp "$(command -v sleep)" "$HOME/src/bin/omastored"
"$HOME/src/bin/omastored" 60 &
dev_daemon=$!
bg_pids="$bg_pids $dev_daemon"
rm "$HOME/src/bin/omastored"
# Someone else's omastored outside $HOME is left alone.
cp "$(command -v sleep)" "$fixture/outside/omastored"
"$fixture/outside/omastored" 60 &
outside=$!
bg_pids="$bg_pids $outside"
cat > "$fixture/mock-bin/systemctl" <<'MOCK'
#!/bin/sh
printf '%s\n' "$*" >> "$FIXTURE/systemctl-calls"
MOCK
chmod +x "$fixture/mock-bin/systemctl"
rm "$HOME/.local/bin/omastore"
printf '#!/bin/sh\n' > "$HOME/.local/bin/omastore"
sh "$project/packaging/install.sh" --uninstall > "$fixture/uninstall-log"
# A killed child stays a zombie until waited for; zombies have no exe.
running() { readlink "/proc/$1/exe" >/dev/null 2>&1; }
for pid in "$daemon" "$dev_daemon"; do
  if running "$pid"; then
    echo 'omastored still running after --uninstall' >&2
    exit 1
  fi
done
if ! running "$outside"; then
  echo 'an omastored outside $HOME was stopped' >&2
  exit 1
fi
kill "$outside"
# The units belong to a package or make install, never to install.sh.
if [ -e "$fixture/systemctl-calls" ]; then
  echo '--uninstall touched systemd units it did not install' >&2
  exit 1
fi
for p in "$HOME/.local/bin/omastored" "$HOME/.local/bin/omastore-gui" \
  "$XDG_DATA_HOME/applications/omastore.desktop" \
  "$XDG_DATA_HOME/icons/hicolor/scalable/apps/omastore.svg" "$root" "$XDG_CACHE_HOME/omastore"; do
  if [ -e "$p" ] || [ -L "$p" ]; then
    echo "Left behind by --uninstall: $p" >&2
    exit 1
  fi
done
[ -f "$HOME/.local/bin/omastore" ]
[ -f "$XDG_DATA_HOME/omastore/omastore.db" ]
[ -d "$XDG_DATA_HOME/omastore/apps/acme__app" ]
sh "$project/packaging/install.sh" --uninstall > /dev/null
