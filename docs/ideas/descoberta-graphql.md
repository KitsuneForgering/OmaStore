# Indexação em lote com GraphQL e novas fontes de descoberta

> **Status:** implementado na Fase 12 (`internal/github/graphql.go`). Resultado
> medido com ~230 repositórios reais (topic + `awesome-omarchy`):
>
> | | REST | GraphQL em lote |
> |---|---|---|
> | 1ª indexação | 863 requisições, 76 s | 173 requisições, 45 s |
> | reindexação | 497 requisições, 44 s | 12 requisições, 15 s |
>
> O tempo restante da reindexação é quase todo do próprio GraphQL (~6 s por
> consulta de 50 repositórios com assets); por isso lotes de 25 com 3 em
> paralelo.

## Evidência

- A indexação real de 34 repositórios levou ~15 s na primeira passada e ~8 s
  quando nada mudou. O custo vem de **4–6 requisições REST por repositório**:
  repo, HEAD, última release, README, árvore.
- A busca por `topic:omarchy` retorna muitos repositórios que não são apps
  (temas, dotfiles, listas). Eles custam requisições e só são descartados
  depois, por não terem binário.
- A própria busca trouxe `aorumbayev/awesome-omarchy`: uma lista curada que
  poderia alimentar as sementes.

## Proposta

1. **GraphQL para a checagem de cache.** Uma única consulta traz, para até
   ~50 repositórios, os campos que a verificação de cache compara:

   ```graphql
   query($ids: [ID!]!) {
     nodes(ids: $ids) {
       ... on Repository {
         nameWithOwner stargazerCount pushedAt isArchived description
         repositoryTopics(first: 20) { nodes { topic { name } } }
         defaultBranchRef { target { oid } }
         latestRelease { tagName releaseAssets(first: 50) { nodes { name size downloadUrl digest } } }
       }
     }
   }
   ```

   Os repositórios que **não mudaram** (a maioria) saem da conta com ~1/50 de
   requisição cada. README e árvore continuam via REST, só para os que mudaram.
   Isso exige token (GraphQL não funciona anônimo), então o REST atual fica
   como caminho sem token.

2. **Pré-filtro barato:** repositórios sem `latestRelease` nunca geram
   requisições de README/árvore. Hoje eles já são marcados não instaláveis,
   mas depois de buscar tudo.

3. **Sementes de listas curadas:** ler links `github.com/owner/repo` de
   READMEs como o `awesome-omarchy` (uma requisição) e somar às sementes.

## Custo

Médio: cliente GraphQL (`githubv4` ou requisição HTTP simples), mapear para os
tipos atuais, manter o REST como fallback, testes com fixtures.

## Riscos

- O custo de GraphQL é medido em "pontos", não em requisições; consultas com
  muitos assets custam mais. Medir antes de adotar `first: 50`.
- Listas "awesome" incluem temas e plugins: o filtro por release com binário
  continua necessário.
