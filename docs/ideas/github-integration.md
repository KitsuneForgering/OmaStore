# GitHub integration: sign-in and sponsors

Starring (`star.get`/`star.set`), release notes and prefilled issue reports
are done (Phases 17 and 18 of `TODO.md`). Build provenance is covered by
[trust.md](trust.md) and Phase 15/16 of `TODO.md`.

## Evidence

- **Sign-in:** everything that needs a token (starring, manifest code
  search, GraphQL batches, 5000 requests/hour) depends on `GITHUB_TOKEN` or
  the `gh` CLI (`github.TokenFromEnv`). A user without `gh` sees disabled
  stars and hits the 60 requests/hour anonymous limit.
- **Sponsors:** GraphQL exposes `Repository.fundingLinks` (`platform`, `url`),
  read from `.github/FUNDING.yml`; the batch query could ask for it at no
  extra request.

## Proposal

1. **Sign in with GitHub (device flow).** The Publish page and the star
   button offer "Sign in": the daemon starts GitHub's device flow, the
   interface shows the user code and opens `github.com/login/device`, and the
   token goes to the Secret Service keyring (libsecret over D-Bus, never a
   plain file). `TokenFromEnv` keeps its order and adds the keyring last.
   Scopes: `public_repo` only if starring is wanted; none for reading.
2. **Sponsor button.** Fetch `fundingLinks` in the batch, store them, and
   show "Sponsor" next to the star when the repository has any. Only `https`
   URLs are shown.

## Cost

Item 2 is small (a column, a field, a link). Item 1 is medium: registering
an OAuth App for OmaStore with device flow enabled, the flow itself (two
endpoints, polling with `interval`), keyring storage and a "Sign out"; the
client id is public, so it can live in the binary.

## Risks

- Device flow means OmaStore holds a token with write scope (for starring).
  Ask for `public_repo` only when the user asks to star, not at sign-in.
