# Changelog

All notable changes to pakka. Format follows [Keep a Changelog](https://keepachangelog.com).

## [Unreleased]

### Security
- **Pass 4.8 review-fix — security hardening.** See [SECURITY.md](SECURITY.md) for threat model and full detail.
  - Stack-config command exec: `stack.json` lint/test commands tokenized via `strings.Fields` + `exec.Command`; shell metacharacters rejected; no `sh -c` path.
  - Semantic-compression sandbox: `claude` subprocess runs with `--permission-mode default` and empty `--allowedTools`. Prompt injection in compressed files cannot exec tools.
  - Prompt template hardening: file content wrapped in `<user-content>` delimiters with system instruction to treat block as data.
  - Audit hash full-width: `InputHash` now full SHA-256 (was truncated to 64 bits).
  - Path traversal guard: regex catches 2+ hop `../`; context-file fallback gated by home-dir or `.git`/`.pakka` trust boundary.
  - File modes: `debug.log` now `0600` (was `0644`).
  - Calibrate script: content piped via stdin instead of unquoted shell interpolation.
  - Out-of-band keep-list: no new telemetry, no new network calls, no new write paths introduced by this pass.

### Fixed
- **Pass 4.8 review-fix — correctness and stability.**
  - Truncate-count off-by-one in tool-result compression metering.
  - File-descriptor leak in compress reader when read errored mid-stream.
  - Race in concurrent audit-log writers; serialized append behind mutex.
  - Path-traversal regex previously allowed crafted multi-hop `../` sequences.
  - Status-line denominator divided by uncompressed total when compression had failed; now guards zero-and-error case.
  - Commit-gate trailer dedupe: repeated invocations no longer stack duplicate `Reviewed-by-pakka` / `Co-authored-by` lines.
  - `repo-root` diff fallback when invoked from subdirectory.
  - File mode on `debug.log` (`0644` → `0600`).
  - `pakka-compress` FD-mode handling for non-regular file inputs.
  - Stack-injection vector in `stack.json` exec path.
- **Pass 4.7 — fix PreToolUse stdout contract for trailer injection.** Auto-trailers (`Reviewed-by-pakka`, `Co-authored-by`, `pakka-session`) now actually land on Claude-authored commits. Pre-fix count: 0 trailers across the entire git history. The commit-gate hook was emitting the legacy `{"tool_input":{"command":"..."}}` envelope; the current Claude Code contract requires `{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"command":"..."}}}`. Claude Code silently ignored the unknown shape, so the rewritten command (with trailers) never reached `git`. Test added in `cmd/pakka-core/main_test.go` to prevent regression — asserts envelope shape and varies-with-input.

### Added
- **`claude -p` subprocess as primary semantic-rewrite engine** (Pass 4.6). Zero-config for Claude Code users — pakka reuses existing `claude` auth on `PATH`. `ANTHROPIC_API_KEY` is now an optional HTTP fallback. Resolution order: `claude` CLI → `ANTHROPIC_API_KEY` HTTP → deterministic strict (nil). New setting `pakka.compress.engine` (`claude-cli` | `anthropic-http` | `auto`, default `auto`). See DESIGN.md §5.16.

### Changed
- **Default output compression level flipped from `strict` to `ultra`** (Pass 4.4). pakka's brand thesis is fewer tokens; the default reflects it. `lite` and `strict` remain available; `super-ultra` for power users. Reversible without code change via `pakka.compress.outputLevel` in `settings.json`. See `memory/DECISIONS.md` "Default output level: ultra (decided 2026-04-29)".
- Status line now shows output token savings alongside input. Format: `↓N (X%) / ↑M (Y%) tok saved · K bugs caught`. UTF-8 with ascii fallback.

### Added
- Output token tracking via transcript JSONL parsing. Multipliers per mode (lite=0.11, strict=0.33, ultra=0.67); uncalibrated for v0.1.0, bench replaces in v0.1.1.
- `meter.WriteOutputTokens(sessionID, outputTokens)` for explicit out-token entries.
- Tool-result truncation savings now surface in status-line input savings.

### Fixed
- Status-line `-- out saved` placeholder replaced with measured value.
- Status line now renders absolute saved tokens alongside percent for both input and output: `↑12.4K (43%) / ↓7.1K (33%) tok saved`. Percent-only output hid scale (50% of 200 vs 50% of 200K). Counts humanize via floor truncation — <1000 raw, K/M with one decimal.
