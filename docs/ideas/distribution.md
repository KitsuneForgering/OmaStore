# Distribution: releases, AUR and OmaStore itself in the catalog

> **Status:** items 1 and 2 implemented in Phase 14 (`make dist`,
> `.github/workflows/release.yml`, `packaging/arch-bin/`). Item 3 is still
> open: the `omastore-<version>-x86_64-linux.tar.gz` tarball follows the
> store's own rules, but the store installing itself is refused on
> purpose (the `omastore*` commands are reserved); the store should be
> managed by pacman.

## Evidence

- The `PKGBUILD` (`packaging/arch/`) produces `omastore-git`, which builds from source
  and requires `go`, `cmake` and `ninja` on the user's machine. Simulating the
  build and check steps, the package took a few minutes to build.
- The project follows its own publishing rules ([`../authors.md`](../authors.md)),
  but does not publish releases yet: OmaStore could not install itself
  (or update itself).

## Proposal

1. **Release workflow** (`.github/workflows/release.yml`, triggered by a `v*`
   tag): builds backend and frontend for `x86_64` and `aarch64` and publishes
   `omastore-<version>-<arch>-linux.tar.gz` with the `make install` tree.
   Add `actions/attest-build-provenance` (see [trust.md](trust.md)).
2. **Two AUR packages:** `omastore-git` (current) and `omastore-bin`, which downloads
   the release tarball and checks its sha256. Installs in seconds.
3. **`omarchy` topic on the repository itself:** the store shows up in the catalog and
   can update itself like any app. Careful: the `omastore` launcher is a
   reserved name (the installer refuses it), so the store updating itself
   needs special handling or should be left to pacman.

## Cost

Small for the workflow and `-bin`. Item 3 requires deciding who manages the
store's own installation (pacman or OmaStore) so there are not two copies.

## Risks

- Cross-compiling the Qt frontend for `aarch64` in CI requires an ARM runner or
  QEMU; start with `x86_64` only.
