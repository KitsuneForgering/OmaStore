# Releasing a Go app

Go builds for both architectures on a single runner when the app does not use
CGO. With CGO (e.g. SQLite via `mattn/go-sqlite3`, GTK), use one runner per
architecture (second job below).

## Without CGO

```yaml
name: Release

on:
  push:
    tags: ['v*']

jobs:
  build:
    runs-on: ubuntu-latest
    env:
      VERSION: ${{ github.ref_name }}
      APP: myapp                # executable name
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
      - name: Build and package
        run: |
          ver="${VERSION#v}"
          mkdir -p dist
          for pair in amd64:x86_64 arm64:aarch64; do
            goarch="${pair%%:*}"; arch="${pair##*:}"
            dir="$APP-$ver-$arch-linux"
            mkdir -p "dist/$dir/bin"
            CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" \
              go build -trimpath -ldflags "-s -w" -o "dist/$dir/bin/$APP" .
            cp LICENSE README.md "dist/$dir/" 2>/dev/null || true
            tar -C dist -czf "dist/$dir.tar.gz" "$dir"
          done
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

## With CGO

Replace the `build` job with a matrix of native runners (GitHub ARM
runners are free for public repositories):

```yaml
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
      APP: myapp
      ARCH: ${{ matrix.arch }}
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
      - name: Build and package
        run: |
          ver="${VERSION#v}"
          dir="$APP-$ver-$ARCH-linux"
          mkdir -p "dist/$dir/bin"
          CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o "dist/$dir/bin/$APP" .
          tar -C dist -czf "dist/$dir.tar.gz" "$dir"
      - uses: actions/upload-artifact@v4
        with:
          name: dist-${{ matrix.arch }}
          path: dist/*.tar.gz
```

In `publish`, download with `pattern: dist-*` and `merge-multiple: true`, and generate
`checksums.txt` there (`cd dist && sha256sum ./*.tar.gz > checksums.txt`).

Note: CGO binaries link against the runner's glibc. Building on a recent
Ubuntu and running on Arch works (Arch has a newer glibc); the
opposite does not.
