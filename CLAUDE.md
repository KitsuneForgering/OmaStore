# CLAUDE.md

Este arquivo orienta o Claude Code (claude.ai/code) ao trabalhar neste repositório.

## Visão geral

OmaStore é uma loja de aplicativos para o [Omarchy](https://omarchy.org) (Arch Linux + Hyprland).
Ela indexa aplicativos publicados no GitHub (ex.: OmaVM, OmaDesign, OmaPhoto, Rawmakase), mostra cada
um com descrição, ícone e screenshots tirados do próprio repositório, e instala o binário da última
release com um clique, gerando o `.desktop` correspondente.

Os itens do catálogo são **aplicativos standalone**, não plugins nem temas do Omarchy. **Só entram no
catálogo repositórios com `omastore.toml` na raiz** declarando um app (`kind = "app"`, o padrão; `plugin` e
`theme` ficam de fora). A presença do arquivo é o opt-in do autor; o conteúdo pode estar vazio e o resto é
deduzido por heurísticas. Formato em `docs/autores.md`; skills para autores em `skills/`.

## Arquitetura

Dois processos com responsabilidades separadas:

```
┌──────────────────────────┐   JSON-RPC sobre Unix socket   ┌─────────────────────────────┐
│ frontend (C++ / Qt Quick)│ ─────────────────────────────▶ │ backend (Go, omastored)     │
│ QML + modelos QObject    │ ◀───────────────────────────── │ indexador, cache, instalador│
└──────────────────────────┘   eventos de progresso         └──────────────┬──────────────┘
                                                                           │
                                               go-github (API) · go-git (clone raso) · SQLite
```

- **Backend (Go)** contém toda a lógica: descoberta, indexação, cache, download, instalação e
  desinstalação. Deve funcionar sozinho (há uma CLI para testes sem a GUI).
- **Frontend (C++/QML)** é só apresentação. Não acessa a rede, o GitHub nem o banco diretamente;
  tudo passa pelo backend.
- **IPC**: JSON-RPC 2.0 (uma mensagem JSON por linha) em `$XDG_RUNTIME_DIR/omastore.sock`.
  Operações longas (indexação, instalação) retornam um id de job e emitem notificações de progresso
  pelo mesmo socket. O protocolo está em `docs/ipc.md`; mantenha-o atualizado ao mudar métodos ou DTOs.
- **Ideias** ainda não aceitas ficam em `docs/ideas/` (uma por arquivo, com evidência e custo);
  ao aceitar uma, transforme-a em itens no `TODO.md`.

### Backend

- `github.com/google/go-github` — busca de repositórios, metadados (stars, topics, `pushed_at`),
  releases e assets. Autenticação opcional por `GITHUB_TOKEN` (ou `gh auth token`) para evitar rate limit.
  Usar requisições condicionais (ETag / `If-None-Match`) sempre que possível.
- `github.com/go-git/go-git/v5` — clone raso (`Depth: 1`) quando a API não basta: extrair ícones,
  screenshots e arquivos de metadados do repositório. Clones ficam em `$XDG_CACHE_HOME/omastore/repos/`.
- `github.com/mattn/go-sqlite3` — cache/índice em `$XDG_DATA_HOME/omastore/omastore.db`.
  Exige CGO (`CGO_ENABLED=1`).

### Frontend

- Qt 6, Qt Quick + QML, build com CMake.
- A camada C++ expõe ao QML: um cliente IPC (`QLocalSocket`) e modelos `QAbstractListModel`
  (catálogo, instalados, jobs). Nada de lógica de negócio em QML além de apresentação e filtros simples.
- Visual segue o tema ativo do Omarchy: `colors.toml` em `~/.local/state/omarchy/current/theme/` (instalações
  antigas: `~/.config/omarchy/current/theme/`), recarregado ao trocar de tema.
- O frontend nunca acessa a rede: imagens vêm do daemon (`image.get`, provider `image://omastore/`), o
  QML engine usa um `QNetworkAccessManager` que bloqueia URLs remotas e o README é exibido sem imagens.
- O frontend nunca procura o `omastored` no `PATH` (que inclui `~/.local/bin`, onde ficam apps baixados):
  usa `$OMASTORED`, o diretório do executável ou `/usr/bin`.

## Pipeline de indexação

1. **Descoberta** — busca de código por `filename:omastore.toml`, busca por topic (`topic:omarchy`),
   sementes e listas curadas (`list:owner/repo` em `seeds.txt`). Sem `omastore.toml` de app, o repositório
   não entra (e sai, se estava). Sem release com binário Linux, entra mas não é instalável.
2. **Verificação de cache** — para cada repositório, comparar `pushed_at`, SHA do commit HEAD e tag
   da última release com o que está no SQLite. **Se nada mudou, não reprocessar.** Esse é o motivo de
   existir o banco; nunca remova essa checagem por conveniência. Como consequência, **ao mudar regras
   de extração/classificação (assets, categorias, README), incremente `index.Version`**; senão os repos
   já gravados nunca são reclassificados.
3. **Extração** — README (renderizado como descrição curta + longa), ícone, screenshots, licença,
   topics e stars. Preferir a API; clonar só se precisar de arquivos que a API não entrega bem.
4. **Classificação** — categoria a partir dos topics; ordenação usando stars (e recência como desempate).
5. **Persistência** — gravar tudo numa transação e registrar o SHA/tag processados.

## Instalação de um app

1. Escolher o asset da release pela arquitetura (`x86_64`/`amd64`, `aarch64`/`arm64`) e formato
   (binário puro, `.tar.gz`, `.zip`, AppImage).
2. Baixar para diretório temporário e validar checksum quando a release publicar um (`*.sha256`, `checksums.txt`).
3. Extrair em `$XDG_DATA_HOME/omastore/apps/<owner>__<repo>/<versão>/` e criar link em `~/.local/bin/`.
4. Salvar o ícone em `~/.local/share/icons/hicolor/<tam>/apps/` e gerar
   `~/.local/share/applications/omastore-<owner>-<repo>.desktop` (Name, Comment, Exec absoluto,
   Icon, Categories derivadas dos topics).
5. Registrar a instalação no SQLite (versão, caminhos criados) para permitir update e desinstalação limpa.

Regras:
- Nunca executar o binário baixado durante a instalação.
- Proteger contra path traversal ao extrair arquivos (zip slip) e contra symlinks que saiam do diretório de destino.
- Escapar todos os campos vindos do repositório antes de escrever no `.desktop`.
- Nada é instalado fora do `$HOME` do usuário; sem `sudo`.
- Desinstalar remove apenas os caminhos registrados no banco.

## Esquema do banco (resumo)

- `repos` — `full_name` (PK), descrição, stars, topics, `pushed_at`, `head_sha`, `latest_tag`, `etag`, `indexed_at`.
- `apps` — dados de exibição derivados do repo: nome, resumo, README, ícone, categoria, score.
- `assets` — assets de release por repo/tag (nome, url, arch, formato, checksum).
- `installs` — app instalado, versão, data, lista de arquivos criados.

Mudanças de esquema via migrações numeradas em `backend/internal/store/migrations/`; nunca editar uma migração já publicada.

## Estrutura de diretórios

```
backend/
  cmd/omastored/        # daemon (servidor IPC)
  cmd/omastore/         # CLI de depuração: index, list, show, install, uninstall, update
  internal/app/         # monta os serviços (usado pela CLI e pelo daemon)
  internal/github/      # wrapper do go-github
  internal/gitrepo/     # clones rasos via go-git + busca de ícone/screenshots
  internal/index/       # descoberta, extração, classificação (index.Version)
  internal/manifest/    # omastore.toml opcional do app (validação estrita no lint)
  internal/store/       # SQLite + migrações
  internal/install/     # download, verificação, extração, lançador, .desktop
  internal/imagecache/  # imagens remotas para o frontend
  internal/notify/      # notificações desktop via D-Bus (sem executar notify-send)
  internal/search/      # busca BM25 e "apps parecidos" (TF-IDF), determinísticos
  internal/rpc/         # servidor JSON-RPC, jobs, socket activation
frontend/
  CMakeLists.txt        # lib omastore-core + app + testes (ctest)
  src/                  # main.cpp, cliente IPC, modelos, tema, image provider
  qml/                  # telas e componentes
  tests/                # Qt Test com daemon falso (QLocalServer)
packaging/              # PKGBUILD, unidades systemd, .desktop e ícone da loja
docs/                   # ipc.md, autores.md, ideas/, screenshots/
```

## Comandos

Tudo pelo `Makefile` na raiz (`make help` lista os alvos):

```sh
make                 # compila backend (bin/omastore, bin/omastored) e frontend
make backend         # só os binários Go
make frontend        # só o frontend (cmake + ninja em frontend/build)
make test            # todos os testes
make test-backend    # go test -race ./...
make test-backend TESTFLAGS='-run TestNome ./internal/index'   # um teste
make test-frontend   # ctest (QT_QPA_PLATFORM=offscreen)
make check           # gofmt + vet + testes do backend (o que a CI roda)
make fmt             # gofmt -w
make run ARGS=index  # indexar sem GUI
make run-gui         # compila e abre a interface Qt
make clean
make release && make install DESTDIR=... PREFIX=/usr   # o que o PKGBUILD faz
make dist VERSION=v1.2.3   # tarball reprodutível + .sha256 em dist/ (usado pelo workflow de release)
make pkgbuild-bin VERSION=v1.2.3   # PKGBUILD do omastore-bin com o sha256 do tarball
```

Variáveis úteis: `BUILD_TYPE` (default `Debug`), `GENERATOR` (default `Ninja`), `TESTFLAGS`, `ARGS`.
O Makefile exporta `CGO_ENABLED=1` (exigido pelo go-sqlite3). A CI chama os mesmos alvos.

## Convenções

- Go: `gofmt`, erros com contexto (`fmt.Errorf("...: %w", err)`), `context.Context` em toda operação de rede/IO longa.
- Testes do backend não acessam a rede: usar fixtures e `httptest` para simular a API do GitHub.
- C++: C++20, sem lógica de rede no frontend, sinais/slots Qt para atualizar modelos.
- Respeitar as variáveis XDG (`XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_RUNTIME_DIR`) com os defaults da especificação.
