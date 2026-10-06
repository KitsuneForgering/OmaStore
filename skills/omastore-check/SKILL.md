---
name: omastore-check
description: Audits whether a GitHub repository can be installed by OmaStore (the Omarchy app store) by running OmaStore itself in a temporary HOME: validates omastore.toml, indexes, checks release, architecture, checksum, icon and, optionally, really installs and uninstalls. Use it when the user asks whether the app "works on OmaStore", why it does not show up or does not install from the store, before publishing a release, or to review a project's compatibility with Omarchy.
---

# OmaStore compatibility audit

The best way to know whether the store installs an app is to let the store
try. This skill's script does exactly that, in a throwaway HOME: nothing is
written to the user's `~/.local` and no app binary is executed.

## Flow

1. **Find the `owner/repo`** (from `git remote get-url origin` if you are in the
   repository) and whether an `omastore.toml` is already published or only local.

2. **Run the audit.** It needs the `omastore` CLI (the OmaStore package or
   `$OMASTORE=/path/omastore`) and a GitHub token (`GITHUB_TOKEN`, or an
   authenticated `gh`). A report that says it *could not read* `omastore.toml`
   is about access (token, rate limit, network), not about the repository:
   ```sh
   # what the store sees today (published manifest):
   python3 <this-skill-dir>/scripts/omastore_check.py owner/repo
   # before pushing, with the local manifest:
   python3 <this-skill-dir>/scripts/omastore_check.py owner/repo --manifest ./omastore.toml
   # full audit, with a test install (downloads the release):
   python3 <this-skill-dir>/scripts/omastore_check.py owner/repo --manifest ./omastore.toml --install
   ```
   `--json` gives structured output, useful for CI. Exit code 1 = there are
   failures.

3. **Interpret the report** for the user. Each line is ✅, ⚠️ or ❌:
   - ❌ keeps the app out of the store or from installing: fix it before publishing.
   - ⚠️ does not block, but makes the experience worse (no icon, no screenshots, no
     ARM build, binary with absolute paths in `/usr`, no build provenance —
     users who require provenance cannot install it, and it is not updated
     automatically for them).
   Explain the likely cause of each ❌ in terms of their project, not just the
   script's message.

4. **Route the fix** to the right skill:
   - missing or invalid manifest, wrong icon or executable →
     `omastore-manifest`;
   - no release, no Linux asset, no ARM, no checksum, no build provenance →
     `omastore-release` (the provenance comes from
     `actions/attest-build-provenance` in the repository's own workflow);
   - an invalid `[services]` declaration (the app disappears from the
     catalog) → `omastore-manifest`;
   - "Absolute paths": the app looks for data in `/usr/share/<app>`, but the
     store installs into `~/.local/share/omastore/apps/…`. The fix is in the
     app's code: resolve files relative to the executable.
   - command name conflict: the executable is named like a system command
     (`ls`, `top`…) or `omastore*`; rename the binary.

5. **Run it again** after the fixes until the result is "compatible with
   OmaStore". Once the local manifest passes, remind the user to commit and
   push `omastore.toml`: the store only reads the file from the default branch.

## Quick report

`omastore check owner/repo [--manifest ./omastore.toml] [--json]` gives the same
manifest/release/asset/checksum/build-provenance checks without a temporary
HOME or a test install, plus a suggested `omastore.toml` built from the
release. Use it for a
fast first pass; use the script above when installation must be tested.

## Without the `omastore` CLI

If OmaStore is not installed and the user does not want to install it, do a
partial check: validate the manifest with the `omastore-manifest` skill's
validator (including `--tag/--asset` with the names from
`gh release view --json assets`) and manually check the format described in
`omastore-release`. Make it clear that installation was not tested.
