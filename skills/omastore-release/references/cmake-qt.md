# Release de app C/C++ (CMake, incluindo Qt)

Apps Qt/GTK linkam contra as bibliotecas do sistema. Para rodar no Omarchy
(Arch), compile **num container Arch**: assim o binário usa as mesmas versões
de Qt que o usuário tem instaladas (o app passa a depender dos pacotes
`qt6-base`, `qt6-declarative` etc., que o README deve listar).

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
      APP: meuapp                # nome do executável gerado pelo CMake
    steps:
      - name: Dependências
        run: pacman -Syu --noconfirm --needed base-devel git cmake ninja qt6-base qt6-declarative
      - uses: actions/checkout@v4
      - name: Compilar e empacotar
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

Com `CMAKE_INSTALL_PREFIX=/` e `GNUInstallDirs`, o tarball fica com
`<dir>/bin/meuapp`, `<dir>/share/…`. Para aarch64, repita o job em
`ubuntu-24.04-arm` (o container `archlinux` não tem imagem ARM oficial;
use `menci/archlinuxarm` ou compile sem container e documente as versões).

Cuidados:
- Recursos QML embutidos com `qt_add_qml_module` não dependem de caminho.
  Arquivos lidos do disco devem ser resolvidos a partir de
  `QCoreApplication::applicationDirPath()` (ex.: `../share/meuapp`), nunca de
  `/usr/share`.
- `RPATH` absoluto apontando para o diretório de build quebra fora do CI;
  `cmake --install` corrige por padrão.
