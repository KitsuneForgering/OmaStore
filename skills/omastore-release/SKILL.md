---
name: omastore-release
description: Configura releases no GitHub que a OmaStore consegue instalar: workflow de GitHub Actions disparado por tag que compila o app para Linux (x86_64 e aarch64), empacota em <app>-<versão>-<arq>-linux.tar.gz com checksums e publica na release. Use quando o usuário quiser que o app seja instalável pela OmaStore/Omarchy, publicar binários Linux, automatizar releases, ou quando a loja mostrar o app como "sem binário para Linux" — mesmo que ele só diga "quero publicar uma versão".
---

# Releases instaláveis pela OmaStore

A OmaStore instala o binário da **última release estável** (não pre-release,
não draft) do repositório. Para isso a release precisa ter um arquivo para
Linux por arquitetura, com nome e conteúdo previsíveis. O manifesto
`omastore.toml` (skill `omastore-manifest`) aponta para esses arquivos; esta
skill faz os arquivos existirem.

## O formato-alvo

```
Release v1.2.0
├── meuapp-1.2.0-x86_64-linux.tar.gz
│     └── meuapp-1.2.0-x86_64-linux/
│           ├── bin/meuapp          ← executável (ELF ou script com shebang)
│           ├── share/…             ← dados, ícones (opcional)
│           └── LICENSE, README.md
├── meuapp-1.2.0-aarch64-linux.tar.gz
└── checksums.txt                   ← sha256sum de todos os tarballs
```

Por que assim:
- `<app>-<versão>-<arq>-linux.tar.gz` é reconhecido mesmo sem manifesto e
  casa com `asset = "meuapp-{version}-x86_64-linux.tar.gz"`.
- Um diretório de topo com `bin/` deixa o executável fácil de achar. Se ele
  tiver o nome do repositório, a loja encontra sozinha; se não, declare
  `exec = "meuapp-{version}-x86_64-linux/bin/meucomando"` no manifesto — o
  `{version}` é trocado pela versão de cada release.
- A loja confere o sha256 que o GitHub calcula por asset; o `checksums.txt`
  é um reforço útil para quem baixa à mão.
- Tudo é instalado em `~/.local/share/omastore/apps/…`, nunca em `/usr`.
  Programas que procuram dados em caminhos absolutos (`/usr/share/meuapp`)
  quebram; resolva caminhos relativos ao executável.

## Fluxo

1. **Identifique o ecossistema** e leia só a referência correspondente:
   - Go → `references/go.md`
   - Rust → `references/rust.md`
   - C/C++ com CMake, incluindo Qt → `references/cmake-qt.md`
   - qualquer outro (Zig, Nim, script empacotado, AppImage já pronto) →
     `references/generico.md`
2. **Descubra o nome do executável** e onde o build o coloca. Se o nome
   colidir com um comando do sistema (`ls`, `code`, `top`), avise: a OmaStore
   recusa instalar para não encobrir o comando.
3. **Crie `.github/workflows/release.yml`** a partir do modelo, ajustando nome
   do app e comandos de build. Mantenha:
   - disparo por tag `v*`;
   - a tag entrando por variável de ambiente (`VERSION: ${{ github.ref_name }}`),
     nunca interpolada direto em `run:` — um nome de tag malicioso viraria
     comando;
   - `permissions: contents: write` só no job que publica.
4. **Valide o workflow**, se possível:
   `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/release.yml`.
5. **Teste o empacotamento localmente** antes da primeira tag, rodando os
   mesmos comandos do job de build e conferindo o tarball:
   `tar tzf dist/meuapp-*-x86_64-linux.tar.gz | head`.
6. **Atualize o `omastore.toml`** para apontar para os assets (ou confirme que
   a heurística basta): `[linux.x86_64] asset = "meuapp-{version}-x86_64-linux.tar.gz"`.
7. **Explique como publicar**: `git tag v1.2.0 && git push origin v1.2.0`.
   Depois, a skill `omastore-check` confirma que a loja instala.

## O que não fazer

- Não publique só `.deb`/`.rpm`: a OmaStore ignora esses formatos.
- Não marque a release como pre-release se quiser que ela apareça na loja.
- Não inclua scripts de instalação que precisem rodar: a loja nunca executa
  nada do pacote durante a instalação.
