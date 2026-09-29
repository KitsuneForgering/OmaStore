# Release de app Go

Go compila para as duas arquiteturas num único runner quando o app não usa
CGO. Com CGO (ex.: SQLite via `mattn/go-sqlite3`, GTK), use um runner por
arquitetura (segundo job abaixo).

## Sem CGO

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
      APP: meuapp               # nome do executável
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Compilar e empacotar
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
    steps:
      - uses: actions/download-artifact@v4
        with:
          name: dist
          path: dist
      - uses: softprops/action-gh-release@v2
        with:
          generate_release_notes: true
          files: |
            dist/*.tar.gz
            dist/checksums.txt
```

## Com CGO

Troque o job `build` por uma matriz de runners nativos (runners ARM do
GitHub são gratuitos em repositórios públicos):

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
      APP: meuapp
      ARCH: ${{ matrix.arch }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Compilar e empacotar
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

No `publish`, baixe com `pattern: dist-*` e `merge-multiple: true`, e gere o
`checksums.txt` ali (`cd dist && sha256sum ./*.tar.gz > checksums.txt`).

Observação: binários com CGO linkam contra a glibc do runner. Compilar num
Ubuntu recente e rodar num Arch funciona (Arch tem glibc mais nova); o
contrário não.
