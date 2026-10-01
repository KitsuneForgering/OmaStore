# Trust: attestations, reputation and sandbox

## Evidence

- The GitHub API already returns a `digest` (sha256) per asset, and the installer
  checks it. But the digest only proves the download was not corrupted: it does not say
  **who** built the binary.
- Some apps publish signatures (`checksums.txt.sig` in RAWmakase), which
  we currently just discard.
- Since Phase 15b a repository gets in only with an `omastore.toml`, but that
  file is an opt-in, not a check: installing an app is, in practice, trusting
  anyone who commits that file and publishes a release.

## Proposal

1. **GitHub artifact attestations:** releases produced by GitHub Actions with
   `actions/attest-build-provenance` have SLSA provenance verifiable through the
   API (`GET /repos/{owner}/{repo}/attestations/{digest}`). Show
   "built by workflow X of the repository itself" when available, and
   allow requiring it in a setting ("only apps with provenance").
   *Accepted:* Phase 15 and 16 of `TODO.md` (badge first, required later).
2. **minisign/cosign signatures:** verify when the repository publishes
   the public key (or in `omastore.toml`, see [`../authors.md`](../authors.md)).
3. **Reputation signals in the catalog:** repository age, number of
   releases, whether the release is newer than the last commit, and whether the asset was
   uploaded by a CI bot (`uploader.type`). Show them, without blocking.
4. **Optional sandbox:** generate the `.desktop` `Exec` through `bwrap` with a
   restricted profile (without the whole `$HOME`), for those who want it. It cannot be the default,
   because many apps need `$HOME`.

## Cost

Attestations are small; minisign is medium (simple
format, there is a Go implementation); the sandbox is large (per-app profiles).

## Risks

- Requiring provenance by default would remove most current apps from the catalog.
- The sandbox breaks apps in ways that are hard to diagnose; it needs to be opt-in
  per app.
