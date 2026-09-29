---
name: omastore-check
description: Audita se um repositório do GitHub é instalável pela OmaStore (loja de apps do Omarchy) rodando a própria OmaStore num HOME temporário: valida o omastore.toml, indexa, confere release, arquitetura, checksum, ícone e, opcionalmente, instala e desinstala de verdade. Use quando o usuário perguntar se o app "funciona na OmaStore", por que ele não aparece ou não instala na loja, antes de publicar uma release, ou para revisar a compatibilidade de um projeto com o Omarchy.
---

# Auditoria de compatibilidade com a OmaStore

A melhor forma de saber se a loja instala um app é deixar a loja tentar. O
script desta skill faz exatamente isso, num HOME descartável: nada é gravado
no `~/.local` do usuário e nenhum binário do app é executado.

## Fluxo

1. **Descubra o `owner/repo`** (de `git remote get-url origin` se estiver no
   repositório) e se já existe um `omastore.toml` publicado ou só local.

2. **Rode a auditoria.** Precisa da CLI `omastore` (pacote da OmaStore ou
   `$OMASTORE=/caminho/omastore`) e de `gh` autenticado:
   ```sh
   # o que a loja vê hoje (manifesto publicado):
   python3 <dir-desta-skill>/scripts/omastore_check.py owner/repo
   # antes do push, com o manifesto local:
   python3 <dir-desta-skill>/scripts/omastore_check.py owner/repo --manifest ./omastore.toml
   # auditoria completa, com instalação de teste (baixa a release):
   python3 <dir-desta-skill>/scripts/omastore_check.py owner/repo --manifest ./omastore.toml --install
   ```
   `--json` dá a saída estruturada, útil para CI. Código de saída 1 = há
   falhas.

3. **Interprete o relatório** para o usuário. Cada linha é ✅, ⚠️ ou ❌:
   - ❌ impede o app de entrar ou de instalar: resolva antes de publicar.
   - ⚠️ não impede, mas piora a experiência (sem ícone, sem screenshots, sem
     build ARM, binário com caminhos absolutos em `/usr`).
   Explique a causa provável de cada ❌ em termos do projeto dele, não só a
   mensagem do script.

4. **Encaminhe a correção** para a skill certa:
   - manifesto ausente, inválido, ícone ou executável errado →
     `omastore-manifest`;
   - sem release, sem asset Linux, sem ARM, sem checksum →
     `omastore-release`;
   - "Caminhos absolutos": o app procura dados em `/usr/share/<app>`, mas a
     loja instala em `~/.local/share/omastore/apps/…`. A correção é no código
     do app: resolver arquivos relativos ao executável.
   - conflito de nome de comando: o executável tem o nome de um comando do
     sistema (`ls`, `top`…) ou `omastore*`; renomeie o binário.

5. **Rode de novo** depois das correções até o resultado ser "compatível com
   a OmaStore". Com o manifesto local aprovado, lembre o usuário de fazer
   commit e push do `omastore.toml`: a loja só lê o arquivo do branch padrão.

## Sem a CLI `omastore`

Se a OmaStore não estiver instalada e o usuário não quiser instalá-la, faça
uma checagem parcial: valide o manifesto com o validador da skill
`omastore-manifest` (incluindo `--tag/--asset` com os nomes de
`gh release view --json assets`) e confira manualmente o formato descrito em
`omastore-release`. Deixe claro que a instalação não foi testada.
