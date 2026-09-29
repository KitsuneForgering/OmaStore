# Manifesto `omastore.toml` no repositório do app

> **Status:** implementado na Fase 11 (`backend/internal/manifest`) e tornado
> **obrigatório** na Fase 15: só repositórios com `omastore.toml` de app são
> indexados. Formato
> documentado em [`../autores.md`](../autores.md#6-declarando-tudo-com-omastoretoml-opcional).

## Evidência

As heurísticas acertam na maioria dos casos, mas os testes reais mostraram
falhas que só o autor do app resolve com certeza:

- **Categoria:** `pch/rawmakase` não tem topics; caía em `Utility` até
  adicionarmos "lightroom" à lista de palavras. `devmobasa/wayscriber` caía em
  `System` por causa do topic `wayland`.
- **Asset:** `omacom/try-omarchy-windows` publicava `vmlinuz-linux` e um zip
  de Windows, e ambos eram aceitos como binário Linux.
- **Executável:** o tarball do RAWmakase traz `usr/bin/rawmakase` (script) e
  `usr/lib/rawmakase/rawmakase` (ELF). Escolhemos certo pelo nome, mas por sorte.
- **Terminal:** `Terminal=true` é deduzido só dos topics `cli`/`tui`.
- **Ícone:** em `erans/hyprmon`, o arquivo `hyprmon.png` casa com o nome do
  repositório e vira o ícone, mas é uma screenshot retangular do app.

Cada correção dessas exige mudar regras e incrementar `index.Version`.

## Proposta

Ler, se existir, um `omastore.toml` na raiz do repositório (pela Trees API,
que já listamos). Tudo opcional; o que faltar continua heurístico.

```toml
name = "RAWmakase"
summary = "Alternativa livre ao Lightroom"
categories = ["Graphics", "Photography"]   # freedesktop
icon = "packaging/rawmakase.svg"
screenshots = ["docs/images/screenshot.png"]
terminal = false

[linux.x86_64]
asset = "rawmakase-{version}-x86_64-linux.tar.gz"   # padrão com {version}
exec = "usr/bin/rawmakase"                           # dentro do pacote
[linux.aarch64]
asset = "rawmakase-{version}-aarch64-linux.tar.gz"
exec = "usr/bin/rawmakase"
```

Regras:

- O manifesto **nunca** amplia permissões: `exec` continua precisando ficar
  dentro do diretório extraído; os caminhos passam pelas mesmas validações.
- Valores do manifesto têm prioridade sobre as heurísticas, mas os campos de
  texto passam pelo mesmo escape do `.desktop`.
- Publicar um guia (Fase 9) e um validador: `omastore lint-manifest <dir>`.

## Custo

Pequeno a médio: um parser TOML (`github.com/pelletier/go-toml/v2` ou
`BurntSushi/toml`), uma coluna/JSON no banco, integração em `extract()` e
`SelectAsset()`, e testes. Incrementar `index.Version`.

## Riscos

- Adoção depende dos autores; por isso o manifesto é opcional.
- Um manifesto desatualizado (nome de asset antigo) deixa o app não instalável.
  Mitigação: se o padrão não casar com nenhum asset, cair na heurística e
  registrar aviso.
