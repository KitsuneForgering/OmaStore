# Release de app Rust

Use um runner nativo por arquitetura: cross-compilar Rust com dependências C
(GTK, OpenSSL, libraw) é frágil. Runners ARM do GitHub são gratuitos em
repositórios públicos.

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
      APP: meuapp               # nome do binário ([[bin]] ou nome do crate)
      ARCH: ${{ matrix.arch }}
    steps:
      - uses: actions/checkout@v4
      - uses: dtolnay/rust-toolchain@stable
      # Dependências de sistema do app, se houver:
      # - run: sudo apt-get update && sudo apt-get install -y libgtk-4-dev
      - name: Compilar e empacotar
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

Dicas:
- `strip = true` e `lto = true` no `[profile.release]` reduzem bastante o binário.
- Recursos (ícones, temas) que o app carrega em runtime devem ir para
  `share/` dentro do tarball e ser procurados relativos a
  `std::env::current_exe()`, não em `/usr/share`.
