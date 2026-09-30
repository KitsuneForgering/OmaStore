# Skills for app authors

[Claude Code](https://claude.com/claude-code) skills that help make an app
compatible with OmaStore. The store only indexes repositories with an
`omastore.toml` at the root declaring an app (plugins and themes are left out).

| Skill | What for |
|---|---|
| [`omastore-manifest`](omastore-manifest/SKILL.md) | Write and validate `omastore.toml` (includes a dependency-free Python validator) |
| [`omastore-release`](omastore-release/SKILL.md) | Release workflow with Linux `x86_64`/`aarch64` tarballs and checksums (Go, Rust, CMake/Qt, generic) |
| [`omastore-check`](omastore-check/SKILL.md) | Audit the repository with OmaStore itself in a temporary HOME, including a test install |

## Installation

OmaStore's `install.sh` does this for you: when `~/.claude` exists, it copies
these skills into `~/.claude/skills` (and updates or removes them with the
store). It never replaces a skill of the same name that it did not install; its
copies carry a `.omastore-managed` file. Use `--no-skills` to opt out.

The pacman packages and `make install` put them in
`/usr/share/omastore/skills/`; copy them from there:

```sh
mkdir -p ~/.claude/skills
cp -r /usr/share/omastore/skills/omastore-* ~/.claude/skills/
```

By hand, in your app's project (just for it):

```sh
mkdir -p .claude/skills
cp -r /path/to/OmaStore/skills/omastore-* .claude/skills/
```

Or for all your projects:

```sh
mkdir -p ~/.claude/skills
cp -r /path/to/OmaStore/skills/omastore-* ~/.claude/skills/
```

Then just ask Claude, for example: "get this app ready for OmaStore" or
"why doesn't my app show up in OmaStore?".

## Maintenance

The Python validator (`omastore-manifest/scripts/validate_manifest.py`)
duplicates the rules of `backend/internal/manifest`. `make test-skills` runs
both over `tests/manifests/` and fails if they disagree; run it whenever the
manifest format changes.
