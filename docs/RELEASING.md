# Releasing pakka

Version-controlled release reference for the pakka plugin. The interactive
`/release` checklist lives in the monorepo root `.claude/commands/release.md`
(outside this repo); this file is the committed source of truth for the
**supply-chain gate** that must pass before a release is finalized.

## How a release is built

1. On a clean tree, `make release` cross-builds the five binaries with
   `-trimpath` + `CGO_ENABLED=0` into `bin/`, writes `bin/SHA256SUMS`, and prints
   each binary's `go version -m` provenance (`vcs.modified=false`,
   `vcs.revision=<HEAD>`). Commit `bin/*` + `bin/SHA256SUMS`.
2. Bump the version, tag `vX.Y.Z`, and push the tag.
3. The tag push triggers `.github/workflows/release.yml`, which rebuilds the
   assets fresh at the tag into a gitignored `dist/`, generates a CycloneDX SBOM,
   attests SLSA build provenance, gates on artifact presence + checksums, and
   uploads the binaries + `SHA256SUMS` + `sbom.cdx.json` as release assets.

The committed in-tree `bin/` binaries and the CI release assets are built the
same way but embed different `vcs.revision` (in-tree = build commit; assets =
tag commit). This is expected — see `SECURITY.md` "Verifying Releases".

## Supply chain (required before `gh release create`)

Do not finalize a release until every box is checked. If any fails, stop.

- [ ] **SHA256SUMS validates** — download the assets and run
      `sha256sum -c SHA256SUMS` (or `shasum -a 256 -c SHA256SUMS` on macOS);
      every line reports `OK`.
- [ ] **Binaries `vcs.modified=false`** —
      `go version -m pakka-core-darwin-arm64 | grep vcs` shows
      `vcs.modified=false` and `vcs.revision` = the tag commit.
- [ ] **Provenance attestation attached** —
      `gh attestation verify pakka-core-darwin-arm64 -R amargautam/pakka`
      passes (also for `SHA256SUMS` and `sbom.cdx.json`).
- [ ] **SBOM attached** — `sbom.cdx.json` is present in the release assets and is
      valid CycloneDX JSON.
- [ ] **Assets built by the release workflow, never by hand** — the assets came
      from the `release` Actions run for this tag, not a manual local upload.

## References

- `SECURITY.md` → "Verifying Releases" — full verification + reproduction steps.
- `.github/workflows/release.yml` — the release workflow.
- `Makefile` → `release` target — local reproducible build.
