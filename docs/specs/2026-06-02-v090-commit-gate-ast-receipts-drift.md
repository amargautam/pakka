# pakka v0.9.0 — commit-gate AST, RECEIPTS persistence, docs drift
Date: 2026-06-02
Status: draft

## Problem
Three drifts compound across releases:

1. **commit-gate substring fallback** still blocks all chained shapes (`git commit && git push`, `make test && git commit`, env-prefixed, subshells, redirects). v0.8.1 fixed the quoted-mention false-positive but kept the chain-rejection path. Users hit it daily; current escape is decomposing every chain into separate bash calls.
2. **RECEIPTS output-token figure shrinks across releases.** Source of truth is Claude Code transcripts, which are pruned. Meter records session count (monotonic) but not output tokens. Result: v0.8.0 reported 7.3M output tokens across 340 sessions; v0.8.1 reported 2.1M across 369 sessions. Number degrades with every release.
3. **Stale docs.** `CLAUDE.md` root references `v0.3.0-dev`; release checklist `step 1.5 part 2` lists website component files that no longer exist; README skill counts drift from `pakka/skills/` reality.

## User stories
- As a user running `git add -A && git commit -m "msg"`, I want pakka to allow the chain (with trailer spliced into the commit segment), instead of being told to split.
- As a maintainer reading RECEIPTS.md, I want the output-token figure to grow monotonically with usage, not shrink because Claude Code rotated transcripts.
- As a releaser, I want `/release` checklist steps that all reference real files and real procedures.

## Module decisions
- AST parser: `mvdan.cc/sh/v3` (MIT, only mature Go shell parser). First non-stdlib dep in `internal/commitgate/`. Justified per `pakka/CLAUDE.md` "stdlib-first, deps only if stdlib awful" — POSIX shell lexing meets the bar.
- AST integration lives in new `internal/commitgate/ast.go`; `commitgate.go` keeps the string-based `IsGitCommit` as fast path for the three already-supported shapes.
- Decision flow: try fast-path first; on miss, try AST path; on AST parse fail or unsupported construct (eval/dynamic command name), block with explicit cause.
- Trailer injection on AST path walks every CallExpr whose head is the git command (or env-prefix wrapper) and whose subcommand is commit. Idempotency check per node.
- Meter writes output_tokens per session-end (existing struct field, no schema change). Source: RepoOutputTokens(repoRoot) called at SessionEnd hook, stored in the session meter line.
- One-time backfill tool: pakka-core backfill-output-tokens reads current transcripts plus meter, writes per-session output tokens into matching meter files. Idempotent. Run once before tagging v0.9.0.
- report.go line 85-89 transcript override removed. Source of truth becomes meter only.
- /release checklist: prune dead step 1.5 part 2; add doc-sync audit substep covering CLAUDE.md root version refs and README skill count.

## Acceptance criteria
1. Given cmd `git add -A && git ci -m "msg"` (with ci replaced by commit), commitgate.Evaluate returns Decision.Allow=true and Decision.Command contains the original chain with --trailer 'Reviewed-by-pakka: ...' spliced into the commit segment.
2. Given cmd `make test && git ci -m "msg" && git push`, same as above — trailer in commit segment only, surrounding chain preserved verbatim.
3. Given env-prefixed cmd `GIT_AUTHOR_DATE=2026-01-01 git ci -m "msg"`, Decision.Allow=true with trailer spliced after env prefix.
4. Given subshell cmd `(cd /tmp/repo && git ci -m "msg")`, Decision.Allow=true with trailer spliced inside subshell.
5. Given redirected cmd `git ci -m "x" > /dev/null 2>&1`, Decision.Allow=true; redirects preserved post-trailer.
6. Given chained-dup cmd `git ci && git ci`, Decision.Allow=true; trailer spliced into both commit nodes.
7. Given cmd `eval "git ci -m msg"`, Decision.Allow=false; stderr names eval as the rejection cause.
8. Given cmd `$(echo git ci) -m msg`, Decision.Allow=false; stderr names dynamic command name as cause.
9. All existing commit-gate tests in internal/commitgate/*_test.go pass without modification.
10. `go test ./...` exits 0; new tests in internal/commitgate/ast_test.go cover criteria 1-8 with one row per shape, no aggregate assertions.
11. After v0.9.0 SessionEnd hook fires on a session with N transcript output tokens, the session meter file in ~/.pakka/meter/<id>.jsonl contains a line with `"output_tokens": N`.
12. `pakka-core backfill-output-tokens` run on the dev machine produces a meter where sum(output_tokens across all sessions) >= 7,294,774 (matches or exceeds the v0.8.0 high-water mark).
13. `make self-report` after backfill produces RECEIPTS.md whose "Output tokens measured across N sessions" figure equals sum(output_tokens) from meter (not transcripts).
14. RECEIPTS.md generated for v0.9.0+1 (next release after v0.9.0) shows output-tokens figure >= v0.9.0 figure (monotonic).
15. .claude/commands/release.md step 1.5 references only files that exist at HEAD of pakka-website/src/. Grep verification: every file path quoted in the step must resolve.
16. .claude/commands/release.md includes a doc-sync substep that (a) greps CLAUDE.md root for version strings, flags any not matching the release tag; (b) counts pakka/skills/ entries and compares to README claim.
17. CLAUDE.md root pass-structure section updated to reflect v0.8.1 + v0.9.0 reality. No more references to v0.3.0-dev as the active branch.

## Out of scope
- Bash AST features beyond what's needed for commit-gate (no general bash linter).
- Schema migration for old meter files predating output_tokens field — backfill handles them by reading transcripts; older entries that have no transcript stay zero.
- Website savings-figure stats refresh on a different cadence than release tagging.
- Replacing the meter file format (stays JSONL).
- Replacing mvdan.cc/sh/v3 with hand-rolled lexer — not worth the engineering.
- Trailer injection in bash -c strings — treated as dynamic, blocked.
- Multi-process job graphs (& background, wait).

## Open questions
- None — pragmatic AST subset chosen, dep approved, fix-forward plus backfill chosen for RECEIPTS, docs-audit substep in scope.
