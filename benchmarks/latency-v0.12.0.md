# Hook hot-path latency — v0.12.0

End-to-end wall-clock per hook invocation (process spawn + stdin parse + work + stdout), the real cost a Claude Code session pays each time the hook fires.

## Results

| subcommand | input class | p50 | p95 | budget | pass/fail |
|---|---|---|---|---|---|
| status-line | native (CC 2.1 payload) | 18.05ms | 19.43ms | <50ms | ✅ pass |
| status-line | legacy fallback (large transcript, cold cache) | 30.81ms | 33.06ms | <50ms | ✅ pass |
| guard | benign Read | 8.65ms | 9.65ms | <10ms | ✅ pass |
| guard | benign Bash | 7.96ms | 8.89ms | <10ms | ✅ pass |
| commit-gate | non-commit Bash (ls -la) | 8.15ms | 9.22ms | <5ms | ❌ FAIL |
| *(reference)* | *process-startup floor (guard, empty stdin)* | *8.13ms* | *9.71ms* | *—* | *—* |

## Root cause — the startup floor dominates

Every subcommand pays a shared **~8ms process-startup floor** before any command logic runs (row above: guard on empty stdin returns immediately after parse, yet still costs ~8ms). The per-command logic itself is negligible: `commit-gate` on a non-commit `ls -la` measures the same as the floor. So statusline (transcript work) fits its 50ms budget easily, guard clears 10ms with a thin margin, but **commit-gate misses its <5ms budget: 9.2ms p95 measured vs 5ms** — the shared startup floor alone exceeds the budget, so no commit-gate code change can close it. This is a real miss, not rounding.

`GODEBUG=inittrace=1` attributes the floor:

- A tiny no-op Go binary starts in ~2.5ms (p50) on this machine — the irreducible fork+exec+runtime cost.
- `pakka-core` adds ~3ms of package `init()` on top, of which **~3ms is a single package**: `modernc.org/libc/.../netdb` (3.66 MB, ~44k allocs) building network service/protocol tables at init. It is pulled in transitively by `internal/recall`'s pure-Go SQLite (`modernc.org/sqlite`), which Go links into the one binary that serves *all* subcommands. Recall (v0.3.0) therefore raised every hook's floor by ~3ms, including hooks that never touch SQLite.

**Fix (out of scope for v0.12.0):** keep the recall/SQLite dependency out of the hook hot-path binary — i.e. split `pakka-core` so hooks (`status-line`/`guard`/`commit-gate`) link a lean binary and `recall` lives in its own. That is the binary split tracked as #17, explicitly deferred out of this consolidation pass. A `modernc.org/libc` version bump is *not* a safe substitute: the netdb table build is intrinsic to that library and bumping risks the recall path. Recorded here; the split closes the commit-gate budget.

## Measurement discipline

Numbers must respond to input or the harness is broken. The statusline native path (CC 2.1 `context_window` payload) skips the transcript scan; the legacy path re-sums a ~4000-line synthetic transcript on every render (cold cache each run — the honest steady-state cost, since the active transcript grows and cache-misses every turn).

- legacy p50 = **30.81ms**, native p50 = **18.05ms**, Δ = **+12.77ms** (≥1ms required).
- Identical numbers across these two inputs ⇒ broken harness; the script exits non-zero and refuses to write this report.

## Method

- Runs: 50 timed per class (8 warmup discarded), nearest-rank p50/p95.
- Timer: `time.perf_counter()` around each `subprocess.run` (includes process spawn — the real hook cost).
- Machine: Apple M3 Max (Mac15,10), darwin/arm64.
- Binary: `go build ./cmd/pakka-core` at commit `04ca6ef2f7bb`.
- Repo under test: `/Users/amar/Projects/pakka.dev/pakka`.
- statusline fallback fed a synthetic `$HOME` with a large transcript so the scan path does real work; guard and commit-gate use the live repo.

## Reproduce

```
make bench-latency
# or:
python3 benchmarks/latency_bench.py --runs 50
```
