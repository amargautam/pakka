---
name: pakka-debug
description: Build a deterministic fail/pass signal before touching code — then reproduce, rank hypotheses, instrument one variable at a time, and lock the fix down with a regression test. Context waste IS the bug; a precise feedback loop gives precise context. Use when debugging a failure, chasing a performance regression, or told something is broken.
allowed-tools: Bash, Read, Glob, Grep, Agent
argument-hint: "[description of bug]"
user-invocable: true
---

## Thesis

No feedback loop = no debugging. Context waste IS the bug — precise signal gives precise context. A 2-second deterministic fail/pass loop beats an hour of reading code.

## Phase 1 — Build a feedback loop (this is the skill)

Spend disproportionate effort here. Everything else is mechanical.

Build in order:
1. **Failing test** at nearest correct seam — unit, integration, e2e
2. **CLI invocation** diffing output against known-good snapshot
3. **HTTP/API script** against running dev server
4. **Throwaway harness** — minimal code path, mocked edges
5. **Property/fuzz loop** — 1000 inputs, look for failure mode
6. **Bisection harness** — automate "boot at state X, check, repeat" for `git bisect run`
7. **Differential** — old-version vs new-version on same input, diff outputs

Once built, improve the loop:
- Faster? Narrow scope, skip unrelated init.
- Sharper? Assert on exact symptom, not "didn't crash."
- More deterministic? Pin time, seed RNG, freeze network, isolate filesystem.

Non-deterministic bugs: raise reproduction rate to ≥50%. Loop 100×, parallelise, inject stress.

**If no loop can be built:** stop. Tell user what you tried. Ask for: (a) environment access, (b) captured artifact (log dump, trace, HAR file, core dump), (c) permission to add temporary instrumentation. Do NOT hypothesize without a loop.

## Phase 2 — Reproduce

Run the loop. Confirm:
- [ ] Failure matches what the **user** described — not a nearby failure
- [ ] Reproducible across runs (or at sufficient rate for non-deterministic bug)
- [ ] Exact symptom captured (error text, wrong output, timing delta)

## Phase 3 — Hypothesize

Generate 3–5 ranked hypotheses **before** testing any. Each must be falsifiable:

> "If X is the cause, then changing Y makes the bug disappear / changing Z makes it worse."

Show ranked list before testing. User often re-ranks instantly. Don't block — proceed if AFK.

## Phase 4 — Instrument

One probe per hypothesis. One variable changed at a time.

Prefer: debugger/REPL → targeted logs. Never "log everything and grep."

Tag every debug log: `[DEBUG-xxxx]`. One `grep "[DEBUG-"` removes all of them at cleanup.

Perf regressions: measure first (profiler, timing harness, query plan), bisect second. Log-and-guess is wrong.

## Phase 5 — Fix + regression test

Write regression test **before** the fix — but only if there is a correct seam.

Correct seam: test exercises the real bug pattern at its actual call site. Wrong seam = false confidence.

If no correct seam exists: document that. The architecture is preventing lockdown. Flag for `/pakka:review-architecture`.

1. Write failing test at correct seam
2. Watch it fail
3. Apply minimal fix
4. Watch it pass
5. Re-run Phase 1 loop on original scenario

## Phase 6 — Cleanup

- [ ] Original repro no longer reproduces
- [ ] Regression test passes — or absence of seam documented
- [ ] All `[DEBUG-...]` tags removed (`grep "[DEBUG-"` to verify)
- [ ] Throwaway harnesses deleted
- [ ] Root cause stated in commit message — next debugger learns

Then ask: what would have prevented this? If architectural → hand off to `/pakka:review-architecture`.

## Red Flags

- Hypothesizing before a feedback loop exists → wrong. Phase 1 is mandatory.
- Loop runs once and passes → not enough. Verify it's deterministic.
- Regression test at wrong seam (tests internals, not behavior) → false confidence. Document missing seam instead.
- Debug logs left in → `grep "[DEBUG-"` before declaring done.
- Declaring done without re-running Phase 1 loop on original scenario → Phase 5 step 5 is mandatory.
- "Log everything and grep" instrumentation strategy → wrong. Targeted probes only.
