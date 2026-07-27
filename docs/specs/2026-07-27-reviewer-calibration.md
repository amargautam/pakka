# Reviewer calibration — measured precision/recall for the review gate
Date: 2026-07-27
Status: draft

## Problem
The review gate's core claim — "catches bugs" — is backed by a verdict count (bugs caught), not a rate: nothing measures how often the four reviewer agents find a planted bug (recall) or how often their findings are real (precision). The seeded-bug corpus for exactly this exists (benchmarks/seeds/: 10 bug classes with expected.json ground truth, 3 clean fixtures, 3 perf seeds) but eval layers 2-3 report "deferred to skill wrapper (requires headless claude -p)" for every target — the harness was never built. Meanwhile the meter's claims were made honest in v0.13-0.14; the gate's headline claim is the last unmeasured one.

## User stories
- As a pakka user, I want measured reviewer precision/recall in RECEIPTS so that the gate's value is a rate I can judge, not a count I must trust.
- As a maintainer, I want per-seed scoring artifacts so that reviewer-agent prompt changes are calibrated against ground truth instead of vibes.
- As an enterprise evaluator, I want false-positive rates on clean fixtures so that I can predict gate friction before deployment.

## Module decisions
- Harness: `make calibrate` → Go command `calibrate` (in internal/cli, pakka-core only — NOT pakka-hot) that, per seed: materializes the seed fixture into a temp repo, applies seed.patch as the staged diff, invokes reviewer agents headless via `claude -p` with the same agent prompt files the live gate uses (agents/*.md), parses findings JSON, scores against expected.json.
- Scoring: a seed is RECALLED when any finding matches expected bug_class or (file AND line within ±5) at confidence >= gate threshold. Finding on a clean fixture = false positive. Precision = matched findings / all findings across bug seeds; FP rate = clean-fixture findings / clean runs.
- Auth: Claude Code OAuth session only (claude CLI on PATH). ANTHROPIC_API_KEY must NOT be read; harness refuses to run if only an API key is present (standing rule: no API-key paths). claude CLI absent → named skip, exit 0, no scores written.
- Artifacts: per-run JSON to benchmarks/results/calibration-<date>.json (per-seed verdicts + rates + model + agent-file SHAs); RECEIPTS.md gains "review gate calibration" section: recall, precision, FP rate, n, date, model — "unmeasured" placeholder when no artifact exists.
- Cost control: explicit invocation only (make calibrate), never per-commit; seeds run sequentially with per-seed timeout.
- Determinism honesty: rates carry n and model; no averaging across models.

## Acceptance criteria
1. `make calibrate` with claude CLI present runs all 16 seeds, writes benchmarks/results/calibration-<date>.json containing per-seed {seed, expected, findings, recalled|falsePositive} + aggregate {recall, precision, fpRate, n, model}.
2. Scoring unit-tested against fixture findings JSON (no live model): expected-match by bug_class, by file+line window, clean-fixture FP counting, threshold filtering — all covered with variation (different fixtures → different rates).
3. `make self-report` after a calibration artifact exists → RECEIPTS.md contains the calibration section with the artifact's rates; without artifact → section shows "unmeasured" (string asserted in test).
4. claude CLI absent → `make calibrate` exits 0 with named skip message, writes nothing.
5. ANTHROPIC_API_KEY set but claude CLI absent → still skip (never falls back to API); harness code contains no ANTHROPIC_API_KEY read (asserted by grep in a test or code review).
6. Per-seed timeout enforced; a hung seed marks that seed "timeout" and the run continues.
7. pakka-hot forbidden-deps test still green (calibrate lives in pakka-core CLI only).
8. go test ./... exit 0; make test-js exit 0.

## Out of scope
- Auto-tuning reviewer prompts from scores.
- Per-commit calibration.
- Expanding the seed corpus (existing 16 only; corpus growth is follow-up).
- Cross-model leaderboards.

## Open questions
