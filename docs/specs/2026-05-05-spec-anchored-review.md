# Spec-anchored review
Date: 2026-05-05
Status: draft

## Problem
`/pakka:review` runs reviewer agents blind — no knowledge of what the code was supposed to do. Code that misses acceptance criteria or implements out-of-scope work passes the gate undetected. Divergence from intent is invisible.

## User stories
- As a developer, I want `/pakka:review` to check my diff against the relevant spec so that acceptance criteria gaps and out-of-scope additions are caught before commit.
- As a developer on a project without specs, I want review to run normally with zero noise.
- As a developer whose spec isn't auto-matched, I want to pass `--spec <file>` to anchor the review explicitly.

## Module decisions
- New subcommand: `pakka-core spec-find --branch <name> --changed <file,...>` — returns matching spec path or empty string.
- Discovery order: `--spec` flag → name match → LLM fallback → no match.
- Name match: case-insensitive substring of branch name or any changed-file path against spec filenames in `docs/specs/`.
- LLM fallback: `claude -p` subprocess reads all spec files, returns JSON `{path, confidence}`. Fires only when name match fails. Confidence threshold: 0.7; below = no match.
- `docs/specs/` absent → silent skip; review runs as today.
- `docs/specs/` present, no match → single advisory line appended to review output; review continues.
- Matched spec content injected into all three reviewer agent prompts (reviewer, security, architect) as additional context block.
- Reviewer agents check two new dimensions: (1) acceptance criteria compliance, (2) out-of-scope additions.
- Gate block triggers: existing severity threshold OR any spec-divergence finding.
- Spec-divergence findings always emit `severity=error` — no warn variant. Existing gate logic then catches them automatically.
- Target: v0.4.0 on `v0.4.0-dev` branch.

## Acceptance criteria
1. `pakka-core spec-find` returns correct spec path when branch name contains a token matching a spec filename (case-insensitive).
2. `pakka-core spec-find` does NOT invoke LLM when name match succeeds.
3. `pakka-core spec-find` invokes LLM fallback only when name match fails; returns path when confidence ≥ 0.7, empty when below.
4. `/pakka:review` with matched spec: all three reviewer agent prompts contain spec content.
5. Reviewer agents report spec-divergence findings when a diffed function contradicts an acceptance criterion.
6. Gate blocks commit when spec-divergence finding is present, even if severity score is below the standard threshold.
7. `docs/specs/` absent: no advisory emitted, review output identical to today's behavior.
8. `docs/specs/` present, no match: exactly one advisory line in review output; gate does not block.
9. `--spec <path>` flag bypasses discovery entirely; specified file used as-is.
10. `pakka-core spec-find` has table-driven tests covering: name match hit, name match miss → LLM hit, name match miss → LLM below threshold, directory absent.

## Out of scope
- Auto-generating specs from code or diffs.
- Spec versioning or history tracking.
- Matching multiple spec files to a single review.
- Any web UI or spec browser.
- Enforcing spec existence (missing spec never blocks).

## Judge prompt — spec-match (LLM fallback)

File: `internal/specfind/spec_match_prompt.md`

Input variables: `{{specs}}` (array of `{filename, heading, acceptance_criteria, out_of_scope}`— first heading + those two sections only, not full content), `{{branch}}`, `{{changed_files}}`

Output schema (strict JSON, no prose):
```json
{
  "match": "<filename or empty string>",
  "confidence": 0.0
}
```

Rules baked into prompt:
- Return the single best match only. Never return multiple.
- `confidence` range: 0.0–1.0. Threshold for acceptance: 0.7.
- If no spec scores ≥ 0.7, return `{"match": "", "confidence": 0.0}`.
- Base confidence on: spec acceptance criteria overlap with changed files, branch name semantic match, out-of-scope list relevance.

## Open questions
None.
