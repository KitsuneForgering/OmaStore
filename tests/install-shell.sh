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
# The session whose OmaStore an install replaces; nothing outside it is touched.
export XDG_RUNTIME_DIR="$fixture/run"
mkdir -p "$XDG_RUNTIME_DIR"
mkdir -p "$HOME" "$fixture/mock-bin" "$fixture/usr/bin" \
  "$fixture/usr/share/applications" "$fixture/usr/share/icons/hicolor/scalable/apps"
for name in omastore omastored omastore-gui; do
  printf '#!/bin/sh\nexit 0\n' > "$fixture/usr/bin/$name"
  chmod +x "$fixture/usr/bin/$name"
done
cp "$project/packaging/desktop/omastore.desktop" "$fixture/usr/share/applications/"
cp "$project/packaging/desktop/omastore-mark.svg" \
  "$fixture/usr/share/icons/hicolor/scalable/apps/omastore.svg"
for skill in omastore-check omastore-release; do
  mkdir -p "$fixture/usr/share/omastore/skills/$skill/scripts"
  printf -- '---\nname: %s\n---\n' "$skill" > "$fixture/usr/share/omastore/skills/$skill/SKILL.md"
  printf '#!/bin/sh\n' > "$fixture/usr/share/omastore/skills/$skill/scripts/run.sh"
  chmod +x "$fixture/usr/share/omastore/skills/$skill/scripts/run.sh"
done
tar -czf "$fixture/omastore-1.2.3-x86_64-linux.tar.gz" -C "$fixture" usr
sha256sum "$fixture/omastore-1.2.3-x86_64-linux.tar.gz" | \
  sed 's| .*/|  |' > "$fixture/checksum"

cat > "$fixture/mock-bin/curl" <<'MOCK'
#!/bin/sh
case " $* " in
  *url_effective*) printf '%s\n' 'https://github.com/KitsuneForgering/OmaStore/releases/tag/v1.2.3'; exit ;;
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

# Without any coding agent, the skills stay only next to the app.
sh "$project/packaging/install.sh" > "$fixture/no-agent-log"
root="$XDG_DATA_HOME/omastore/self"
[ -f "$root/1.2.3/share/skills/omastore-check/SKILL.md" ]
[ -x "$root/1.2.3/share/skills/omastore-check/scripts/run.sh" ]
for home in .claude .codex .agents .pi .hermes; do [ ! -e "$HOME/$home" ]; done
grep -F 'no coding agent found' "$fixture/no-agent-log" >/dev/null

# Without Omarchy (~/.config/omarchy), no post-update hook.
hook="$HOME/.config/omarchy/hooks/post-update.d/omastore.hook"
[ ! -e "$HOME/.config/omarchy" ]
# With it, the hook runs the CLI by its absolute path; --no-hooks skips it.
mkdir -p "$HOME/.config/omarchy"
sh "$project/packaging/install.sh" --no-hooks > /dev/null
[ ! -e "$hook" ]
sh "$project/packaging/install.sh" > /dev/null
grep -qxF '# omastore-managed' "$hook"
grep -F "cli='$HOME/.local/bin/omastore'" "$hook" >/dev/null
printf '#!/bin/sh\nprintf "%%s\\n" "$*" > "$FIXTURE/hook-args"\n' > "$root/1.2.3/bin/omastore"
bash "$hook" > /dev/null
[ "$(cat "$fixture/hook-args")" = 'update --auto --notify' ]
# A failing CLI never fails omarchy-update.
printf '#!/bin/sh\nexit 3\n' > "$root/1.2.3/bin/omastore"
bash "$hook" > /dev/null

# With coding agents, they are copied into each installed agent's skill
# directory (the shared ~/.agents/skills, Claude Code, Codex, Pi, Hermes and
# its profiles). A copy of the same name OmaStore did not install gives way to
# the new one and is kept in skill-backups.
claude_skills="$HOME/.claude/skills"
agent_skills="$claude_skills
$HOME/.codex/skills
$HOME/.agents/skills
$HOME/.pi/agent/skills
$HOME/.hermes/skills
$HOME/.hermes/profiles/work/skills"
mkdir -p "$claude_skills/omastore-release" "$HOME/.codex" "$HOME/.agents" "$HOME/.pi/agent" \
  "$HOME/.hermes/profiles/work"
printf 'mine\n' > "$claude_skills/omastore-release/SKILL.md"
sh "$project/packaging/install.sh" > "$fixture/skills-log" 2>&1
printf '%s\n' "$agent_skills" | while IFS= read -r dir; do
  [ -f "$dir/omastore-check/SKILL.md" ] || { echo "skill missing in $dir" >&2; exit 1; }
  [ -f "$dir/omastore-check/.omastore-managed" ]
  grep -F "$dir" "$fixture/skills-log" >/dev/null
done
grep -qx 'name: omastore-release' "$claude_skills/omastore-release/SKILL.md"
[ -f "$claude_skills/omastore-release/.omastore-managed" ]
[ -f "$HOME/.codex/skills/omastore-release/.omastore-managed" ]
grep -F 'Skill omastore-release: replaced' "$fixture/skills-log" >/dev/null
skill_backup=$(ls -d "$XDG_DATA_HOME"/omastore/skill-backups/omastore-release.*/omastore-release)
[ "$(cat "$skill_backup/SKILL.md")" = mine ]

# --no-skills leaves ~/.claude alone; a normal run restores a deleted copy.
rm -rf "$claude_skills/omastore-check"
sh "$project/packaging/install.sh" --no-skills > /dev/null
[ ! -e "$claude_skills/omastore-check" ]
[ -f "$root/.no-skills" ] # so OmaStore's self-update leaves the agents alone too
sh "$project/packaging/install.sh" > "$fixture/reinstall-log"
[ ! -e "$root/.no-skills" ]
bash -o pipefail -c 'cat "$1" | bash' _ "$project/packaging/install.sh" \
  > "$fixture/piped-log"
sh "$project/packaging/install.sh" "$fixture/omastore-1.2.3-x86_64-linux.tar.gz" \
  > "$fixture/local-log"
[ -f "$claude_skills/omastore-check/SKILL.md" ]
[ -x "$HOME/.local/bin/omastore-gui" ]
grep -Fx "Exec=\"$HOME/.local/bin/omastore-gui\" %u" \
  "$XDG_DATA_HOME/applications/omastore.desktop" >/dev/null
if command -v desktop-file-validate >/dev/null 2>&1; then
  desktop-file-validate "$XDG_DATA_HOME/applications/omastore.desktop"
fi

# Installing over a running older OmaStore of this session closes it: its
# daemon would keep the socket. Another session's OmaStore is left alone.
mkdir -p "$root/1.2.2/bin" "$fixture/other-session"
cp "$(command -v sleep)" "$root/1.2.2/bin/omastored"
"$root/1.2.2/bin/omastored" 60 &
old_daemon=$!
cp "$(command -v sleep)" "$fixture/other-session/omastored"
XDG_RUNTIME_DIR="$fixture/other-run" "$fixture/other-session/omastored" 60 &
other_session=$!
bg_pids="$bg_pids $old_daemon $other_session"
sleep 0.2
sh "$project/packaging/install.sh" > "$fixture/handover-log"
for _ in 1 2 3 4 5 6 7 8 9 10; do readlink "/proc/$old_daemon/exe" >/dev/null 2>&1 || break; sleep 0.2; done
if readlink "/proc/$old_daemon/exe" >/dev/null 2>&1; then
  echo 'the older omastored kept running after the install' >&2
  exit 1
fi
if ! readlink "/proc/$other_session/exe" >/dev/null 2>&1; then
  echo "another session's omastored was stopped" >&2
  exit 1
fi
kill "$other_session"
grep -F 'Closed the OmaStore that was running' "$fixture/handover-log" >/dev/null
# A link to a system install's hook (left by make install) is OmaStore's: replaced.
mkdir -p "$fixture/sys/share/omastore/omarchy"
: > "$fixture/sys/share/omastore/omarchy/omastore.hook"
rm -f "$hook"
ln -s "$fixture/sys/share/omastore/omarchy/omastore.hook" "$hook"
sh "$project/packaging/install.sh" > /dev/null
[ ! -L "$hook" ] && grep -qxF '# omastore-managed' "$hook"

# --uninstall removes what install.sh created (and stops its daemon), but keeps
# the catalog, the apps installed through OmaStore and files it does not own.
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
  "$XDG_DATA_HOME/icons/hicolor/scalable/apps/omastore.svg" "$root" "$XDG_CACHE_HOME/omastore" \
  "$claude_skills/omastore-check" "$HOME/.agents/skills/omastore-check" \
  "$HOME/.hermes/profiles/work/skills/omastore-check" "$hook"; do
  if [ -e "$p" ] || [ -L "$p" ]; then
    echo "Left behind by --uninstall: $p" >&2
    exit 1
  fi
done
[ -f "$HOME/.local/bin/omastore" ]
[ -f "$XDG_DATA_HOME/omastore/omastore.db" ]
[ -d "$XDG_DATA_HOME/omastore/apps/acme__app" ]
[ "$(cat "$skill_backup/SKILL.md")" = mine ] # backups survive --uninstall
sh "$project/packaging/install.sh" --uninstall > /dev/null

# A hook of the same name that the user wrote is never replaced or removed.
rm "$HOME/.local/bin/omastore" # the user's file from the test above
mkdir -p "${hook%/*}"
printf 'mine\n' > "$hook"
sh "$project/packaging/install.sh" > /dev/null 2> "$fixture/user-hook-log"
[ "$(cat "$hook")" = mine ]
grep -F 'Omarchy hook left as is' "$fixture/user-hook-log" >/dev/null
sh "$project/packaging/install.sh" --uninstall > /dev/null
[ "$(cat "$hook")" = mine ]
