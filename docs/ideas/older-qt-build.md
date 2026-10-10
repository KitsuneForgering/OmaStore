# Build the frontend against the oldest supported Qt

The release build is pinned to an Arch snapshot whose Qt is deliberately one
minor behind the bleeding edge (2026-03-15 → Qt 6.10), bounded by `make
check-qt-floor`. This takes the room for systems at Qt 6.10 and newer, but not
older ones, and it still lets the snapshot drift sideways.

## Evidence

- The GUI is dynamically linked against Qt, so it carries symbol-version
  requirements: a binary built against Qt 6.12 needs `Qt_6.12` and will not
  load on a system with Qt 6.11 (`omastore-gui: version 'Qt_6.12' not found`,
  2026-10-10; system on qt6-base 6.11.2). The Go binaries are static and never
  hit this.
- The declared minimum Qt is 6.5 (`frontend/CMakeLists.txt`), but the CMake
  min only guards the *developers'* compile; `archlinux:latest` used to build
  whatever Qt the rolling container had, silently raising what shipped.
- A single Arch snapshot cannot carry both the oldest Qt and the required Go:
  `go.mod` needs Go 1.26 (first on Arch 2026-02-18) while Qt 6.5 was last in
  extra in 2023-09 (`qt6-base-6.5.3`).
- Qt 6.8 is an LTS and ships in Debian trixie; Qt 6.5 is the CMake floor and
  exists in Arch Linux Archive (2023-10, glibc far older than today's).

## Proposal

Decouple the two build halves of `make dist`:

1. **Go binaries** with the current toolchain (official `golang` image or the
   live `go` package) — static, no libc/Qt concern.
2. **Frontend GUI** in an environment carrying the oldest supported Qt — an
   Arch Linux Archive snapshot from ~2023-10 for Qt 6.5, or Debian trixie for
   Qt 6.8 — and its own `go`-free build.
3. Assemble both in `dist/` with the existing reproducible `tar` step, and run
   `check-qt-floor` against the shipped GUI.

## Cost

One extra build environment in the release and CI workflows (the frontend no
longer shares a container with Go); `make dist` needs a step that builds the
two parts from different toolchains; verification that the current code still
compiles and the frontend tests pass against Qt 6.5/6.8.

## Risks

- An old toolchain may not compile present-day code (CMake/C++20/QML API use
  newer than the floor), which is exactly what the tests must prove.
- The Qt 6.5 Arch snapshot means an old glibc too: fine for forward
  compatibility, but upstream security fixes for that toolchain stop.
- The floor check stays mandatory: features pulled in over time would quietly
  raise the requirement again.