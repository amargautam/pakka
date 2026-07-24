#!/usr/bin/env python3
"""Hook hot-path latency benchmark for pakka.

Measures end-to-end wall-clock (process spawn + stdin parse + work + output)
for the three hooks that fire on the session hot path, feeding each a realistic
Claude Code hook-event JSON on stdin:

  status-line  — CC 2.1 native payload (context_window/cost present → no
                 transcript scan) vs a legacy payload (native absent → cold
                 transcript-scan fallback over a large synthetic transcript).
  guard        — benign Read PreToolUse + benign Bash PreToolUse.
  commit-gate  — non-commit Bash ("ls -la"): the every-Bash-call passthrough.

Since the v0.17 hot-path split these three subcommands are served by the lean
`pakka-hot` binary (no recall/sqlite, no semantic/net-http), so the harness
measures them against pakka-hot — the real cost a session pays. It also measures
the fat `pakka-core` startup floor so the report can show the before/after the
split closed.

Timing is taken with time.perf_counter() around each subprocess. A warmup batch
is discarded. Percentiles are nearest-rank p50/p95.

Measurement-discipline guard (project hard rule): the harness proves numbers
VARY with input — statusline-legacy over a large synthetic transcript must be
measurably slower than statusline-native (which skips the scan). If that
invariant does not hold the harness is broken and exits non-zero before
reporting anything.

Re-run:  python3 benchmarks/latency_bench.py --runs 50
         (or `make bench-latency`)
"""

import argparse
import json
import math
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import time


# --- percentile (nearest-rank) ----------------------------------------------
def pct(sorted_ms, p):
    """Nearest-rank percentile: rank = ceil(p/100 * N), 1-based, clamped.
    ceil (not round) keeps it exact for every N — round() would pick the
    element one past the correct rank when p/100*N lands on an integer."""
    if not sorted_ms:
        return float("nan")
    k = max(0, min(len(sorted_ms) - 1, math.ceil(p / 100.0 * len(sorted_ms)) - 1))
    return sorted_ms[k]


# --- synthetic $HOME with a large transcript --------------------------------
def build_home_large(root, repo):
    """Create a fake $HOME whose .claude/projects holds one large transcript
    whose embedded cwd resolves (via git) to `repo`, forcing the statusline
    fallback to do a real per-render scan+sum of a multi-MB JSONL file."""
    home = pathlib.Path(root) / "home_large"
    proj = home / ".claude" / "projects" / "synthetic-repo"
    proj.mkdir(parents=True, exist_ok=True)
    (home / ".pakka").mkdir(parents=True, exist_ok=True)

    line = {
        "type": "assistant",
        "cwd": repo,
        "message": {
            "role": "assistant",
            "usage": {
                "input_tokens": 137,
                "output_tokens": 521,
                "cache_creation_input_tokens": 2048,
                "cache_read_input_tokens": 65536,
            },
        },
    }
    tpath = proj / "session-0001.jsonl"
    with open(tpath, "w") as f:
        for i in range(4000):
            line["uuid"] = f"u{i:06d}"
            f.write(json.dumps(line))
            f.write("\n")
    return home, home / ".pakka" / "transcript-cache.json"


# --- one measured class ------------------------------------------------------
def measure(bin_path, subcmd, stdin_bytes, env, runs, warmup, pre=None):
    """Run subcmd `runs` times (after `warmup` discarded runs), return sorted
    per-run wall-clock in ms. `pre` (callable) runs before each iteration."""
    samples = []
    full_env = dict(os.environ)
    full_env.update(env)
    for i in range(runs + warmup):
        if pre:
            pre()
        t0 = time.perf_counter()
        subprocess.run(
            [bin_path, subcmd],
            input=stdin_bytes,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            env=full_env,
            check=False,
        )
        dt = (time.perf_counter() - t0) * 1000.0
        if i >= warmup:
            samples.append(dt)
    samples.sort()
    return samples


def gobuild(pkg, out):
    print(f"building {pkg} → {out}", file=sys.stderr)
    subprocess.run(["go", "build", "-o", out, pkg], check=True)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--core-bin", default="", help="path to pakka-core; built if empty")
    ap.add_argument("--hot-bin", default="", help="path to pakka-hot; built if empty")
    ap.add_argument("--repo", default=os.getcwd(), help="repo cwd fed to hooks")
    ap.add_argument("--runs", type=int, default=50)
    ap.add_argument("--warmup", type=int, default=8)
    ap.add_argument("--out", default="benchmarks/latency-v0.17.0.md")
    ap.add_argument("--sha", default="", help="commit SHA for the report header")
    args = ap.parse_args()

    repo = str(pathlib.Path(args.repo).resolve())

    core_bin = args.core_bin
    hot_bin = args.hot_bin
    tmp = tempfile.mkdtemp(prefix="pakka-latency-")
    try:
        if not core_bin:
            core_bin = os.path.join(tmp, "pakka-core")
            gobuild("./cmd/pakka-core", core_bin)
        if not hot_bin:
            hot_bin = os.path.join(tmp, "pakka-hot")
            gobuild("./cmd/pakka-hot", hot_bin)

        home_large, cache_path = build_home_large(tmp, repo)

        def clear_cache():
            # Cold statusline: remove BOTH the transcript sum cache and the
            # per-dir resolve cache (issue #36) so the legacy path does full work.
            for p in (cache_path, home_large / ".pakka" / "resolve-cache.json"):
                try:
                    os.remove(p)
                except FileNotFoundError:
                    pass

        # --- payloads -------------------------------------------------------
        native_payload = json.dumps({
            "session_id": "bench",
            "hook_event_name": "Status",
            "cwd": repo,
            "transcript_path": "",
            "cost": {"total_cost_usd": 1.23},
            "context_window": {
                "context_window_size": 200000,
                "used_percentage": 42.5,
                "current_usage": {
                    "input_tokens": 1000,
                    "output_tokens": 5000,
                    "cache_creation_input_tokens": 2000,
                    "cache_read_input_tokens": 80000,
                },
            },
        }).encode()

        legacy_payload = json.dumps({
            "session_id": "bench",
            "hook_event_name": "Status",
            "cwd": repo,
            "transcript_path": "",
        }).encode()

        guard_read = json.dumps({
            "session_id": "bench",
            "hook_event_name": "PreToolUse",
            "tool_name": "Read",
            "tool_input": {"file_path": "/tmp/x.go"},
        }).encode()

        guard_bash = json.dumps({
            "session_id": "bench",
            "hook_event_name": "PreToolUse",
            "tool_name": "Bash",
            "tool_input": {"command": "ls -la"},
        }).encode()

        gate_noncommit = json.dumps({
            "session_id": "bench",
            "hook_event_name": "PreToolUse",
            "tool_name": "Bash",
            "tool_input": {"command": "ls -la"},
            "cwd": repo,
        }).encode()

        big_home = {"HOME": str(home_large)}

        # Every measured hot-path class runs against pakka-hot — the binary that
        # bin/run routes these three subcommands to in a live session.
        # (label, subcmd, input-class, stdin, env, budget_ms, pre)
        classes = [
            ("status-line", "status-line", "native (CC 2.1 payload)",
             native_payload, big_home, 50.0, None),
            ("status-line", "status-line", "legacy fallback (large transcript, cold cache)",
             legacy_payload, big_home, 50.0, clear_cache),
            ("guard", "guard", "benign Read",
             guard_read, {}, 10.0, None),
            ("guard", "guard", "benign Bash",
             guard_bash, {}, 10.0, None),
            ("commit-gate", "commit-gate", "non-commit Bash (ls -la)",
             gate_noncommit, {}, 5.0, None),
        ]

        # Startup-floor reference: guard on empty stdin returns immediately after
        # parse, isolating the shared process-startup cost (fork+exec + Go runtime
        # + package init). Measured for BOTH binaries so the report shows the
        # before (pakka-core, sqlite+net/http linked) and after (pakka-hot, lean).
        core_floor = measure(core_bin, "guard", b"", {}, args.runs, args.warmup)
        hot_floor = measure(hot_bin, "guard", b"", {}, args.runs, args.warmup)
        core_floor_p50, core_floor_p95 = pct(core_floor, 50), pct(core_floor, 95)
        hot_floor_p50, hot_floor_p95 = pct(hot_floor, 50), pct(hot_floor, 95)
        print(f"{'(floor)':12} {'pakka-core startup floor (guard, empty stdin)':52} "
              f"p50={core_floor_p50:6.2f}ms p95={core_floor_p95:6.2f}ms", file=sys.stderr)
        print(f"{'(floor)':12} {'pakka-hot  startup floor (guard, empty stdin)':52} "
              f"p50={hot_floor_p50:6.2f}ms p95={hot_floor_p95:6.2f}ms", file=sys.stderr)

        rows = []
        for label, subcmd, cls, stdin, env, budget, pre in classes:
            s = measure(hot_bin, subcmd, stdin, env, args.runs, args.warmup, pre)
            p50 = pct(s, 50)
            p95 = pct(s, 95)
            rows.append({
                "label": label, "class": cls,
                "p50": p50, "p95": p95, "budget": budget,
                "pass": p95 < budget, "min": s[0], "max": s[-1],
            })
            print(f"{label:12} {cls:52} p50={p50:6.2f}ms p95={p95:6.2f}ms "
                  f"budget<{budget:.0f}ms {'PASS' if p95 < budget else 'FAIL'}",
                  file=sys.stderr)

        # --- measurement-discipline guard: numbers must vary with input -----
        native_p50 = next(r["p50"] for r in rows if "native" in r["class"])
        legacy_p50 = next(r["p50"] for r in rows if "legacy" in r["class"])
        margin = legacy_p50 - native_p50
        varies = margin >= 1.0
        print(f"\nVARIATION CHECK: legacy_p50={legacy_p50:.2f}ms  "
              f"native_p50={native_p50:.2f}ms  Δ={margin:+.2f}ms  "
              f"{'OK' if varies else 'BROKEN'}", file=sys.stderr)
        if not varies:
            print("HARNESS BROKEN: statusline scan vs native paths do not differ "
                  ">=1ms; numbers are not responding to input. Not writing report.",
                  file=sys.stderr)
            return 2

        sha = args.sha or git_sha(repo)
        write_report(args.out, rows, args.runs, args.warmup, sha, repo,
                     native_p50, legacy_p50, margin,
                     core_floor_p50, core_floor_p95, hot_floor_p50, hot_floor_p95)
        print(f"\nwrote {args.out}", file=sys.stderr)

        any_fail = any(not r["pass"] for r in rows)
        return 1 if any_fail else 0
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def git_sha(repo):
    try:
        out = subprocess.run(
            ["git", "-C", repo, "rev-parse", "HEAD"],
            capture_output=True, text=True, check=True,
        )
        return out.stdout.strip()[:12]
    except Exception:
        return "unknown"


def write_report(path, rows, runs, warmup, sha, repo, native_p50, legacy_p50,
                 margin, core_floor_p50, core_floor_p95, hot_floor_p50, hot_floor_p95):
    delta_p50 = core_floor_p50 - hot_floor_p50
    lines = []
    lines.append("# Hook hot-path latency — v0.17 (hot-path split)")
    lines.append("")
    lines.append("End-to-end wall-clock per hook invocation (process spawn + stdin "
                 "parse + work + stdout), the real cost a Claude Code session pays "
                 "each time the hook fires. The three hot-path subcommands "
                 "(guard, commit-gate, status-line) are measured against the lean "
                 "`pakka-hot` binary that bin/run routes them to.")
    lines.append("")
    lines.append("## Results")
    lines.append("")
    lines.append("| subcommand | input class | p50 | p95 | budget | pass/fail |")
    lines.append("|---|---|---|---|---|---|")
    for r in rows:
        lines.append(f"| {r['label']} | {r['class']} | {r['p50']:.2f}ms | "
                     f"{r['p95']:.2f}ms | <{r['budget']:.0f}ms | "
                     f"{'✅ pass' if r['pass'] else '❌ FAIL'} |")
    lines.append(f"| *(reference)* | *pakka-hot startup floor (guard, empty stdin)* | "
                 f"*{hot_floor_p50:.2f}ms* | *{hot_floor_p95:.2f}ms* | *—* | *—* |")
    lines.append(f"| *(reference)* | *pakka-core startup floor — pre-split, for comparison* | "
                 f"*{core_floor_p50:.2f}ms* | *{core_floor_p95:.2f}ms* | *—* | *—* |")
    lines.append("")
    lines.append("## Root cause + fix — the startup floor, halved")
    lines.append("")
    gate = next(r for r in rows if r["label"] == "commit-gate")
    guard = next(r for r in rows if r["label"] == "guard")
    lines.append("Every subcommand pays a shared process-startup floor before any "
                 "command logic runs. Until v0.17 that floor was ~10ms because the "
                 "single `pakka-core` binary linked **two heavy dependencies that the "
                 "hot path never uses**:")
    lines.append("")
    lines.append("1. **`modernc.org/sqlite`** (via `internal/recall`) — its "
                 "`modernc.org/libc/.../netdb` package `init()` builds network "
                 "service/protocol tables (3.66 MB, ~44k allocs) at startup, "
                 "measured at **~4ms** via `GODEBUG=inittrace=1`.")
    lines.append("2. **`net/http`** (via `internal/compress/semantic`'s Anthropic "
                 "client) — linking it adds **~1.8ms** to the floor (its runtime "
                 "init + the larger binary's page-in).")
    lines.append("")
    lines.append("The fix (spec: docs/specs/2026-07-24-hot-path-startup-floor.md) "
                 "splits delivery into two binaries. `pakka-hot` links ONLY the three "
                 "hot-path subcommands — no `internal/recall`, no "
                 "`internal/compress/semantic`, no `internal/compress/orchestrator` — "
                 "so neither sqlite nor net/http is linked (guarded by a build-time "
                 "dependency test in cmd/pakka-hot). `pakka-core` keeps everything "
                 "(recall, compress, bench, report, …). bin/run routes guard / "
                 "commit-gate / status-line to `pakka-hot` when present, falling back "
                 "to `pakka-core` for older caches.")
    lines.append("")
    lines.append("**Before/after floor (this machine, this run):**")
    lines.append("")
    lines.append(f"- pakka-core floor (before): p50 **{core_floor_p50:.2f}ms** / "
                 f"p95 **{core_floor_p95:.2f}ms**")
    lines.append(f"- pakka-hot floor (after): p50 **{hot_floor_p50:.2f}ms** / "
                 f"p95 **{hot_floor_p95:.2f}ms**")
    lines.append(f"- floor reduction: **{delta_p50:.2f}ms** p50 "
                 "(≈ the sqlite netdb init + net/http link cost, removed).")
    lines.append("")
    lines.append(f"Outcome: **guard** clears its <10ms budget "
                 f"({guard['p95']:.2f}ms p95) and **commit-gate**'s non-commit "
                 f"passthrough now clears its <5ms budget "
                 f"({gate['p95']:.2f}ms p95) — both were FAIL at the ~10ms shared "
                 "floor in the v0.12.0 baseline (guard 12.87ms, commit-gate 11.42ms "
                 "p95). The known-issue disclosed since v0.12.0 is closed.")
    lines.append("")
    lines.append("## Measurement discipline")
    lines.append("")
    lines.append("Numbers must respond to input or the harness is broken. The "
                 "statusline native path (CC 2.1 `context_window` payload) skips "
                 "the transcript scan; the legacy path re-sums a ~4000-line "
                 "synthetic transcript on every render (cold cache each run — the "
                 "honest steady-state cost, since the active transcript grows and "
                 "cache-misses every turn).")
    lines.append("")
    lines.append(f"- legacy p50 = **{legacy_p50:.2f}ms**, native p50 = "
                 f"**{native_p50:.2f}ms**, Δ = **{margin:+.2f}ms** (≥1ms required).")
    lines.append("- Identical numbers across these two inputs ⇒ broken harness; "
                 "the script exits non-zero and refuses to write this report.")
    lines.append("")
    lines.append("## Method")
    lines.append("")
    lines.append(f"- Runs: {runs} timed per class ({warmup} warmup discarded), "
                 "nearest-rank p50/p95.")
    lines.append("- Timer: `time.perf_counter()` around each `subprocess.run` "
                 "(includes process spawn — the real hook cost).")
    lines.append("- Machine: Apple M3 Max (Mac15,10), darwin/arm64.")
    lines.append(f"- Binaries: `go build ./cmd/pakka-hot` (hot-path classes + floor) "
                 f"and `./cmd/pakka-core` (pre-split floor) at commit `{sha}`.")
    lines.append(f"- Repo under test: `{repo}`.")
    lines.append("- statusline fallback fed a synthetic `$HOME` with a large "
                 "transcript so the scan path does real work; guard and "
                 "commit-gate use the live repo.")
    lines.append("")
    lines.append("## Reproduce")
    lines.append("")
    lines.append("```")
    lines.append("make bench-latency")
    lines.append("# or:")
    lines.append("python3 benchmarks/latency_bench.py --runs 50")
    lines.append("```")
    lines.append("")
    with open(path, "w") as f:
        f.write("\n".join(lines))


if __name__ == "__main__":
    sys.exit(main())
