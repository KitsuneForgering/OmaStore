# Releasing a Rust app

Use one native runner per architecture: cross-compiling Rust with C dependencies
(GTK, OpenSSL, libraw) is fragile. GitHub ARM runners are free for
public repositories.

```yaml
name: Release

on:
  push:
    tags: ['v*']

jobs:
  build:
    strategy:
      matrix:
        include:
          - runner: ubuntu-latest
            arch: x86_64
          - runner: ubuntu-24.04-arm
            arch: aarch64
    runs-on: ${{ matrix.runner }}
    env:
      VERSION: ${{ github.ref_name }}
      APP: myapp                # binary name ([[bin]] or crate name)
      ARCH: ${{ matrix.arch }}
    steps:
      - uses: actions/checkout@v4
      - uses: dtolnay/rust-toolchain@stable
      # The app's system dependencies, if any:
      # - run: sudo apt-get update && sudo apt-get install -y libgtk-4-dev
      - name: Build and package
        run: |
          cargo build --release --locked
          ver="${VERSION#v}"
          dir="$APP-$ver-$ARCH-linux"
          mkdir -p "dist/$dir/bin"
          install -m755 "target/release/$APP" "dist/$dir/bin/$APP"
          cp LICENSE* README.md "dist/$dir/" 2>/dev/null || true
          tar -C dist -czf "dist/$dir.tar.gz" "$dir"
      - uses: actions/upload-artifact@v4
        with:
          name: dist-${{ matrix.arch }}
          path: dist/*.tar.gz

  publish:
    needs: build
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/download-artifact@v4
        with:
          pattern: dist-*
          path: dist
          merge-multiple: true
      - run: cd dist && sha256sum ./*.tar.gz > checksums.txt
      - uses: softprops/action-gh-release@v2
        with:
          generate_release_notes: true
          files: |
            dist/*.tar.gz
            dist/checksums.txt
```

Tips:
- `strip = true` and `lto = true` in `[profile.release]` shrink the binary a lot.
- Resources (icons, themes) the app loads at runtime must go into
  `share/` inside the tarball and be looked up relative to
  `std::env::current_exe()`, not in `/usr/share`.
