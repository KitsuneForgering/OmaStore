# TODO — OmaStore

Open work, in the order in which the pieces depend on each other. Each item must end with
`make test` passing. Finished items are removed: the history of phases 0–18 is in git
(this file before 2026-10-01).

## Phase 15 — Admission by manifest and provenance

- [ ] Require a valid `omastore.toml` to appear in **Discover**, with `asset` and `exec` declared for a supported architecture; the asset must exist in the latest stable release. Presentation fields stay optional.
- [ ] Concentrate the app and asset eligibility decision in a rule shared by indexing and installation; the database stores the result for queries, but install/update must check the rule again with current data. Test that catalog, CLI and daemon reach the same decision.
- [ ] Require provenance by default (today: verified at index time with Sigstore, shown as a badge, required only with the *Install only apps with build provenance* setting).
- [ ] On install and update, fetch and verify the attestation again with current data instead of trusting the index-time result (the downloaded bytes are already checked against the attested digest). Do not accept an arbitrary URL declared in the manifest.
- [ ] Keep already installed apps visible in **Installed** and allow uninstalling them even if they no longer meet the new rule; block updates without valid provenance.
- [ ] Generate, from the repository, release and file data, a diagnosis and a copyable prompt to adapt the project: manifest, build/release workflow, attestation and validation commands. Do not invent executable paths or build steps that cannot be confirmed.
- [ ] Update `omastore lint-manifest`, the authors guide and the cached index for the new policy; test admission, rejection, installation and the case of legacy apps already installed.

## Catalog growth

The catalog starts nearly empty until authors adopt `omastore.toml`, so first runs and authors come first.

- [ ] Published catalog snapshot (built by a scheduled workflow, attested) so a first run without a GitHub token shows the catalog with ~1 request
- [ ] Open PRs with the suggested manifest in the apps that were installable before the manifest became mandatory; target: 5 apps from 3 authors (2026-09-30: ZacharyZhang-NY/OmaPhoto#12, pch/rawmakase#27, michaelmonetized/omadesign#188)

## Checks on a real Omarchy session

- [ ] `pkexec` from the socket-activated (systemd `--user`) daemon: polkit must find the graphical agent for a process outside the login session (on Omarchy that agent is the shell's own, `/usr/share/omarchy/shell/plugins/polkit`)
- [ ] The notification click opens the GUI, and a TUI app opens and is focused on a second launch

## Smaller features

- [ ] Graphical-session user services: let `[services]` order a unit after `graphical-session.target` (`PartOf`/`WantedBy`) and set `RestartSec`/`RestartPreventExitStatus`, so units like Omakade's `omakade-guide-button` can be managed (btsouth/omakade#86)

- [ ] Read `.PKGINFO` `depend` lines from `.pkg.tar.zst` assets when the repository has no PKGBUILD

## Distribution

- [ ] Publish on the AUR (see `docs/ideas/distribution.md`)

## Future ideas

Detailed proposals, with evidence and cost, live in [`docs/ideas/`](docs/ideas/README.md)
(signatures and the sandbox are in `trust.md`). Not written up yet:

- [ ] Ratings/flagging of problematic apps
