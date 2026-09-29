# TODO — OmaStore

Roteiro de implementação, na ordem em que as peças dependem umas das outras.
Cada fase deve terminar com `go test ./...` (ou `ctest`) passando.

## Fase 0 — Fundação

- [x] `backend/go.mod` (módulo, versão do Go) com `go-github`, `go-git/v5`, `go-sqlite3`
- [x] Estrutura de diretórios do backend conforme o CLAUDE.md
- [x] Pacote `internal/xdg`: resolver `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_RUNTIME_DIR` com defaults da especificação
- [x] Logging estruturado (`log/slog`) com nível configurável
- [x] `frontend/CMakeLists.txt` mínimo (Qt 6 Quick, C++20, janela vazia abrindo) — *build local pendente: cmake/ninja não instalados*
- [x] `Makefile` na raiz (build, testes, vet, fmt, run, clean)
- [x] CI (GitHub Actions) chamando os alvos do `Makefile`
- [x] Atualizar `.gitignore` (`frontend/build/`, binários do Go)

## Fase 1 — Armazenamento (`internal/store`)

- [x] Abrir SQLite com WAL, `foreign_keys=ON`, `busy_timeout`
- [x] Runner de migrações numeradas (tabela `schema_migrations`, migrações embutidas via `embed`)
- [x] `0001_init.sql`: tabelas `repos`, `apps`, `assets`, `installs` (+ índices)
- [x] Repositório de dados: upsert de repo/app/assets numa única transação
- [x] Consultas: listar catálogo (filtro por categoria, busca por texto, ordenação por score), obter app, listar instalados
- [x] Testes com banco em arquivo temporário

## Fase 2 — GitHub (`internal/github`)

- [x] Cliente autenticado por `GITHUB_TOKEN`, com fallback para `gh auth token`, e anônimo se nenhum existir
- [x] Busca por topic (`topic:omarchy`) com paginação
- [x] Lista curada de repositórios semente (arquivo embutido, `internal/index/seeds.txt`)
- [x] Metadados do repo: stars, topics, `pushed_at`, licença, HEAD SHA
- [x] Última release + assets (inclui `digest` sha256 que a API já fornece por asset)
- [x] README via API (conteúdo bruto)
- [x] Requisições condicionais com ETag / `If-None-Match` (guardar ETag em `repos.etag`)
- [x] Tratamento de rate limit (ler headers, esperar/abortar com erro claro)
- [x] Testes com `httptest` + fixtures JSON em `testdata/`

## Fase 3 — Clone raso (`internal/gitrepo`)

- [x] Clone `Depth: 1` em `$XDG_CACHE_HOME/omastore/repos/<owner>__<repo>`
- [x] Atualizar clone existente (fetch raso) em vez de re-clonar
- [x] Localizar ícone (convenções: `icon.png`, `assets/icon.*`, `*.svg` na raiz, etc.)
- [x] Localizar screenshots (`screenshots/`, `preview`, `demo`; imagens do README ficam no indexador)
- [x] Busca de ícone/screenshots como função pura sobre lista de caminhos; o indexador usa a Trees API e só clona se a árvore vier truncada
- [x] Testes com repositório git local criado no próprio teste

## Fase 4 — Indexador (`internal/index`)

- [x] Descoberta: união de busca por topic + sementes, sem duplicatas
- [x] **Checagem de cache**: comparar `pushed_at`, `head_sha` e `latest_tag` com o SQLite; pular se nada mudou
- [x] Filtrar repos sem release com binário Linux (não instaláveis)
- [x] Extração do README: resumo curto (primeiro parágrafo) + descrição longa
- [x] Resolver URLs relativas de imagens do README para URLs absolutas (raw.githubusercontent)
- [x] Classificação: mapa topic → categoria (ex.: `photo` → Graphics, `vm` → System)
- [x] Score: stars com recência (`pushed_at`) como desempate
- [x] Detecção de arch/formato dos assets (`x86_64`/`amd64`, `aarch64`/`arm64`; binário, `.tar.gz`/`.xz`/`.bz2`/`.zst`, `.zip`, AppImage, `.pkg.tar.zst`; descarta `.deb`/`.rpm`, fontes e outros SOs)
- [x] Detectar asset de checksum (`*.sha256`, `checksums.txt`)
- [x] Persistência transacional + registro de SHA/tag processados
- [x] `index.Version` (migração 0002): mudar regras do indexador reprocessa repos antigos
- [x] Concorrência limitada (worker pool) e cancelamento via `context`
- [x] Atualização leve quando só stars/descrição mudam (sem README/árvore)
- [x] Repos arquivados, removidos (404) ou renomeados saem do catálogo; `Prune` opcional
- [x] Testes: repo inalterado não é reprocessado; repo novo é indexado; repo sem binário é excluído

## Fase 5 — Instalador (`internal/install`)

- [x] Seleção do asset pela arquitetura da máquina (`runtime.GOARCH`)
- [x] Download para diretório temporário com progresso (callback de bytes)
- [x] Verificação de checksum: `digest` da API, senão `*.sha256`/`checksums.txt` (formatos GNU e BSD, sha256/sha512); falhar se não bater
- [x] Extração `.tar.gz`/`.xz`/`.bz2`/`.zst`, `.zip` e `.pkg.tar.zst` (só `usr/`, sem scripts de instalação) com proteção contra zip slip e symlinks para fora do destino
- [x] Instalar em `$XDG_DATA_HOME/omastore/apps/<owner>__<repo>/<versão>/`
- [x] Identificar o executável principal (nome do repo, bit executável, único binário)
- [x] `chmod +x` e link em `~/.local/bin/`
- [x] Ícone em `~/.local/share/icons/hicolor/<tam>/apps/` (redimensionar ou usar `scalable/` para SVG)
- [x] Gerar `omastore-<owner>-<repo>.desktop` com escaping correto de todos os campos
- [x] Rodar `update-desktop-database` / `gtk-update-icon-cache` se disponíveis (sem falhar se ausentes)
- [x] Registrar instalação em `installs` com lista de arquivos criados
- [x] Rollback se qualquer etapa falhar no meio
- [x] Desinstalação: remover apenas caminhos registrados
- [x] Update: instalar nova versão, trocar link, remover versão antiga
- [x] Garantir: nunca executar o binário, nada fora de `$HOME`, sem `sudo` (hooks do sistema por caminho absoluto, nunca pelo `PATH`)
- [x] Nunca sobrescrever arquivos de terceiros em `~/.local/bin` ou `applications/` (`ErrConflict`)
- [x] Recusar lançadores que encobririam comandos do sistema (`/usr/bin/<cmd>` existe) ou do OmaStore
- [x] `.desktop` validado com `desktop-file-validate` nos testes (quando disponível)
- [x] Testes: zip slip, symlink malicioso, escaping do `.desktop`, checksum inválido, rollback
- [x] Lançador em `~/.local/bin` é um script `exec` (não symlink): apps que usam `dirname "$0"` quebravam via link

## Fase 6 — CLI de depuração (`cmd/omastore`)

- [x] `omastore index [--force] [--prune] [--max N] [owner/repo...]`
- [x] `omastore list [--category X] [--query Q] [--installed] [--all] [--json]` e `omastore categories`
- [x] `omastore show [--json] <owner/repo>`
- [x] `omastore install <owner/repo>`
- [x] `omastore uninstall <owner/repo>`
- [x] `omastore update [<owner/repo>]`
- [x] Serviços montados em `internal/app` (compartilhado com o daemon)
- [x] Testado contra o GitHub real (HOME isolado): 34 repos indexados, 2ª execução sem reprocessar; omadesign e rawmakase instalados e removidos

## Fase 7 — Daemon e IPC (`internal/rpc`, `cmd/omastored`)

- [x] Servidor JSON-RPC 2.0 (NDJSON) em `$XDG_RUNTIME_DIR/omastore.sock` (permissão `0600`, remove socket órfão, recusa 2º daemon, erro claro para caminho > 107 bytes)
- [x] Métodos documentados em `docs/ipc.md`: `daemon.hello`, `catalog.list/get/categories`, `installs.list`, `index.start`, `install.start`, `update.start`, `install.uninstall`, `jobs.list/cancel`, `image.get`
- [x] Gerenciador de jobs (id, estado, progresso, cancelamento)
- [x] Notificações (`job.started/progress/done/failed`, `catalog.changed` sempre depois do fim do job); progresso limitado a ~10/s
- [x] Um job de indexação por vez; instalações do mesmo app serializadas
- [x] Encerramento limpo (SIGTERM, cancelar jobs, fechar DB)
- [x] Unidades systemd `--user` com socket activation (`packaging/systemd/`)
- [x] Testes do servidor com cliente em memória
- [x] DTOs camelCase estáveis, separados das structs internas; erros do backend mapeados para códigos fixos
- [x] Cache de imagens (`internal/imagecache`, método `image.get`): só https, conteúdo validado, downloads simultâneos compartilhados
- [x] Testado com o daemon real (cliente Python pelo socket): hello, catálogo, imagem, job de índice, SIGTERM remove o socket

## Fase 8 — Frontend (`frontend/`)

- [x] Cliente IPC em C++ (`QLocalSocket`, framing JSON-RPC, reconexão)
- [x] Iniciar `omastored` se o socket não existir (nunca pelo `PATH`: `$OMASTORED`, diretório do app, `/usr/bin`)
- [x] Modelos `QAbstractListModel`: catálogo, instalados, jobs
- [x] Tela de catálogo: grid com ícone, nome, resumo, stars; busca e filtro por categoria
- [x] Tela de detalhes: descrição longa, screenshots, licença, versão, botão Instalar/Remover/Atualizar
- [x] Barra de progresso ligada às notificações de job
- [x] Tela de instalados
- [x] Cache de imagens remotas (via backend: `image.get`, feito na Fase 7)
- [x] Cores do tema ativo (`~/.local/state/omarchy/current/theme/colors.toml`, com fallback) e recarga ao trocar de tema
- [x] Testes com `ctest` (modelos + cliente IPC contra servidor falso)
- [x] Frontend sem rede: `QNetworkAccessManager` do QML bloqueia URLs remotas; README exibido sem imagens; imagens via `image://omastore/`
- [x] Primeira execução com catálogo vazio dispara `index.start`; confirmação antes de instalar release sem checksum
- [x] Atalhos: `/` busca, `Esc` volta, `Ctrl+R` atualiza catálogo; `--open owner/repo` na linha de comando
- [x] Verificado contra o daemon real (`--screenshot` em modo offscreen)

## Fase 9 — Empacotamento e lançamento

- [x] `PKGBUILD` (`packaging/arch/`, pacote `omastore-git`) com build PIE/trimpath, `check()` e `make install DESTDIR`
- [x] Entrada `.desktop` da própria OmaStore
- [x] README com screenshots, instalação e como publicar um app compatível
- [x] Guia para autores de apps (`docs/autores.md`): topic `omarchy`, nomes de assets, ícone, checksums
- [x] `make release`/`install`/`uninstall`; frontend reconfigura quando `BUILD_TYPE` muda
- [ ] Publicar no AUR (depende do primeiro commit/push; ver `docs/ideas/distribuicao.md`)

## Fase 10 — Busca e recomendação (`internal/search`)

Busca e "apps parecidos" locais e determinísticos (mesma entrada, mesmo
resultado), sem serviço externo nem LLM.

- [x] Tokenização: minúsculas, sem acentos, stopwords pt/en, plural simples e -ing/-ed (passo 1b de Porter)
- [x] Ranking BM25 com pesos por campo (nome > topics > resumo > README) e estrelas como desempate
- [x] Tolerância a erro: prefixo e distância de edição 1 (só para termos fora do vocabulário; idf das variantes limitado ao do termo exato); E lógico com fallback para OU
- [x] "Apps parecidos": TF-IDF (topics, categoria, resumo, README) + cosseno; empate resolvido pelo nome
- [x] Índice em memória no daemon, reconstruído quando o catálogo muda
- [x] `catalog.list` com `query` usa o ranking; novo método `catalog.similar`; CLI `omastore similar`
- [x] Frontend: seção "Apps parecidos" no detalhe
- [x] Testes: ranking esperado, erro de digitação, acentos, determinismo, parecidos
- [x] Consultas em português: dicionário pt→en de termos de apps ("editor de fotos", "voz para texto", "calendário")
- [x] Calibrado no catálogo real (34 repos): limiar de similaridade 0,12 (pares reais > 0,2; ruído 0,09–0,16)

## Fase 11 — Manifesto `omastore.toml` (ver `docs/ideas/manifesto.md`)

- [x] Parser e validação (campos opcionais; caminhos passam pelas mesmas travas)
- [x] Indexador lê o manifesto pela árvore e sobrepõe as heurísticas (nome, resumo, categorias, ícone, screenshots, terminal)
- [x] Asset por arquitetura com padrão `{version}` e executável declarado; fallback para heurística se não casar
- [x] `omastore lint-manifest <dir>`; documentar em `docs/autores.md`; incrementar `index.Version`
- [x] Migração 0003 (`apps.manifest`); instalador usa asset/exec/terminal/categorias declarados; `exec` por symlink para fora é recusado
- [x] Manifesto inválido nunca derruba a indexação; só é buscado quando aparece na árvore

## Fase 12 — Descoberta em lote (ver `docs/ideas/descoberta-graphql.md`)

- [x] Consulta GraphQL em lote dos campos da checagem de cache (com token); REST continua sem token
- [x] Repos sem release pulam README/árvore
- [x] Sementes a partir de listas curadas (links `github.com/owner/repo` num README)
- [x] Medido com ~230 repos reais: 1ª indexação 863 → 173 requisições (76 s → 45 s); reindexação 497 → 12 requisições (44 s → 15 s, lotes de 25 com 3 em paralelo)
- [x] `omastore index` mostra o número de requisições; `--no-batch` força REST; repos inexistentes fora do catálogo contam como "ignorados"
- [x] Lista `aorumbayev/awesome-omarchy` nas sementes: catálogo instalável de 15 → 25 apps

## Fase 13 — Daemon sob demanda (ver `docs/ideas/daemon-sob-demanda.md`)

- [x] Encerrar ocioso (sem conexões e sem jobs) quando ativado por socket
- [x] `omastore update --check` e notificação desktop via D-Bus
- [x] Timer systemd diário (`omastore-index.timer`)
- [x] Fim de um job conta como atividade (sem isso, o daemon se achava ocioso "há horas" logo após um índice longo)
- [x] Notificação só quando o conjunto repo@versão muda; falha ao notificar não grava o estado (tenta de novo)
- [x] Testado: daemon com `-idle-timeout 1s` encerra ~1 s depois do último cliente e remove o socket; D-Bus sem servidor de notificação dá erro claro

## Fase 14 — Distribuição (ver `docs/ideas/distribuicao.md`)

- [x] Workflow de release por tag (`.github/workflows/release.yml`, validado com actionlint): testes, tarball, attestation SLSA, PKGBUILD `-bin` e upload
- [x] `PKGBUILD` `omastore-bin` gerado de `packaging/arch-bin/PKGBUILD.in` com o sha256 real (`make pkgbuild-bin`); testado com `makepkg`
- [x] `make dist`: tarball reprodutível (mesmos bytes em dois builds; `--sort=name`, dono 0, `SOURCE_DATE_EPOCH`, `gzip -n`, `-trimpath`, binários sem símbolos: 21 → 14 MB)
- [x] Tag validada antes de virar nome de arquivo (entra por variável de ambiente, nunca interpolada)

## Fase 15 — Admissão por manifesto e proveniência

- [ ] Exigir `omastore.toml` válido para aparecer em **Descobrir**, com `asset` e `exec` declarados para uma arquitetura suportada; o asset deve existir na última release estável. Campos de apresentação continuam opcionais.
- [ ] Concentrar a decisão de elegibilidade do app e do asset numa regra compartilhada pela indexação e pela instalação; o banco guarda o resultado para consulta, mas instalar/atualizar deve conferir a regra novamente com os dados atuais. Testar que catálogo, CLI e daemon tomam a mesma decisão.
- [ ] Exigir que o asset tenha sido compilado e publicado por um workflow GitHub Actions do próprio repositório, com atestação de proveniência verificável vinculada ao digest, repositório e commit/tag da release.
- [ ] Na instalação e atualização, verificar o digest dos bytes baixados e a proveniência do asset selecionado; recusar asset ausente, divergente ou sem atestação válida. Não aceitar URL arbitrária declarada no manifesto.
- [ ] Revalidar os assets da release ao reindexar mesmo quando a tag não mudou: um arquivo pode ser substituído sob a mesma tag, e o cache atual compara apenas a tag, o HEAD e `pushed_at`.
- [ ] Manter apps já instalados visíveis em **Instalados** e permitir sua desinstalação mesmo que deixem de atender à nova regra; bloquear atualizações sem proveniência válida.
- [ ] Gerar, a partir dos dados do repositório, release e arquivos, um diagnóstico e um prompt copiável para adaptar o projeto: manifesto, workflow de build/release, atestação e comandos de validação. Não inventar caminhos de executável ou etapas de build que não possam ser confirmados.
- [ ] Atualizar `omastore lint-manifest`, o guia de autores e o índice em cache para a nova política; testar admissão, rejeição, instalação e o caso de apps legados já instalados.

## Correções de manutenção — triagem do código fonte

- [ ] **Alta — desinstalação confiável:** `install.removeRegistered` só registra falhas no log, mas `Uninstall` apaga o registro do banco mesmo quando um arquivo não foi removido (`backend/internal/install/install.go`). Diferenciar arquivo ausente, arquivo alterado por terceiros e falha de I/O; preservar no banco os caminhos pendentes para permitir nova tentativa. Na atualização, tratar também falhas ao limpar arquivos da versão anterior.
- [ ] **Média — respostas antigas na interface:** `Backend::reloadDetail` e `loadSimilar` descartam respostas de outro repositório, mas aceitam respostas antigas para o mesmo repositório (`frontend/src/backend.cpp`). Usar um identificador de geração por pedido e testar respostas fora de ordem, como `CatalogModel` já faz.
- [ ] **Média — nome do estado de checksum:** `AssetInfo.verified` significa apenas que há um digest ou arquivo de checksum disponível antes do download (`backend/internal/rpc/methods.go`, `frontend/qml/DetailPage.qml`). Renomear o campo e o texto exibido para não sugerir verificação já concluída; manter proveniência como estado distinto na Fase 15 e atualizar `docs/ipc.md`.
- [ ] **Média — identidade de repositório:** `github.SplitFullName`, `rpc.repoParams.validate` e `install.splitName` aceitam conjuntos diferentes de nomes `owner/repo`. Usar uma validação única antes de chamadas à API e de operações em caminhos locais; cobrir nomes inválidos e válidos em testes compartilhados.
- [ ] **Média — fronteira de formatos:** `install` importa `index` apenas para constantes, classificação de arquitetura e preferência de formatos (`backend/internal/install/{install,extract}.go`). Colocar esses conceitos de asset em um módulo neutro usado por ambos, sem fazer o instalador depender do pipeline de indexação.
- [ ] **Média — entrada única dos casos de uso:** a CLI chama `Indexer` e `Installer` diretamente e altera opções do indexador, enquanto o daemon usa métodos de `app.App` (`backend/cmd/omastore/main.go`, `backend/internal/app/backend.go`). Passar opções por chamada e encaminhar as duas interfaces pelos mesmos métodos de aplicação, especialmente antes de adicionar a política da Fase 15.
- [ ] **Baixa — primeira indexação:** `Backend::maybeIndexOnFirstRun` marca a consulta como feita antes da resposta (`frontend/src/backend.cpp`). Permitir nova tentativa após erro transitório para não deixar uma instalação nova com catálogo vazio até o usuário agir.
- [ ] **Baixa — erros nos testes do indexador:** substituir as chamadas `stats, _ = ix.Run(...)` em `backend/internal/index/index_test.go` por verificações explícitas do erro; uma falha de indexação não deve aparecer apenas como estatística inesperada.

## Fase 15 — Só apps com manifesto + skills para autores

- [x] `omastore.toml` obrigatório: sem ele o repo não entra (e sai, se estava); arquivo vazio vale como opt-in
- [x] Campo `kind` (`app` padrão); `plugin`/`theme`/outros não são indexados; `lint-manifest` avisa
- [x] Manifesto vem no lote GraphQL (`object(expression: "HEAD:omastore.toml")`), sem requisição extra; REST distingue arquivo vazio de ausente
- [x] Descoberta pela busca de código `filename:omastore.toml` (raiz), independente de topic
- [x] Bug: `index.Version` não forçava reprocessamento quando o nome descoberto diferia do gravado (capitalização/renomeação) — o forçar é recalculado após resolver o nome; teste de regressão
- [x] Catálogo real: 0 apps até os autores adotarem o manifesto (mensagem explicativa na interface)
- [x] Skills em `skills/`: `omastore-manifest`, `omastore-release`, `omastore-check`

## Ideias futuras

Propostas detalhadas, com evidências e custo, ficam em [`docs/ideas/`](docs/ideas/README.md).


- [ ] Verificação de assinatura (minisign/cosign) além de checksum
- [ ] Atualização automática em segundo plano (a notificação de atualizações já existe: Fase 13; falta instalar sozinho, opt-in)
- [ ] Suporte a Flatpak/AppImage com integração de sandbox
- [ ] Avaliações/sinalização de apps problemáticos
