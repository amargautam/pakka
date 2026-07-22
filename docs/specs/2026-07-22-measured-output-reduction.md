# Measured output reduction
Date: 2026-07-22
Status: draft

## Problem
Status-line output savings derive from a fixed per-level multiplier calibrated once (2026-05-02) against benchmark samples. Enterprise feedback flagged it: the headline reduction figure is a constant, not a measurement, and the code comment promising per-session tracking went stale at v0.3.0. The v0.13.0 cache-aware fix made input-side savings honest; output side still reports calibration folklore as if it were evidence.

## User stories
- As a pakka user, I want the output-savings figure grounded in measured data for my repo and model so that the status line reports evidence.
- As an enterprise evaluator, I want the ratio provenance visible (measured vs default) so that I can judge the claim.

## Module decisions
- `make bench` A/B already produces paired pakka-on/off output token counts -> persist measured reduction ratio to ~/.pakka/bench-ratios.json keyed by repo+model+level.
- Multiplier resolution order: repo+model+level measured -> model+level measured -> global calibrated constant (current behavior).
- RECEIPTS/self-report disclose ratio source and sample count; status-line $ format unchanged (tokens AND percent stay).
- No counterfactual invented outside A/B data. Bench stays on Claude Code OAuth, no API keys.

## Acceptance criteria
1. A bench run persists a ratio entry (repo, model, level, ratio, samples, timestamp) to ~/.pakka/bench-ratios.json; second run increments samples.
2. With a measured ratio present, statusline savings use it; without the file, calibrated constant applies. Test: identical telemetry, ratio != constant -> different $ figures.
3. Generated RECEIPTS/self-report contains ratio source string ("measured, n=K" or "default calibration").
4. Behavioral: two different stored ratios -> two different $ figures for same telemetry.
5. go test ./... exit 0.

## Out of scope
- Per-session counterfactuals without A/B data.
- Live mid-session A/B sampling.
- Website copy (docs-sync).

## Open questions
