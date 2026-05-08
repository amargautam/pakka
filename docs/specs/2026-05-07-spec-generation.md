# Spec Generation
Date: 2026-05-07
Status: approved

## Problem
`/pakka:plan` sessions produce design artifacts in conversation but spec file output is inconsistent — sometimes written, sometimes stays in chat. Without a guaranteed, structured spec file at `docs/specs/`, Pass 7's `spec-find` falls back to LLM name-matching (slow, brittle). The plan→exec→review loop has no durable contract artifact from the plan phase: builder subagents start without a verifiable acceptance-criteria anchor, and the review gate cannot assert spec conformance reliably. When a spec is updated mid-build, that drift is currently invisible to the review gate.

## User stories
- As a user running `/pakka:plan`, I want a spec file written automatically at the end of the session so I don't have to copy-paste design output manually.
- As a builder subagent, I want a spec file to exist before I start implementing so acceptance criteria are unambiguous.
- As the review gate (`/pakka:review`), I want `spec-find` to resolve specs via filename convention (fast path) rather than LLM fallback.
- As a user running `/pakka:plan` on an existing topic, I want to see what changed when the spec is updated so I can confirm the revision before building.
- As the review gate (`/pakka:review`), I want to be warned when the spec changed during the build so I can flag potential scope drift.

## Module decisions
- `pakka-core spec-generate` subcommand writes the spec file — applies template, validates required sections, writes to disk. Go code required.
- Skill synthesizes content from conversation and passes it to `pakka-core spec-generate` via stdin or flags — skill does not write the file directly.
- Applies in all modes: explicit `/pakka:plan` call AND auto-triggered plan sessions. No behavioral difference based on trigger.
- Template enforced by pakka-core, not skill instructions — sections are validated before write; missing sections cause an error, not a warning.
- Naming: `YYYY-MM-DD-<descriptive-kebab-name>.md` — date-based triggers `spec-find` name-match fast path.
- Location: `<repo>/docs/specs/` — consistent with existing specs; `pakka-core spec-generate` creates dir if absent.
- `pakka-core spec-find` gets explicit guard: if filename matches `YYYY-MM-DD-*.md`, resolve via name-match only — no LLM fallback call. Unit test required.
- No auto-chain to `/pakka:build` after spec write — user reads spec first.
- Current conversational behavior (auto-trigger on signal words) unchanged — spec file is an added output artifact, not a new trigger.
- **Re-plan diff (Case 1):** if target spec file already exists, `spec-generate` checks `git ls-files --error-unmatch <path>` before overwriting. If tracked → diff via `git diff`; if untracked → in-memory diff (read old content, write new, diff strings). Diff output written to stdout. File is always overwritten.
- **Review gate drift (Case 2):** after `spec-find` resolves the spec for a reviewed commit, review gate runs `git log <merge-base>..HEAD -- <spec-path>`. If any spec-modifying commits found → emit `spec-drift` finding (warning, not gate-block). If `spec-find` returns no spec → skip silently.

## Acceptance criteria
1. After every `/pakka:plan` session that reaches the spec-write step, a file exists at `<repo>/docs/specs/YYYY-MM-DD-<kebab-name>.md`.
2. File contains all six required sections: Problem, User stories, Module decisions, Acceptance criteria, Out of scope, Open questions.
3. `pakka-core spec-find <commit-sha>` resolves the spec via name-match (not LLM fallback) for any spec written after this feature ships.
4. If spec filename matches `YYYY-MM-DD-*.md`, `spec-find` resolves it without any LLM call — enforced by a unit test that asserts zero LLM invocations on a date-prefixed filename.
5. Spec file is written to disk before the skill outputs its final confirmation line.
6. If no descriptive slug can be inferred from context, skill asks user for one slug before writing — one question, not a loop.
7. If `docs/specs/` does not exist in the target repo, `spec-generate` creates it before writing the file.
8. Skill output ends with: `Spec written to docs/specs/YYYY-MM-DD-<name>.md. Review it, then run /pakka:build when ready.` — no additional prose.
9. If target spec file already exists and is git-tracked, `spec-generate` overwrites it and outputs `git diff` to stdout before the confirmation line.
10. If target spec file already exists and is untracked, `spec-generate` overwrites it and outputs an in-memory unified diff (old vs new) to stdout before the confirmation line.
11. If `git log <merge-base>..HEAD -- <spec-path>` returns any commits, the review gate emits a `spec-drift` finding before the final verdict.
12. `spec-drift` finding includes: spec filename, number of spec-modifying commits on branch, unified diff of spec changes.
13. `spec-drift` is warning severity — does not block the gate.
14. If `spec-find` returns no spec for the reviewed commit, drift check is skipped silently (no finding, no error).

## Out of scope
- `pakka-core` generating spec content via LLM call — content comes from the skill (Claude).
- New slash commands or CLI flags.
- Changing auto-trigger behavior of `/pakka:plan`.
- Spec version numbering (v1/v2 filename suffixes) — git history is the version store.
- Different behavior between explicit and auto-triggered plan sessions.

## Open questions
None.
