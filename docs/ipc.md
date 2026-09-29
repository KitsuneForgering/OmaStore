# Protocolo IPC do OmaStore

O frontend conversa com o `omastored` por **JSON-RPC 2.0** num socket Unix em
`$XDG_RUNTIME_DIR/omastore.sock` (permissão `0600`). Implementação:
`backend/internal/rpc/`. Versão do protocolo: **1** (`daemon.hello`).

## Enquadramento

- Cada mensagem é **um objeto JSON numa linha**, terminado por `\n` (NDJSON).
  Lotes (arrays) não são suportados.
- Tamanho máximo de uma mensagem recebida: 1 MiB. Acima disso o servidor
  responde `-32600` e fecha a conexão.
- Requisições de um mesmo cliente são processadas **em paralelo**; associe as
  respostas pelo `id`, não pela ordem.
- Mensagens do servidor sem `id` são **notificações** e vão para todos os
  clientes conectados.

```json
→ {"jsonrpc":"2.0","id":1,"method":"catalog.get","params":{"repo":"pch/rawmakase"}}
← {"jsonrpc":"2.0","id":1,"result":{"repo":"pch/rawmakase","name":"RAWmakase", ...}}
← {"jsonrpc":"2.0","method":"job.progress","params":{"id":"job-3","stage":"download","done":512,"total":1024, ...}}
```

`params` ausente equivale a `{}`. Campos desconhecidos em `params` são erro
(`-32602`), para pegar erros de digitação no frontend.

## Métodos

| Método | Parâmetros | Resultado |
|---|---|---|
| `daemon.hello` | — | `{name, protocol}` |
| `catalog.list` | `{category?, query?, installed?, all?, limit?, offset?}` | `AppItem[]`: por score; com `query`, por relevância |
| `catalog.get` | `{repo}` | `AppDetail` |
| `catalog.similar` | `{repo, limit?}` (padrão 8, máx. 50) | `AppItem[]` mais parecidos, só instaláveis |
| `catalog.categories` | — | `[{name, count}]` |
| `installs.list` | — | `InstallInfo[]` |
| `index.start` | `{force?, repos?}` | `Job` (tipo `index`) |
| `install.start` | `{repo}` | `Job` (tipo `install`) |
| `update.start` | `{repo}` | `Job` (tipo `update`) |
| `install.uninstall` | `{repo}` | `{}` (síncrono) |
| `jobs.list` | — | `Job[]` (em andamento e os 50 últimos concluídos) |
| `jobs.cancel` | `{job}` | `{}` |
| `image.get` | `{url}` | `{path}`: caminho local da imagem em cache |

`repo` é sempre `"owner/repo"`. `all: true` inclui apps sem binário instalável.

Busca e recomendação (`backend/internal/search`) são locais e
determinísticas: a mesma consulta sobre o mesmo catálogo devolve sempre a
mesma ordem.
- **Busca:** BM25 com pesos por campo (nome > topics/categoria > resumo >
  README). Aceita prefixo ("rawmak") e corrige um erro de digitação
  ("lightrom") quando o termo não existe no catálogo. Ignora acentos,
  maiúsculas, plurais e -ing/-ed, e traduz termos comuns em português
  ("editor de fotos", "voz para texto") para o inglês das descrições.
- **Parecidos:** similaridade de cosseno entre vetores TF-IDF de topics,
  nome, resumo e categoria, com o README em peso baixo.
`image.get` só aceita `https://` e valida o conteúdo como imagem; o frontend
nunca acessa a rede diretamente.

### Tipos

```ts
AppItem {
  repo, name, summary, iconUrl, category: string
  screenshots: string[]            // nunca null
  stars: number, score: number, installable: boolean
  latestVersion: string            // tag da última release
  installedVersion: string         // "" se não instalado
  updateAvailable: boolean
}
AppDetail extends AppItem {
  readme: string                   // markdown com URLs já absolutas
  description, license, htmlUrl: string
  topics: string[]
  pushedAt?, indexedAt?: string    // RFC 3339
  assets: {name, size, arch, format, verified}[]
  install: InstallInfo | null
}
InstallInfo { repo, version, installedAt, execPath, desktopPath }
Job {
  id, kind: "index" | "install" | "update", repo?: string
  state: "running" | "done" | "failed" | "canceled"
  stage?: string                   // install: download, verify, extract, integrate, done
  done, total: number              // bytes (install) ou repos (index)
  message?: string                 // index: repo atual
  error?: {code, message}
  result?: InstallInfo | IndexResult
  started, finished?: string
}
IndexResult { updated, refreshed, unchanged, removed, skipped, notApps, failed }
```

## Jobs e notificações

Operações longas retornam um `Job` imediatamente e seguem por notificações:

| Notificação | Quando |
|---|---|
| `job.started` | job criado |
| `job.progress` | mudança de etapa ou avanço (no máximo ~10/s por job) |
| `job.done` | concluído com sucesso (`result` preenchido) |
| `job.failed` | falhou ou foi cancelado (`state` diz qual; `error` preenchido) |
| `catalog.changed` | depois do fim de um índice ou de uma instalação/atualização/remoção bem-sucedida; params `{}` ou `{repo}` |

Concorrência: **um índice por vez** e **uma operação por app** (instalar,
atualizar e desinstalar o mesmo repo são mutuamente exclusivos); conflitos
retornam `-32002`. Apps diferentes instalam em paralelo.

`catalog.changed` sempre chega **depois** do `job.done`/`job.failed`
correspondente, então ao recebê-lo o frontend pode recarregar a lista sabendo
que o job já terminou. Um job cancelado mantém em `result` o que foi feito
até o cancelamento (ex.: repos já indexados).

## Códigos de erro

| Código | Significado |
|---|---|
| -32700 | JSON inválido |
| -32600 | requisição inválida (sem `jsonrpc: "2.0"`/`method`, mensagem grande demais) |
| -32601 | método desconhecido |
| -32602 | parâmetros inválidos |
| -32603 | erro interno |
| -32001 | não encontrado (app, job) |
| -32002 | ocupado: já existe operação conflitante |
| -32003 | conflito: arquivo existente não pertence ao OmaStore |
| -32004 | app sem binário para esta arquitetura |
| -32005 | rate limit do GitHub esgotado |
| -32006 | app não está instalado |
| -32007 | app já está na versão mais recente |
| -32008 | checksum não confere |
| -32009 | cancelado |

## Execução

- Manual: `omastored` (ou `make run-daemon`). Recusa iniciar se outro daemon
  já atende o socket; remove sockets órfãos.
- systemd (usuário): `packaging/systemd/omastored.{socket,service}` com
  socket activation. `systemctl --user enable --now omastored.socket`.
- Ociosidade: `-idle-timeout D` encerra o daemon depois de `D` sem clientes
  conectados nem jobs em andamento (o fim de um job conta como atividade).
  Padrão: 10 min quando iniciado por socket activation (o systemd o reinicia
  na próxima conexão), desligado quando iniciado à mão.
- `SIGTERM`/`SIGINT`: cancela os jobs, espera terminarem (com rollback das
  instalações interrompidas), fecha o banco e remove o socket.
