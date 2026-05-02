# Architect agent in review gate
Date: 2026-05-02
Status: draft

## Problem

The review gate catches logic bugs (reviewer) and attack surface (security) but misses architecture drift. A shallow wrapper, a new cross-layer coupling, or a module taking on too many responsibilities ships undetected. Architecture drift compounds — each slipped pattern makes the next one cheaper to justify. The result is rework cost that exceeds the token savings pakka delivers elsewhere. The gate exists to prevent rework; not catching architecture drift undermines the entire thesis.

## User stories

- As a developer, I want the review gate to block commits that introduce coupling or shallow abstractions so that architecture drift never reaches main.
- As a developer, I want all three review agents (correctness, security, architecture) to run in parallel so that gate latency does not increase.

## Module decisions

- New file: `agents/architect.md` — architecture reviewer subagent. Opus. Tools: Read, Bash.
- `kind` field value: `"architecture"` — distinguishes findings in the JSONL log.
- Diff-first scope: architect reads `git diff --cached` as primary input. May Read files touched by the diff to resolve coupling context — never reads unrelated files.
- `line` field: required same as other agents. For file-level or module-level findings with no single line, point to the first line of the new function, type, or file that introduces the issue.
- Confidence threshold: same as reviewer/security — `pakka.review.confidenceThreshold` (default 80). No new config key.
- `severity=error` blocks commit (exit 2). `severity=warn` passes through.
- `commands/review.md` step 3: add `architect` as third parallel agent alongside `reviewer` and `security`.
- Findings from architect pass through same pipeline: changed-line scope filter, confidence filter, group-print, verdict.
- Version bump: `plugin.json` → 0.2.5.

## Acceptance criteria

1. `agents/architect.md` exists with correct frontmatter (`name: architect`, `model: opus`, `tools: Read, Bash`) and a Red Flags section.
2. `/pakka:review` launches three agents in parallel — `reviewer`, `security`, `architect` — confirmed by three concurrent Agent tool calls in the transcript.
3. Architect findings use JSON shape `{"kind":"architecture","file":"...","line":N,"severity":"warn|error","confidence":N,"rationale":"...","fix":"..."}` — no extra or missing fields.
4. A diff that introduces a shallow wrapper function (delegates entirely to one other function, adds no logic) produces an architect finding with `severity=error` and `confidence ≥ 80`.
5. A diff that adds a new import creating a cross-layer dependency (e.g. handler package importing a DB package directly) produces an architect finding with `severity=error` and `confidence ≥ 80`.
6. A clean diff with no structural issues produces zero architect findings.
7. Architect finding with `confidence < 80` is filtered out — does not appear in output and does not affect verdict.
8. Architect finding on a line not in the changed-line set is filtered out — does not appear in output.
9. `/pakka:review` exits 2 when architect emits a `severity=error` finding above threshold, same as reviewer/security.
10. `/pakka:review` exits 0 when architect emits only `severity=warn` findings or zero findings, and reviewer + security also pass.
11. `go test ./...` exits 0 after changes.

## Out of scope

- Architect agent scanning files not touched by the diff.
- New config keys — uses existing `pakka.review.confidenceThreshold`.
- Auto-fix suggestions that require refactoring (architect reports, does not fix).
- Performance review (N+1 queries etc.) — that stays with the correctness reviewer.
- Full codebase architecture report — use `/pakka:build` audit mode for that.

## Open questions

None. Proceed to build.
