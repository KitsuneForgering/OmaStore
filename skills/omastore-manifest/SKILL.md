---
name: omastore-manifest
description: Creates, reviews and validates the omastore.toml an app needs at the root of its repository to be listed in OmaStore (the Omarchy app store). Use it whenever the user wants to publish, list or "put on OmaStore" an app, make a project compatible with OmaStore/Omarchy, write or fix omastore.toml, or when the app does not show up in the store — even if they do not mention the manifest by name.
---

# OmaStore manifest (`omastore.toml`)

OmaStore only indexes repositories that have an `omastore.toml` at the root of
the default branch and that declare an **app** (not an Omarchy plugin or
theme). The file may even be empty: its presence is the author's opt-in, and
whatever is not declared the store infers on its own. The manifest exists to
declare what the inference would get wrong — icon, executable, which release
file to install.

The full specification is in `references/spec.md`. Read it before writing
fields you do not know by heart.

## Flow

1. **Confirm it is an app.** A standalone program the user opens or runs in
   the terminal. If it is an Omarchy theme, plugin, dotfiles or extension,
   explain that OmaStore does not distribute that kind of project and stop: a
   `kind = "theme"` only serves to make that explicit.

2. **Gather the repository's facts** (do not make things up; every wrong
   field becomes a broken app in the store):
   - display name (README, title, `Cargo.toml`/`package.json`/`go.mod`);
   - one-sentence summary (GitHub repo description, first README paragraph);
   - freedesktop category (see the table in `references/spec.md`);
   - icon: a PNG (≥ 256×256) or SVG committed to the repo;
   - committed screenshots or `https://` URLs;
   - whether it runs in the terminal (CLI/TUI) → `terminal = true`;
   - releases: `gh release view --json tagName,assets` shows the tag and the
     asset names. Without a release with a Linux binary the app is listed but
     cannot be installed — in that case, suggest the `omastore-release` skill.

3. **Decide what to declare.** Prefer a short manifest. Always declare:
   `kind`, `categories` and `icon`. Declare `[linux.<arch>]` when the release
   has more than one file per architecture, names outside the
   `<app>-<version>-<arch>-linux.tar.gz` pattern, or when the executable is not
   named after the repository (e.g. a `bin/app` script that calls `lib/app/app`).
   Use `{version}` in the asset pattern, never the fixed version — the manifest
   has to keep working for future releases.

4. **Write the file** at the repository root, with short comments only where
   the choice is not obvious.

5. **Validate** and fix until there are no errors:
   ```sh
   # with OmaStore installed (it is the reference):
   omastore lint-manifest .
   # without it, this skill's validator applies the same rules:
   python3 <this-skill-dir>/scripts/validate_manifest.py .
   ```
   To check the asset patterns against the real release:
   ```sh
   python3 <this-skill-dir>/scripts/validate_manifest.py . --tag "$TAG" \
     $(gh release view --json assets --jq '.assets[].name' | sed 's/^/--asset /')
   ```

6. **Show the result before pushing**, if OmaStore is installed:
   ```sh
   omastore index --manifest ./omastore.toml owner/repo && omastore show owner/repo
   ```
   This indexes using the local file (in a temporary database, if you prefer:
   `HOME=$(mktemp -d) omastore ...`). Check name, category, icon,
   screenshots and that `installable: true` shows up.

7. **Summarize for the user**: what was declared and why, what was left to
   automatic inference, and the next step (commit + push `omastore.toml`;
   if the release has no Linux binary, the `omastore-release` skill).

## Common pitfalls

- **File outside the root or on another branch**: the store reads
  `HEAD:omastore.toml` from the default branch.
- **Invalid TOML removes the app from the store** (individual invalid fields
  only produce warnings). Always validate.
- **Misnamed field** (`icone`, `binary`): the strict lint flags it; indexing
  silently ignores it.
- **Executable named like a system command** (`ls`, `top`, `code`) or
  `omastore*`: the installation is refused so it does not shadow the existing
  command. Suggest renaming the binary.
- **Absolute paths in `exec`** or `..`: refused. `exec` is relative to the
  root of the extracted package (e.g. `usr/bin/app` for a `.pkg.tar.zst`,
  `app-1.0/bin/app` for a tarball with a top-level directory).
