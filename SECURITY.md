# Security Policy

## Reporting

Email **hello@amargautam.com**. Do not open public issues for vulnerabilities.

Include: pakka version (`pakka --version`), Claude Code version, OS, reproducer, expected vs observed behavior. Acknowledgement within 7 days.

## Scope

In scope:
- pakka plugin runtime (`pakka-core`, `pakka-compress`, `pakka-commitgate`)
- Hooks installed by pakka (PreToolUse, PostToolUse, Stop, SessionStart, status-line)
- Plugin CLI surface and install/init flow
- Audit log integrity (`~/.claude/pakka/audit.jsonl`)

Out of scope:
- Claude Code itself (report to Anthropic)
- Anthropic API and `claude` CLI auth
- User's own scripts invoked by hooks
- Stack tools (`go vet`, `eslint`, etc.) that pakka spawns

## Threat Model

pakka runs locally with full filesystem access in the user's session. It intercepts tool calls before they reach the harness, gates commits, and writes an append-only audit log.

Threats considered:

| Threat | Surface | Mitigation |
|---|---|---|
| Prompt injection in compressed file content | `pakka-compress` reads user files, sends to LLM subprocess | Subprocess sandboxed (see below); content wrapped in `<user-content>` delimiters |
| Malicious `stack.json` (command injection) | Stack overlay defines lint/test commands | Tokenized via `strings.Fields` + `exec.Command`; shell metachars rejected; no `sh -c` |
| Audit log tampering | Local file, no signing | Out of scope for v0.1.0; full SHA-256 input hashes added so post-hoc detection is feasible |
| Path traversal via crafted file paths | Hook input, context-file resolution | 2+ hop `../` regex guard; context-file fallback gated by trust boundary (home dir or `.git`/`.pakka` sentinel) |
| Supply chain | Go modules, `claude` CLI on PATH | Stdlib-first policy; deps vendored; no telemetry, no network calls outside the documented `claude` subprocess and optional `ANTHROPIC_API_KEY` HTTP fallback |

## Hardening Landed in v0.1.0-dev (Pass 4.8 review-fix)

| Area | Change |
|---|---|
| Stack-config command exec | `stack.json` lint/test commands tokenized via `strings.Fields` + `exec.Command`. Shell metacharacters rejected. No `sh -c` path. |
| Semantic-compression sandbox | `claude` subprocess invoked with `--permission-mode default` and empty `--allowedTools`. Compressed-file content cannot trigger tool calls. |
| Prompt template hardening | File content wrapped in `<user-content>` delimiters with explicit system instruction to treat the block as data, not instructions. |
| Audit hash full-width | `InputHash` now full SHA-256. Was previously truncated to 64 bits — collision-feasible. |
| Path traversal guard | Regex catches 2+ hop `../` sequences. Context-file fallback only activates inside the user's home directory or a directory containing a `.git`/`.pakka` sentinel. |
| File modes | `debug.log` created `0600` (was `0644`). |
| Calibrate script | Compressed content piped via stdin instead of unquoted shell interpolation. |
| Telemetry / network | Builders confirmed no new write paths, no telemetry, no outbound network calls beyond the documented `claude` subprocess and optional `ANTHROPIC_API_KEY` HTTP fallback. |

## Out-of-Band Keep-List

For v0.1.0-dev, the review-fix pass introduced **no new** telemetry, no new outbound network calls, and no new write paths outside `~/.claude/pakka/`. This is a guarantee of the pass, not a forward commitment — future passes that change it must be called out in `CHANGELOG.md` under `### Security`.

## Verifying Releases

Shipped binaries in `bin/` are built reproducibly. `make release` refuses to run on a dirty working tree, builds with `-trimpath` and `CGO_ENABLED=0`, writes `SHA256SUMS` into the release dir (`bin/` by default; `RELEASE_DIR=dist` in CI), and prints each binary's `go version -m` provenance. On every `v*` tag the `release` CI workflow rebuilds the assets fresh at the tag into a gitignored dir, verifies the build mutates no tracked files, and uploads them; it does not byte-compare against the committed in-tree binaries (which embed their own build commit, not the tag).

**Check integrity** — from the `bin/` directory:

```
cd bin && shasum -a 256 -c SHA256SUMS
```

Every line must report `OK`.

**Check provenance** — a shipped binary must be built from a clean, committed tree:

```
go version -m bin/pakka-core-darwin-arm64 | grep vcs
```

Require `vcs.modified=false`. `vcs.revision` is the commit the binary was built at: for the committed in-tree binaries that is the parent of the "regenerate binaries" commit (a binary cannot embed the hash of the commit that carries it); for the CI-uploaded release assets it is the tag commit. `vcs.modified=true` means the binary was built from an uncommitted tree — do not trust it.

**Reproduce from source** — reproduce a binary at the exact revision it reports, using the same Go toolchain (version pinned in `go.mod`):

```
rev=$(go version -m bin/pakka-core-darwin-arm64 | awk '/vcs.revision/{print $2}')
git checkout "$rev"
make release RELEASE_DIR=/tmp/pakka-repro
shasum -a 256 /tmp/pakka-repro/pakka-core-darwin-arm64   # compare against bin/SHA256SUMS
```

Because builds use `-trimpath` and `CGO_ENABLED=0`, the same Go toolchain at that revision yields byte-identical binaries. Reproduce the release assets the same way, checking out the tag (their embedded `vcs.revision`) instead.

**Verify build provenance** — release assets carry a signed SLSA build-provenance attestation (`actions/attest-build-provenance`, keyless via GitHub OIDC). Confirm an asset was built by the release workflow, not by hand:

```
gh attestation verify pakka-core-darwin-arm64 -R amargautam/pakka
```

Run it against any downloaded asset — the five binaries, `SHA256SUMS`, and `sbom.cdx.json` are all attested. A failed verification means the artifact did not come from the tagged release workflow — do not trust it.

**SBOM** — each release ships `sbom.cdx.json`, a [CycloneDX](https://cyclonedx.org/) SBOM generated by `cyclonedx-gomod app` over `cmd/pakka-core`. It lists every module in the build so you can audit the dependency set and cross-check against advisories. It is a release asset and is covered by both the provenance attestation and integrity checks.
