# v0.12.0 — consolidation: repo sync, level convergence, CC 2.1 compat + optimization
Date: 2026-07-21
Status: draft

## Problem
Repo state diverged: local main is 2 commits behind origin (PR #26/#27 squashes), local carries an unpushed CHANGELOG commit plus uncommitted, unlogged #28 WIP (141 insertions, tests green). Issue #28 remains open: three code paths (compress_cmd.go:66, orchestrator.go:78, compress.go:122) fall back to ultra while brand default is super-ultra. Plugin was built against Claude Code 2.0.x contracts; installed CC is 2.1.216 — docs audit confirms zero breaking changes, but statusline hand-parses transcript JSONL for metrics that native statusLine JSON now provides (cost.*, context_window.current_usage), and hook hot-path latency has no current measurements. Consolidation before any new feature work.

## User stories
- As a pakka user, I want level fallbacks converged so my configured compression level applies uniformly across every code path.
- As a pakka user, I want the plugin verified against CC 2.1.216 so hooks and commands behave per the current contract.
- As a pakka user, I want hook overhead measured and bounded so pakka never perceptibly slows a session.
- As the maintainer, I want main synced and WIP landed so local == origin == released state.

## Module decisions
- Repo sync first: rebase local main (08a48f9) onto origin/main; #28 WIP preserved via stash or branch; builder-executed, never Larry.
- #28: single fallback source = resolveOutputLevel in cmd/pakka-core; existing WIP diff-reviewed against issue #28 acceptance before adoption — discard and rewrite if wrong.
- CC 2.1 adoption, low-risk only: plugin.json displayName; statusline migrates to native statusLine JSON fields (context_window, cost.*) where they replace transcript scans — perf win, fewer file reads; PreToolUse envelope already current (docs-confirmed), contract test stays.
- Latency budgets: statusline render <50ms p95; guard PreToolUse <10ms p95; commit-gate non-commit Bash passthrough <5ms p95. Measured on this repo, 20+ runs, recorded in benchmarks/.
- New 2.1 capabilities (PreCompact/PostCompact, prompt/agent hook types, userConfig, subagentStatusLine): deferred to v0.13 investigation — out of this pass.
- Release: full /release checklist, docs drift audit, tag v0.12.0.

## Acceptance criteria
1. `git rev-list main..origin/main --count` = 0 and local main contains PR #26/#27 squash commits.
2. Issue #28 closed via merged PR: `go test ./...` exit 0; compress_cmd.go, orchestrator.go, compress.go, semantic.go contain no independent ultra fallback literal — all resolve through resolveOutputLevel (or its single exported equivalent).
3. plugin.json contains displayName; `claude plugin validate` exits 0.
4. Existing hook-envelope contract tests (TestEmitCommitRewrite_HookSpecificOutputShape et al.) pass against CC 2.1 documented shape.
5. Statusline p95 <50ms over 20 runs on this repo; figure committed to benchmarks/.
6. Guard p95 <10ms over 20+ runs; commit-gate non-commit Bash passthrough p95 measured and published. [Amended 2026-07-21, pre-tag: original <5ms passthrough budget MISSED — measured 9.2ms p95, root cause shared-binary startup floor (~3ms modernc/libc netdb init via recall SQLite), unfixable without binary split. Miss accepted and disclosed in benchmarks/latency-v0.12.0.md + CHANGELOG Known; 5ms target transfers to #17. Guard passed at 9ms p95. Decision recorded in memory/DECISIONS.md "Commit-gate latency budget miss".]
7. Statusline metrics that native statusLine JSON provides (context window usage, cost) read from hook payload, not transcript rescans; savings meter (pakka-specific) unchanged; status line still shows tokens AND percent.
8. Docs drift audit clean at tag: README skill/command counts match skills/ and commands/ trees; website claim numbers match README.
9. v0.12.0 tagged; GitHub releases live on amargautam/pakka and amargautam/pakka-marketplace; marketplace ref → v0.12.0; plugin.json version = 0.12.0.
10. CHANGELOG carries v0.12.0 entry listing #26, #27 (first release shipping them) and #28.
11. memory/LOG.md updated end of session.

## Out of scope
- #14 predictive intervention.
- #17 plugin split.
- Adopting new hook events or prompt/agent hook types.
- userConfig, channels, themes, monitors.
- vs-raw bench calibration.
- Website redesign; any new user-facing feature.

## Open questions
- #28 WIP provenance unknown (uncommitted, absent from LOG.md). Builder diff-reviews against issue before adopting; discard if it misses acceptance.
- docs/specs/.bungu/ untracked dir (chats/, index) — unknown origin, not touched this pass; user to identify or approve removal.