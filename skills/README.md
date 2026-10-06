# Skills for app authors

Agent skills (`SKILL.md`) that help make an app compatible with OmaStore, for
any coding agent that reads skills: Claude Code, Codex, OpenCode, GitHub
Copilot, Gemini CLI, Cursor, Crush, Pi/Oh My Pi, Grok, Hermes and the other
agents Omarchy offers. The store only indexes repositories with an
`omastore.toml` at the root declaring an app (plugins and themes are left out).

| Skill | What for |
|---|---|
| [`omastore-manifest`](omastore-manifest/SKILL.md) | Write and validate `omastore.toml` (includes a dependency-free Python validator) |
| [`omastore-release`](omastore-release/SKILL.md) | Release workflow with Linux `x86_64`/`aarch64` tarballs and checksums (Go, Rust, CMake/Qt, generic) |
| [`omastore-check`](omastore-check/SKILL.md) | Audit the repository with OmaStore itself in a temporary HOME, including a test install |

## Installation

OmaStore's `install.sh` does this for you. It copies these skills into the
same directories Omarchy links its own skills into, for each agent installed
(whose directory exists):

| Directory | Agents |
|---|---|
| `~/.agents/skills` | the shared one: OpenCode, GitHub Copilot, Gemini CLI, Cursor, Crush, Oh My Pi, Grok, Muse, OpenClaw… |
| `~/.claude/skills` (or `$CLAUDE_CONFIG_DIR/skills`) | Claude Code |
| `~/.codex/skills` (or `$CODEX_HOME/skills`) | Codex |
| `~/.pi/agent/skills` | Pi |
| `~/.hermes/skills` and `~/.hermes/profiles/*/skills` | Hermes |

OmaStore's self-update and `install.sh` runs keep them current, and
`install.sh --uninstall` removes them. OmaStore's copies carry a
`.omastore-managed` file. Another copy of the same skill (from an older
OmaStore, or copied by hand) gives way to the new version and is moved to
`~/.local/share/omastore/skill-backups/`, which uninstalling keeps. `--no-skills` opts out, and the choice is remembered by the self-update.

The pacman packages and `make install` put them in
`/usr/share/omastore/skills/`; copy them into your agent's directory, for
example the shared one:

```sh
mkdir -p ~/.agents/skills
cp -r /usr/share/omastore/skills/omastore-* ~/.agents/skills/
```

By hand, just for your app's project (Claude Code reads `.claude/skills`,
most other agents `.agents/skills`):

```sh
mkdir -p .agents/skills
cp -r /path/to/OmaStore/skills/omastore-* .agents/skills/
```

Then ask your agent, for example: "get this app ready for OmaStore" or
"why doesn't my app show up in OmaStore?".

## Maintenance

The Python validator (`omastore-manifest/scripts/validate_manifest.py`)
duplicates the rules of `backend/internal/manifest`. `make test-skills` runs
both over `tests/manifests/` and fails if they disagree; run it whenever the
manifest format changes.
