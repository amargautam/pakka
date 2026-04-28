# Changelog

All notable changes to pakka. Format follows [Keep a Changelog](https://keepachangelog.com).

## [Unreleased]

### Changed
- Status line now shows output token savings alongside input. Format: `↓N (X%) / ↑M (Y%) tok saved · K bugs caught`. UTF-8 with ascii fallback.

### Added
- Output token tracking via transcript JSONL parsing. Multipliers per mode (lite=0.11, strict=0.33, ultra=0.67); uncalibrated for v0.1.0, bench replaces in v0.1.1.
- `meter.WriteOutputTokens(sessionID, outputTokens)` for explicit out-token entries.
- Tool-result truncation savings now surface in status-line input savings.

### Fixed
- Status-line `-- out saved` placeholder replaced with measured value.
- Status line now renders absolute saved tokens alongside percent for both input and output: `↑12.4K (43%) / ↓7.1K (33%) tok saved`. Percent-only output hid scale (50% of 200 vs 50% of 200K). Counts humanize via floor truncation — <1000 raw, K/M with one decimal.
