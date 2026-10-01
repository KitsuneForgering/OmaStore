# Releasing a C/C++ app (CMake, including Qt)

Qt/GTK apps link against the system libraries. To run on Omarchy
(Arch), build **in an Arch container**: that way the binary uses the same Qt
versions the user has installed (the app then depends on the packages
`qt6-base`, `qt6-declarative` etc., which the README must list).

```yaml
name: Release

on:
  push:
    tags: ['v*']

jobs:
  build:
    runs-on: ubuntu-latest
    container: archlinux:latest
    env:
      VERSION: ${{ github.ref_name }}
      APP: myapp                 # name of the executable produced by CMake
    steps:
      - name: Dependencies
        run: pacman -Syu --noconfirm --needed base-devel git cmake ninja qt6-base qt6-declarative
      - uses: actions/checkout@v7
      - name: Build and package
        run: |
          ver="${VERSION#v}"
          dir="$APP-$ver-x86_64-linux"
          cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/
          cmake --build build
          DESTDIR="$PWD/dist/$dir" cmake --install build --strip
          tar -C dist -czf "dist/$dir.tar.gz" "$dir"
          (cd dist && sha256sum ./*.tar.gz > checksums.txt)
      - uses: actions/upload-artifact@v4
        with:
          name: dist
          path: |
            dist/*.tar.gz
            dist/checksums.txt

  publish:
    needs: build
    runs-on: ubuntu-latest
    permissions:
      contents: write
      id-token: write       # provenance attestation
      attestations: write
    steps:
      - uses: actions/download-artifact@v4
        with:
          name: dist
          path: dist
      - uses: actions/attest-build-provenance@v4
        with:
          subject-path: dist/*.tar.gz
      - uses: softprops/action-gh-release@v3
        with:
          generate_release_notes: true
          files: |
            dist/*.tar.gz
            dist/checksums.txt
```

With `CMAKE_INSTALL_PREFIX=/` and `GNUInstallDirs`, the tarball contains
`<dir>/bin/myapp`, `<dir>/share/…`. For aarch64, repeat the job on
`ubuntu-24.04-arm` (the `archlinux` container has no official ARM image;
use `menci/archlinuxarm` or build without a container and document the versions).

Caveats:
- QML resources embedded with `qt_add_qml_module` do not depend on paths.
  Files read from disk must be resolved from
  `QCoreApplication::applicationDirPath()` (e.g. `../share/myapp`), never from
  `/usr/share`.
- An absolute `RPATH` pointing to the build directory breaks outside CI;
  `cmake --install` fixes it by default.
