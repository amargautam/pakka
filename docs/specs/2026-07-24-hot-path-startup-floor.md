# Hot-path startup floor + statusline walk cache (v0.17 perf)
Date: 2026-07-24
Status: draft

## Problem
Every PreToolUse hook execution pays a ~10ms process-startup floor (bench-latency 2026-07-24: floor p50 10.02ms / p95 11.21ms), sinking two published budgets before any work runs: guard p95 12.9ms vs 10ms budget, commit-gate non-commit passthrough p95 11.4ms vs 5ms budget — disclosed since v0.12.0. Root suspect: the fat pakka-core binary links recall's modernc.org/sqlite, whose package init dominates startup; hot-path subcommands (guard, commit-gate passthrough, statusline) never touch SQLite. Separately, issue #36: the statusline transcript walk re-resolves cwd/repo per project dir every render (file open + git rev-parse), unbounded by the existing mtime cache.

## User stories
- As a pakka user, I want hook overhead inside published budgets so that every Bash call, Read, and render stays imperceptible.
- As a maintainer, I want the latency disclosure in benchmarks/ to show PASS rows so that the known-issue is closed with evidence.

## Module decisions
- Measure first: instrument what dominates the 10ms floor (expected: sqlite package init). If confirmed → split delivery binary: new slim `pakka-hot` (guard, commit-gate, statusline — no recall/sqlite import) + existing pakka-core keeps everything (recall, bench, report, spec-generate, review-pass). bin/run dispatches hot subcommands to pakka-hot, falls back to pakka-core when absent (compat).
- If floor is NOT sqlite init: apply the actual dominant fix found; spec constraint is the budget outcome, not the mechanism. Document finding either way in benchmarks/.
- Makefile release/cross build both binaries; SHA256SUMS + CI attestation cover pakka-hot artifacts.
- #36: per-dir cwd/repo resolution cache keyed by dir mtime, mirroring meterCache pattern; output byte-identical warm vs cold.
- No behavior change to any hook decision logic — perf only.

## Acceptance criteria
1. bench-latency after change: guard p95 < 10ms AND commit-gate non-commit passthrough p95 < 5ms on the same machine/method as the 2026-07-24 baseline; report regenerated into benchmarks/ showing PASS.
2. Startup-floor cause documented in benchmarks/ report (measured, named — e.g. sqlite init X ms) with before/after floor numbers.
3. If split: pakka-hot contains no sqlite symbol (verified: `go version -m` deps or `nm`-style check in test/CI); bin/run routes guard/commit-gate/statusline to pakka-hot when present, pakka-core otherwise — behavioral test covers both routes.
4. All existing gate/guard/statusline behavioral tests pass unchanged against the hot binary path.
5. #36: N renders over unchanged project tree perform per-dir resolution once (probe/exec counter test); dir mtime bump invalidates only that dir; warm output byte-identical to cold.
6. make release builds + checksums all shipped binaries including pakka-hot variants; workflow artifact check updated.
7. go test ./... exit 0; make test-js exit 0.

## Out of scope
- Full three-plugin split (#17 stays deferred).
- Statusline semantics/attribution changes (#11/#34).
- Any recall feature work.

## Open questions
