# Skills para autores de apps

Skills do [Claude Code](https://claude.com/claude-code) que ajudam a tornar um
app compatível com a OmaStore. A loja só indexa repositórios com um
`omastore.toml` na raiz declarando um app (plugins e temas ficam de fora).

| Skill | Para quê |
|---|---|
| [`omastore-manifest`](omastore-manifest/SKILL.md) | Escrever e validar o `omastore.toml` (validador em Python incluído, sem dependências) |
| [`omastore-release`](omastore-release/SKILL.md) | Workflow de release com tarballs Linux `x86_64`/`aarch64` e checksums (Go, Rust, CMake/Qt, genérico) |
| [`omastore-check`](omastore-check/SKILL.md) | Auditar o repositório com a própria OmaStore num HOME temporário, inclusive instalação de teste |

## Instalação

No projeto do seu app (só para ele):

```sh
mkdir -p .claude/skills
cp -r /caminho/para/OmaStore/skills/omastore-* .claude/skills/
```

Ou para todos os seus projetos:

```sh
mkdir -p ~/.claude/skills
cp -r /caminho/para/OmaStore/skills/omastore-* ~/.claude/skills/
```

Depois é só pedir ao Claude, por exemplo: "deixa esse app pronto para a
OmaStore" ou "por que meu app não aparece na OmaStore?".

## Manutenção

O validador em Python (`omastore-manifest/scripts/validate_manifest.py`)
duplica as regras de `backend/internal/manifest`. `make test-skills` roda os
dois sobre `tests/manifests/` e falha se discordarem; rode sempre que mudar o
formato do manifesto.
