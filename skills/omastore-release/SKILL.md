---
name: omastore-release
description: Sets up GitHub releases that OmaStore can install: a tag-triggered GitHub Actions workflow that builds the app for Linux (x86_64 and aarch64), packages it as <app>-<version>-<arch>-linux.tar.gz with checksums and publishes it to the release. Use it when the user wants the app to be installable through OmaStore/Omarchy, to publish Linux binaries, to automate releases, or when the store shows the app as "no Linux binary" — even if they only say "I want to publish a version".
---

# Releases installable by OmaStore

OmaStore installs the binary from the repository's **latest stable release**
(not a pre-release, not a draft). For that the release needs one Linux file
per architecture, with a predictable name and content. The `omastore.toml`
manifest (skill `omastore-manifest`) points to those files; this skill makes
the files exist.

## Target format

```
Release v1.2.0
├── myapp-1.2.0-x86_64-linux.tar.gz
│     └── myapp-1.2.0-x86_64-linux/
│           ├── bin/myapp           ← executable (ELF or script with shebang)
│           ├── share/…             ← data, icons (optional)
│           └── LICENSE, README.md
├── myapp-1.2.0-aarch64-linux.tar.gz
└── checksums.txt                   ← sha256sum of all tarballs
```

Why this way:
- `<app>-<version>-<arch>-linux.tar.gz` is recognized even without a manifest
  and matches `asset = "myapp-{version}-x86_64-linux.tar.gz"`.
- A top-level directory with `bin/` makes the executable easy to find. If it
  is named after the repository, the store finds it on its own; if not, declare
  `exec = "myapp-{version}-x86_64-linux/bin/mycommand"` in the manifest — the
  `{version}` is replaced by each release's version.
- The store checks the sha256 GitHub computes per asset; `checksums.txt`
  is a useful extra for people who download by hand.
- The templates attest the tarballs' build provenance
  (`actions/attest-build-provenance`, like OmaStore's own release). Keep that
  step and its `id-token: write` and `attestations: write` permissions:
  OmaStore verifies the attestation with Sigstore, shows "Built by this
  repository's GitHub Actions" on the app page, and users who require
  provenance cannot install a release without it. It must run in a workflow
  **of the app's own repository** (a reusable workflow from another repository
  does not count) and cover the exact tarballs uploaded.
- A background service (daemon) ships inside the same tarball, e.g.
  `bin/myapp-daemon`; the manifest's `[services.<id>]` points at it and
  OmaStore writes the systemd user unit. Do not rely on a packaged `.service`
  with `/usr/bin` paths.
- Everything is installed into `~/.local/share/omastore/apps/…`, never into `/usr`.
  Programs that look for data at absolute paths (`/usr/share/myapp`)
  break; resolve paths relative to the executable.

## Flow

1. **Identify the ecosystem** and read only the matching reference:
   - Go → `references/go.md`
   - Rust → `references/rust.md`
   - C/C++ with CMake, including Qt → `references/cmake-qt.md`
   - anything else (Zig, Nim, packaged script, ready-made AppImage) →
     `references/generic.md`
2. **Find out the executable's name** and where the build puts it. If the name
   collides with a system command (`ls`, `code`, `top`), warn: OmaStore
   refuses to install it so it does not shadow the command.
3. **Create `.github/workflows/release.yml`** from the template, adjusting the
   app name and build commands. Keep:
   - the `v*` tag trigger;
   - the tag coming in through an environment variable (`VERSION: ${{ github.ref_name }}`),
     never interpolated directly into `run:` — a malicious tag name would become
     a command;
   - `permissions: contents: write` only on the publishing job.
4. **Validate the workflow**, if possible:
   `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/release.yml`.
5. **Test the packaging locally** before the first tag, running the same
   commands as the build job and checking the tarball:
   `tar tzf dist/myapp-*-x86_64-linux.tar.gz | head`.
6. **Update `omastore.toml`** to point to the assets (or confirm that the
   heuristics are enough): `[linux.x86_64] asset = "myapp-{version}-x86_64-linux.tar.gz"`.
7. **Explain how to publish**: `git tag v1.2.0 && git push origin v1.2.0`.
   Afterwards, the `omastore-check` skill confirms that the store installs it
   and that the build provenance is verified. Users with automatic updates
   (OmaStore's default) get the new release on their own after
   `omarchy update` or when the store opens, as long as it has a checksum.

## What not to do

- Do not publish only `.deb`/`.rpm`: OmaStore ignores those formats.
- Do not mark the release as a pre-release if you want it to show up in the store.
- Do not include install scripts that need to run: the store never executes
  anything from the package during installation.
