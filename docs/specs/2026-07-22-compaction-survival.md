# Compaction survival + spec-generate repo guard
Date: 2026-07-22
Status: amended 2026-07-22 — Part 1 premise corrected during build

## Amendment (build finding)
The Part 1 premise was wrong. CC 2.1 PostCompact is side-effects-only — its output is discarded, it cannot inject context (docs: "No decision control"). The real post-compaction injection point is SessionStart firing with source "compact", whose output IS injected — and pakka's SessionStart matcher ".*" already matches it, so the full discipline chain (ruleset + verification + skill-check) re-injects after every compaction today. Revised Part 1 scope: no PostCompact registration (would be a silent no-op); instead a regression test pins the SessionStart matcher's coverage of source "compact" so the mechanism cannot be narrowed unnoticed. AC1-AC2 replaced accordingly. Found by the builder during implementation, verified independently against code.claude.com/docs/en/hooks.md.

## Problem
Pakka injects its full discipline block (compression ruleset, verification requirement, skill-check triggers) once at SessionStart. When Claude Code compacts a long session, that block survives only if the summarizer happens to preserve it — the ruleset itself pleads "No revert after many turns," which is prompt-hoping, the exact failure mode pakka tells users hooks eliminate. [Amended: survival is in fact mechanical today via SessionStart source "compact" — see Amendment; the deliverable is the pinning test, not new registration.] Separately, `pakka-core spec-generate` writes docs/specs relative to process CWD with no repo validation — today it silently wrote a spec into the wrong sibling repo when the shell CWD drifted.

## User stories
- As a pakka user with long sessions, I want disciplines re-injected after compaction so that verification and compression behavior do not silently degrade mid-session.
- As a spec author, I want spec-generate to refuse ambiguous locations so that specs land in the repo they belong to.

## Module decisions
- Register PostCompact in hooks/hooks.json invoking the existing SessionStart injector (compress-start.js) — one injection path, no duplicated ruleset text.
- PreCompact intentionally NOT used: preservation hints to the summarizer are prompt-hoping; PostCompact re-injection is the mechanical guarantee.
- Kill-switch parity: PAKKA_DISABLED=1 no-ops PostCompact like every other hook.
- spec-generate: resolve git toplevel of CWD; no repo → nonzero exit, no write; write target is <toplevel>/docs/specs/. Optional --repo-root override.

## Acceptance criteria
1. hooks.json registers PostCompact; running compress-start.js with a PostCompact event payload emits the same discipline block as a SessionStart run (byte-identical ruleset content).
2. PAKKA_DISABLED=1 PostCompact run emits nothing (exit 0, empty output), matching existing kill-switch tests.
3. JS hook tests cover 1-2 and pass (existing hooks test runner exit 0).
4. spec-generate outside any git repo: nonzero exit, error message, no file created.
5. spec-generate from a repo SUBDIRECTORY: file lands in <git-toplevel>/docs/specs/, not CWD-relative.
6. spec-generate --repo-root <path>: writes to <path>/docs/specs; nonexistent or non-repo path → nonzero exit.
7. go test ./... exit 0 and hooks JS tests exit 0.

## Out of scope
- PreCompact summarizer hints.
- prompt/agent hook types, subagentStatusLine (separate v0.16 candidates).
- Re-injection of stack overlays or setup wizard state.

## Open questions
