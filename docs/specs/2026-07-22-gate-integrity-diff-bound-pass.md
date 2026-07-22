# Gate integrity — diff-bound review pass
Date: 2026-07-22
Status: draft

## Problem
The commit gate accepts any fresh (300s) epoch in .pakka/reviews/last-pass-ts as proof a review passed. Nothing binds the marker to what was reviewed: a marker minted for one diff authorizes any other commit in the window, stale markers from other agents authorize unrelated work, and any process can stamp the file (observed five times in one day of dogfooding, including by the orchestrating session itself). For a product whose promise is "proves both via gate enforcement," the gate proves only that someone wrote a timestamp. Separately, the gate's injected --trailer args collide with `git commit -- <pathspec>` form, parsed as pathspecs (hit twice today).

## User stories
- As a pakka user, I want the gate to verify the review covered the exact staged diff so that a pass cannot authorize different changes.
- As an auditor, I want pass markers to record what was approved so that gate history is evidence, not folklore.

## Module decisions
- Marker format: JSON {ts, diffSHA256, verdict} at existing path .pakka/reviews/last-pass-ts; diffSHA256 = sha256 of raw `git diff --cached` bytes.
- New subcommand `pakka-core review-pass`: computes staged-diff hash, writes marker atomically. Review flows call it instead of shell-redirecting an epoch.
- Gate (internal/commitgate): pass requires marker fresh AND diffSHA256 == recomputed staged-diff hash. Legacy bare-epoch markers rejected with upgrade message.
- Trailer injection fix: inject --trailer flags before any `--` separator in the wrapped git commit argv.
- Skill/docs update: /pakka:review skill md + agent docs call review-pass.
- Non-goal honesty: review-pass can still be invoked without a genuine review; this release eliminates stale/foreign/accidental marker reuse and makes stamping a deliberate, diff-bound, auditable act. Cryptographic binding of reviewer output is future work.

## Acceptance criteria
1. `pakka-core review-pass` with a staged diff writes JSON marker {ts, diffSHA256, verdict:"passed"}; exit 0. Empty staged diff: exit nonzero, no marker written.
2. Gate allows commit when marker is fresh and diffSHA256 matches current staged diff.
3. Gate blocks when staged diff differs from marker diffSHA256; stderr names the mismatch. Test: marker minted, then extra file staged, commit blocked.
4. Gate blocks legacy bare-epoch marker; stderr instructs re-running review.
5. Identical re-staging (same content) yields same hash: commit passes.
6. `git commit --trailer`-injected form with `-- <pathspec>` succeeds; regression test covers the collision.
7. /pakka:review skill instructions and any hook docs reference review-pass, not `date +%s >`.
8. go test ./... exit 0.

## Out of scope
- HMAC/signature on markers; binding reviewer agent transcripts.
- Cross-repo or cross-session marker sharing.
- Commit-gate p95 latency (#17).
- Website copy beyond docs-sync.

## Open questions
