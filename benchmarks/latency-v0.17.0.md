# Hook hot-path latency — v0.17 (hot-path split)

End-to-end wall-clock per hook invocation (process spawn + stdin parse + work + stdout), the real cost a Claude Code session pays each time the hook fires. The three hot-path subcommands (guard, commit-gate, status-line) are measured against the lean `pakka-hot` binary that bin/run routes them to.

## Results

| subcommand | input class | p50 | p95 | budget | pass/fail |
|---|---|---|---|---|---|
| status-line | native (CC 2.1 payload) | 17.05ms | 19.67ms | <50ms | ✅ pass |
| status-line | legacy fallback (large transcript, cold cache) | 31.09ms | 35.59ms | <50ms | ✅ pass |
| guard | benign Read | 4.21ms | 5.00ms | <10ms | ✅ pass |
| guard | benign Bash | 3.83ms | 4.31ms | <10ms | ✅ pass |
| commit-gate | non-commit Bash (ls -la) | 3.70ms | 4.21ms | <5ms | ✅ pass |
| *(reference)* | *pakka-hot startup floor (guard, empty stdin)* | *3.76ms* | *4.53ms* | *—* | *—* |
| *(reference)* | *pakka-core startup floor — pre-split, for comparison* | *9.65ms* | *10.46ms* | *—* | *—* |

## Root cause + fix — the startup floor, halved

Every subcommand pays a shared process-startup floor before any command logic runs. Until v0.17 that floor was ~10ms because the single `pakka-core` binary linked **two heavy dependencies that the hot path never uses**:

1. **`modernc.org/sqlite`** (via `internal/recall`) — its `modernc.org/libc/.../netdb` package `init()` builds network service/protocol tables (3.66 MB, ~44k allocs) at startup, measured at **~4ms** via `GODEBUG=inittrace=1`.
2. **`net/http`** (via `internal/compress/semantic`'s Anthropic client) — linking it adds **~1.8ms** to the floor (its runtime init + the larger binary's page-in).

The fix (spec: docs/specs/2026-07-24-hot-path-startup-floor.md) splits delivery into two binaries. `pakka-hot` links ONLY the three hot-path subcommands — no `internal/recall`, no `internal/compress/semantic`, no `internal/compress/orchestrator` — so neither sqlite nor net/http is linked (guarded by a build-time dependency test in cmd/pakka-hot). `pakka-core` keeps everything (recall, compress, bench, report, …). bin/run routes guard / commit-gate / status-line to `pakka-hot` when present, falling back to `pakka-core` for older caches.

**Before/after floor (this machine, this run):**

- pakka-core floor (before): p50 **9.65ms** / p95 **10.46ms**
- pakka-hot floor (after): p50 **3.76ms** / p95 **4.53ms**
- floor reduction: **5.89ms** p50 (≈ the sqlite netdb init + net/http link cost, removed).

Outcome: **guard** clears its <10ms budget (5.00ms p95) and **commit-gate**'s non-commit passthrough now clears its <5ms budget (4.21ms p95) — both were FAIL at the ~10ms shared floor in the v0.12.0 baseline (guard 12.87ms, commit-gate 11.42ms p95). The known-issue disclosed since v0.12.0 is closed.

## Measurement discipline

Numbers must respond to input or the harness is broken. The statusline native path (CC 2.1 `context_window` payload) skips the transcript scan; the legacy path re-sums a ~4000-line synthetic transcript on every render (cold cache each run — the honest steady-state cost, since the active transcript grows and cache-misses every turn).

- legacy p50 = **31.09ms**, native p50 = **17.05ms**, Δ = **+14.04ms** (≥1ms required).
- Identical numbers across these two inputs ⇒ broken harness; the script exits non-zero and refuses to write this report.

## Method

- Runs: 50 timed per class (8 warmup discarded), nearest-rank p50/p95.
- Timer: `time.perf_counter()` around each `subprocess.run` (includes process spawn — the real hook cost).
- Machine: Apple M3 Max (Mac15,10), darwin/arm64.
- Binaries: `go build ./cmd/pakka-hot` (hot-path classes + floor) and `./cmd/pakka-core` (pre-split floor) at commit `455bd7813d47`.
- Repo under test: `/Users/amar/Projects/pakka.dev/pakka`.
- statusline fallback fed a synthetic `$HOME` with a large transcript so the scan path does real work; guard and commit-gate use the live repo.

## Reproduce

```
make bench-latency
# or:
python3 benchmarks/latency_bench.py --runs 50
```
