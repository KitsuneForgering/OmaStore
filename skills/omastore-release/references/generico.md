# Release genérica

Para qualquer build que produza um executável (Zig, Nim, Crystal, um script
com shebang empacotado junto dos dados, ou um AppImage já gerado por outra
ferramenta), a regra é a mesma: um arquivo por arquitetura com nome
`<app>-<versão>-<arq>-linux.<ext>`.

## Tarball a partir de um diretório pronto

```yaml
name: Release

on:
  push:
    tags: ['v*']

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    env:
      VERSION: ${{ github.ref_name }}
      APP: meuapp
    steps:
      - uses: actions/checkout@v4
      # Substitua pelo build do seu projeto; o resultado deve ser um
      # executável em out/meuapp.
      - run: ./build.sh
      - name: Empacotar
        run: |
          ver="${VERSION#v}"
          dir="$APP-$ver-x86_64-linux"
          mkdir -p "dist/$dir/bin"
          install -m755 "out/$APP" "dist/$dir/bin/$APP"
          tar -C dist -czf "dist/$dir.tar.gz" "$dir"
          (cd dist && sha256sum ./*.tar.gz > checksums.txt)
      - uses: softprops/action-gh-release@v2
        with:
          generate_release_notes: true
          files: |
            dist/*.tar.gz
            dist/checksums.txt
```

## AppImage

A OmaStore instala AppImages, mas eles vêm por último na preferência (depois
de tarballs, zip e binário puro). Nome esperado:
`MeuApp-1.2.0-x86_64.AppImage`. Declare no manifesto se houver outro arquivo
Linux na mesma release:

```toml
[linux.x86_64]
asset = "MeuApp-{version}-x86_64.AppImage"
```

## Scripts

Um app em Python/Node/Bash pode ser distribuído como tarball com um lançador
`bin/meuapp` (shebang + bit de execução) e o código em `share/meuapp/`. O
lançador deve localizar o código relativo a ele mesmo:

```sh
#!/bin/sh
here=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
exec python3 "$here/share/meuapp/main.py" "$@"
```

A OmaStore cria em `~/.local/bin` um lançador que chama o caminho real do
seu executável, então `$0` sempre aponta para o arquivo dentro do pacote.
Dependências de runtime (Python, módulos) precisam estar documentadas no
README: a loja não instala dependências.
