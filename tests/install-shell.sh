#!/bin/sh
set -eu

project=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
export HOME="$fixture/home with space"
export XDG_DATA_HOME="$HOME/.local/share"
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
sh "$project/packaging/install.sh" "$fixture/omastore-1.2.3-x86_64-linux.tar.gz" \
  > "$fixture/local-log"
[ -x "$HOME/.local/bin/omastore-gui" ]
grep -F "Exec=\"$HOME/.local/bin/omastore-gui\"" \
  "$XDG_DATA_HOME/applications/omastore.desktop" >/dev/null
if command -v desktop-file-validate >/dev/null 2>&1; then
  desktop-file-validate "$XDG_DATA_HOME/applications/omastore.desktop"
fi
