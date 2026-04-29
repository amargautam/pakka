# Changelog

All notable changes to pakka. Format follows [Keep a Changelog](https://keepachangelog.com).

## [Unreleased]

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
