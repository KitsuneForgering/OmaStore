# Batch indexing with GraphQL and new discovery sources

> **Status:** implemented in Phase 12 (`internal/github/graphql.go`). Result
> measured with ~230 real repositories (topic + `awesome-omarchy`):
>
> | | REST | Batched GraphQL |
> |---|---|---|
> | 1st indexing | 863 requests, 76 s | 173 requests, 45 s |
> | reindexing | 497 requests, 44 s | 12 requests, 15 s |
>
> The remaining reindexing time is almost all GraphQL itself (~6 s per
> query of 50 repositories with assets); hence batches of 25 with 3 in
> parallel.

## Evidence

- Real indexing of 34 repositories took ~15 s on the first pass and ~8 s
  when nothing changed. The cost comes from **4–6 REST requests per repository**:
  repo, HEAD, latest release, README, tree.
- The `topic:omarchy` search returns many repositories that are not apps
  (themes, dotfiles, lists). They cost requests and are only discarded
  later, for having no binary.
- The search itself brought up `aorumbayev/awesome-omarchy`: a curated list that
  could feed the seeds.

## Proposal

1. **GraphQL for the cache check.** A single query brings, for up to
   ~50 repositories, the fields the cache check compares:

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

   Repositories that **did not change** (most of them) cost ~1/50 of a
   request each. README and tree still go through REST, only for the ones that changed.
   This requires a token (GraphQL does not work anonymously), so the current REST stays
   as the tokenless path.

2. **Cheap pre-filter:** repositories without `latestRelease` never generate
   README/tree requests. Today they are already marked not installable,
   but only after fetching everything.

3. **Seeds from curated lists:** read `github.com/owner/repo` links from
   READMEs such as `awesome-omarchy` (one request) and add them to the seeds.

## Cost

Medium: GraphQL client (`githubv4` or a plain HTTP request), mapping to the
current types, keeping REST as a fallback, tests with fixtures.

## Risks

- GraphQL cost is measured in "points", not requests; queries with
  many assets cost more. Measure before adopting `first: 50`.
- "awesome" lists include themes and plugins: the filter by release with a binary
  is still needed.
