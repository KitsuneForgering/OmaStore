# Trust: attestations, reputation and sandbox

## Evidence

- The GitHub API already returns a `digest` (sha256) per asset, and the installer
  checks it. But the digest only proves the download was not corrupted: it does not say
  **who** built the binary.
- Some apps publish signatures (`checksums.txt.sig` in RAWmakase), which
  we currently just discard.
- Installing software from any repository with the `omarchy` topic is, in
  practice, trusting anyone who sets that topic.

## Proposal

1. **GitHub artifact attestations:** releases produced by GitHub Actions with
   `actions/attest-build-provenance` have SLSA provenance verifiable through the
   API (`GET /repos/{owner}/{repo}/attestations/{digest}`). Show
   "built by workflow X of the repository itself" when available, and
   allow requiring it in a setting ("only apps with provenance").
2. **minisign/cosign signatures:** verify when the repository publishes
   the public key (or in the manifest, see [manifesto.md](manifesto.md)).
3. **Reputation signals in the catalog:** repository age, number of
   releases, whether the release is newer than the last commit, and whether the asset was
   uploaded by a CI bot (`uploader.type`). Show them, without blocking.
4. **Explicit warning without checksum:** today we only log a warning.
   The frontend should ask for confirmation.
5. **Optional sandbox:** generate the `.desktop` `Exec` through `bwrap` with a
   restricted profile (without the whole `$HOME`), for those who want it. It cannot be the default,
   because many apps need `$HOME`.

## Cost

Attestations and the no-checksum warning are small; minisign is medium (simple
format, there is a Go implementation); the sandbox is large (per-app profiles).

## Risks

- Requiring provenance by default would remove most current apps from the catalog.
- The sandbox breaks apps in ways that are hard to diagnose; it needs to be opt-in
  per app.
