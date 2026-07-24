# Policy floor + diff-bound marker freshness
Date: 2026-07-24
Status: draft

## Problem
Every pakka enforcement knob (gate confidence threshold, marker freshness, guard allowlist behavior, input-compress opt-in) is local, user-writable configuration — one engineer can quietly detune the gate the whole team relies on, which blocks org adoption: an enterprise cannot deploy a quality gate that is advisory in practice. Separately, marker freshness is a flat 300s wall clock: while iterating, an UNCHANGED staged diff forces the full re-review dance every five minutes even though v0.15.0 markers bind to content — time-only expiry is a pre-diff-binding leftover that now only taxes DX.

## User stories
- As an engineering org, I want a committed policy file whose minimums the binary enforces so that local settings cannot weaken the gate my team standardized on.
- As a developer iterating on one change, I want a review pass to stay valid while the staged diff is byte-identical so that I re-review when content changes, not when a timer fires.
- As an auditor, I want policy violations visible (which floor clamped what) so that drift attempts are observable, not silent.

## Module decisions
- Policy file: `<repo>/.pakka/policy.json`, committed, read by the gate/guard binaries directly (Go enforcement, not skill prompts). Absent file → current behavior exactly.
- Floor semantics — policy wins only in the strict direction: confidenceThreshold local above policy value is clamped down (policy sets maximum filtering, i.e. minimum sensitivity); markerFreshnessSeconds local capped by policy; guard `lockedCategories` can never be overridden by the learned per-repo override list; `inputCompress: "locked-off"` ignores local opt-in. Unknown policy keys → gate warns on stderr, continues (forward compat).
- Freshness: marker with diffSHA256 matching current staged diff is valid for `markerFreshnessSeconds` (default 1800); non-matching or legacy markers keep failing as today. Exact rule: match window 1800s default, policy may lower, never raise above 3600s hard ceiling.
- Clamping is logged: one stderr line naming the clamped key + policy value; also an audit entry kind "policy-clamp".
- Policy schema versioned (`"v": 1`); newer major version → gate blocks with upgrade message (fail closed on unknown policy version, fail open on absent policy).

## Acceptance criteria
1. No policy file → byte-identical gate/guard behavior to v0.18.0 (existing tests unaffected).
2. Policy `confidenceThreshold: 80` + local setting 95 → findings filtered at 80; stderr names the clamp; audit entry kind "policy-clamp" written. Local 70 (stricter) → 70 used, no clamp.
3. Policy `lockedCategories: ["secrets"]` → a guard block in a locked category cannot be overridden: a learned-override entry for that category is ignored, block stands.
4. Policy `inputCompress: "locked-off"` + `PAKKA_INPUT_COMPRESS=1` → auto-compress does not run; explicit /pakka:compress orchestrator run also refuses with policy message.
5. Marker freshness: marker whose diffSHA256 matches current staged diff passes at age 25min (default window 1800s); same marker at age > window fails stale. Marker with non-matching diff fails regardless of age (existing behavior).
6. Policy `markerFreshnessSeconds: 300` → 20-min-old matching marker fails; local setting 3600 with policy 300 → 300 wins. No policy → 1800 default. Values above 3600 rejected at parse with named error.
7. Policy file with `"v": 2` → gate blocks any commit with an upgrade message (fail closed); malformed JSON → same (fail closed, message names the file).
8. go test ./... exit 0; make test-js exit 0.

## Out of scope
- Central/remote policy distribution (file is committed to the repo — git IS the distribution).
- Fleet audit aggregation.
- Reviewer calibration evals (v0.20).
- Signing the policy file.

## Open questions
