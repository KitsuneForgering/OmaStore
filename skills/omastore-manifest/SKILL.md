---
name: omastore-manifest
description: Cria, revisa e valida o omastore.toml que um app precisa ter na raiz do repositório para entrar na OmaStore (a loja de apps do Omarchy). Use sempre que o usuário quiser publicar, listar ou "colocar na OmaStore" um app, tornar um projeto compatível com a OmaStore/Omarchy, escrever ou corrigir o omastore.toml, ou quando o app não aparece na loja — mesmo que ele não cite o manifesto pelo nome.
---

# Manifesto da OmaStore (`omastore.toml`)

A OmaStore só indexa repositórios que têm um `omastore.toml` na raiz do branch
padrão e que declaram um **app** (não plugin nem tema do Omarchy). O arquivo
pode até estar vazio: a presença dele é o opt-in do autor, e o que não for
declarado a loja deduz sozinha. O manifesto serve para declarar o que a
dedução erraria — ícone, executável, qual arquivo da release instalar.

A especificação completa está em `references/spec.md`. Leia-a antes de
escrever campos que você não conhece de cor.

## Fluxo

1. **Confirme que é um app.** Um programa standalone que o usuário abre ou
   roda no terminal. Se for tema, plugin, dotfiles ou extensão do Omarchy,
   explique que a OmaStore não distribui esse tipo de projeto e pare: um
   `kind = "theme"` só serve para deixar isso explícito.

2. **Levante os fatos do repositório** (não invente; cada campo errado vira
   um app quebrado na loja):
   - nome de exibição (README, título, `Cargo.toml`/`package.json`/`go.mod`);
   - resumo de uma frase (descrição do repo no GitHub, primeiro parágrafo do README);
   - categoria freedesktop (veja a tabela em `references/spec.md`);
   - ícone: um PNG (≥ 256×256) ou SVG versionado no repo;
   - screenshots versionadas ou URLs `https://`;
   - se roda no terminal (CLI/TUI) → `terminal = true`;
   - releases: `gh release view --json tagName,assets` mostra a tag e os nomes
     dos assets. Sem release com binário Linux o app entra na loja mas não
     pode ser instalado — nesse caso, sugira a skill `omastore-release`.

3. **Decida o que declarar.** Prefira um manifesto curto. Declare sempre:
   `kind`, `categories` e `icon`. Declare `[linux.<arq>]` quando a release tiver
   mais de um arquivo por arquitetura, nomes fora do padrão
   `<app>-<versão>-<arq>-linux.tar.gz`, ou quando o executável não tiver o
   nome do repositório (ex.: um script `bin/app` que chama `lib/app/app`).
   Use `{version}` no padrão de asset, nunca a versão fixa — o manifesto tem
   que continuar valendo nas próximas releases.

4. **Escreva o arquivo** na raiz do repositório, com comentários curtos só
   onde a escolha não for óbvia.

5. **Valide** e corrija até não haver erros:
   ```sh
   # com a OmaStore instalada (é a referência):
   omastore lint-manifest .
   # sem ela, o validador desta skill aplica as mesmas regras:
   python3 <dir-desta-skill>/scripts/validate_manifest.py .
   ```
   Para conferir os padrões de asset contra a release real:
   ```sh
   python3 <dir-desta-skill>/scripts/validate_manifest.py . --tag "$TAG" \
     $(gh release view --json assets --jq '.assets[].name' | sed 's/^/--asset /')
   ```

6. **Mostre o resultado antes do push**, se a OmaStore estiver instalada:
   ```sh
   omastore index --manifest ./omastore.toml owner/repo && omastore show owner/repo
   ```
   Isso indexa usando o arquivo local (num banco temporário, se preferir:
   `HOME=$(mktemp -d) omastore ...`). Confira nome, categoria, ícone,
   screenshots e se aparece `instalável: true`.

7. **Resuma para o usuário**: o que foi declarado e por quê, o que ficou para
   a dedução automática, e o próximo passo (commit + push do `omastore.toml`;
   se a release não tiver binário Linux, a skill `omastore-release`).

## Armadilhas comuns

- **Arquivo fora da raiz ou em outro branch**: a loja lê `HEAD:omastore.toml`
  do branch padrão.
- **TOML inválido tira o app da loja** (campos inválidos isolados só geram
  aviso). Sempre valide.
- **Campo com nome errado** (`icone`, `binary`): o lint estrito acusa; a
  indexação ignora em silêncio.
- **Executável com nome de comando do sistema** (`ls`, `top`, `code`) ou
  `omastore*`: a instalação é recusada para não encobrir o comando existente.
  Sugira renomear o binário.
- **Caminhos absolutos em `exec`** ou `..`: recusados. `exec` é relativo à
  raiz do pacote extraído (ex.: `usr/bin/app` para um `.pkg.tar.zst`,
  `app-1.0/bin/app` para um tarball com diretório de topo).
