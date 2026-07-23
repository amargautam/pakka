# Review provenance — findings bound to markers, trailers, recall
Date: 2026-07-23
Status: draft

## Problem
v0.15.0 bound the pass marker to the reviewed diff, but the review EVIDENCE — the findings JSONL the reviewer agents produce — stays a loose untracked file: unlinked from the marker, absent from the commit, invisible to recall. "Audit-ready" currently means "a commit was gated," not "here is what the review found." A developer (or auditor) cannot answer "which review approved this commit and what did it flag" without archaeology.

## User stories
- As a developer, I want the commit itself to carry digests of the reviewed diff and the findings so that provenance survives in git history.
- As a developer, I want review findings searchable via /pakka:recall so that "what did reviews flag about file X" is one query.
- As an auditor, I want the gate to reject a pass whose findings file changed after review so that evidence cannot be swapped post-approval.

## Module decisions
- `review-pass --findings <verdict.jsonl>`: marker JSON gains findingsSHA256 + findingsCounts {error,warning,info,total} parsed from the file. Flag optional — without it marker keeps v0.15 shape (back-compat).
- Gate on successful pass: append audit entry kind "review-verdict" (diffSHA256, findingsSHA256, counts, findings rationale text) to the session audit log — existing recall Index picks it up with zero recall-schema change.
- Reviewed-by-pakka trailer extended: `diff:<8hex>` always; `findings:<8hex> (<E> errors, <W> warnings)` when bound.
- Gate re-hashes the findings file at commit time; mismatch → block (same class as diff mismatch).
- Skill doc: /pakka:review PASS step writes verdict JSONL then calls review-pass --findings with it.

## Acceptance criteria
1. review-pass --findings <path> writes marker containing findingsSHA256 (sha256 of file bytes) and findingsCounts matching the file's severity tallies; unreadable path → nonzero exit, no marker written.
2. review-pass without --findings: marker byte-shape identical to v0.15 (no findings fields).
3. Successful gate pass with bound findings appends an audit-log entry kind "review-verdict"; after recall Index, an FTS5 query on a rationale keyword from the findings returns that entry.
4. Gate-injected commit trailer contains diff:<8hex>; with bound findings also findings:<8hex> and error/warning counts — asserted via git log in a temp-repo test.
5. Findings file modified after review-pass → gate blocks; stderr names findings mismatch.
6. commands/review.md PASS step documents review-pass --findings.
7. go test ./... exit 0.

## Out of scope
- Cryptographic signing of markers or findings.
- Capturing reviewer agent transcripts.
- Recall query-syntax additions (FTS5 content search suffices).
- Commit-gate p95 (#17).

## Open questions
