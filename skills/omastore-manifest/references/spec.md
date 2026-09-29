# Especificação do `omastore.toml`

Fonte da verdade: `backend/internal/manifest` no repositório da OmaStore.
Este arquivo resume as regras que o `omastore lint-manifest` aplica.

## Campos

| Campo | Tipo | Regra |
|---|---|---|
| `kind` | string | `"app"` (padrão). `"plugin"` e `"theme"` são aceitos mas **não indexados**; outros valores são erro. |
| `name` | string | Nome exibido. Espaços normalizados; cortado em 80 caracteres (aviso). |
| `summary` | string | Resumo de uma frase. Cortado em 300 caracteres (aviso). |
| `categories` | lista | Categorias freedesktop (tabela abaixo). A primeira **principal** vira a categoria da loja; todas vão para o `.desktop`. Máx. 4. |
| `icon` | string | Caminho relativo no repositório, `.png` ou `.svg`. |
| `screenshots` | lista | Caminhos relativos (imagens) ou URLs `https://`. Máx. 8. |
| `terminal` | bool | `true` para CLI/TUI: o atalho abre num terminal. |
| `[linux.<arq>]` | tabela | `<arq>`: `x86_64`/`amd64`/`x64` ou `aarch64`/`arm64`. |
| `linux.<arq>.asset` | string | Nome do asset da release. Marcadores: `{version}` (tag sem `v`), `{tag}` (tag como está), `*` (qualquer sequência). Sem `/`. Não pode ser só `*`. |
| `linux.<arq>.exec` | string | Caminho do executável **dentro do pacote extraído**, relativo, sem `..`. Aceita `{version}` e `{tag}` (ex.: `app-{version}-x86_64-linux/bin/app`). Symlink para fora do pacote é recusado. |

Qualquer outro campo é erro no lint (`strict`), mas é ignorado na indexação.

## Categorias freedesktop

Principais (ao menos uma, senão o app não aparece em menus):
`AudioVideo`, `Audio`, `Video`, `Development`, `Education`, `Game`,
`Graphics`, `Network`, `Office`, `Science`, `Settings`, `System`, `Utility`.

Adicionais comuns (combine com uma principal): `Photography`, `RasterGraphics`,
`VectorGraphics`, `2DGraphics`, `Viewer`, `Recorder`, `AudioVideoEditing`,
`Player`, `TextEditor`, `IDE`, `TerminalEmulator`, `FileManager`,
`Monitor`, `Calendar`, `Email`, `Chat`, `WebBrowser`, `Emulator`,
`PackageManager`, `Security`, `Archiving`, `Calculator`.

## Como a loja escolhe o asset e o executável

1. Se `[linux.<arq da máquina>].asset` existir e casar com um asset da última
   release estável, é ele. O nome não precisa seguir nenhuma convenção.
2. Senão, heurística: arquitetura no nome (`x86_64`/`amd64`, `aarch64`/`arm64`),
   formatos em ordem `.tar.gz`/`.tar.xz`/`.tar.zst`/`.tar.bz2` > `.zip` >
   binário puro > `.AppImage` > `.pkg.tar.zst`. `.deb`, `.rpm`, fontes e
   builds de outros sistemas são ignorados.
3. Executável: `exec` do manifesto, se existir no pacote; senão o arquivo ELF
   (ou script com shebang e bit de execução) com o nome do repositório, de
   preferência em `bin/` ou `usr/bin/`.

## Exemplos

Mínimo (tudo deduzido):

```toml
kind = "app"
```

App de terminal com release no padrão:

```toml
kind = "app"
categories = ["System", "Monitor"]
icon = "assets/icon.svg"
terminal = true
```

Release com vários arquivos por arquitetura e executável fora do padrão:

```toml
kind = "app"
name = "RAWmakase"
categories = ["Graphics", "Photography"]
icon = "packaging/icons/rawmakase.svg"
screenshots = ["docs/images/screenshot.png"]

[linux.x86_64]
asset = "rawmakase-{version}-x86_64-linux.tar.gz"
exec = "usr/bin/rawmakase"
```
