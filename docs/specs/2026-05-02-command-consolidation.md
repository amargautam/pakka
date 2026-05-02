# Spec: Command Consolidation + Discipline Gaps
**Date:** 2026-05-02
**Status:** approved
**Ships in:** v0.2.0

---

## Problem

14 commands create cognitive overhead for builders. Many are thin wrappers around one behavior. Trigger phrases miss novel phrasings. Three discipline gaps exist: no pre-completion verification, no persistent spec files, no ambient skill-check discipline.

---

## Decision

Consolidate 14 commands → 5 discipline hubs. Add 3 ambient behaviors via hooks. No new commands for the gaps — they run automatically.

---

## 5 Commands

### `/pakka:plan`

**Purpose:** Everything before a line of code is written.

**Context inference (auto-route, no user decision):**

| Signal in message | Behavior |
|---|---|
| "build X", "implement", "add feature", "design" | full design session → spec file |
| "challenge this", "poke holes", "stress test" | challenge plan against domain model |
| "what am I missing", "probe me", "question my design" | probe until all branches resolved |
| "break into tickets", "slice this", "create issues" | decompose spec into vertical slices |

**Output:** `docs/specs/YYYY-MM-DD-<kebab-name>.md` — always written to disk. Never in-session only.

**Spec file format:**
```
# <Feature name>
Date: YYYY-MM-DD
Status: draft | approved | superseded

## Problem
## User stories
## Module decisions
## Acceptance criteria (testable, verifiable)
## Out of scope
## Open questions
```

**End state:** "Spec written to `docs/specs/YYYY-MM-DD-name.md`. Review it, then run `/pakka:build` when ready." Stop. Do not auto-chain to build.

---

### `/pakka:build`

**Purpose:** Disciplined implementation with spec as ground truth.

**Context inference:**

| Signal | Behavior |
|---|---|
| "write tests", "TDD", "test first" | one failing test → minimal code → repeat |
| "broken", "failing", "debug", "fix this bug" | deterministic feedback loop first; reproduce → hypothesize → instrument → fix → regression test |
| "how does X work", "explain this module", "I don't know this code" | map relevant modules and callers before touching anything |
| "coupling", "hard to test", "architecture", "refactor" | audit code architecture; find shallow modules |
| no clear signal | ask once: "What are we building?" |

**Spec approval gate (runs before any code):**
1. Scan `docs/specs/` — match by topic against current task description
2. If match found: show `filename + first heading + age` → ask "Use this spec? [y/n]"
3. If yes: load spec; acceptance criteria are hard constraints throughout
4. If no or no match: ask "Should I run `/pakka:plan` first?" — do not silently proceed without spec

**Verification gate (runs before claiming done):**
- Execute relevant tests/lint/build commands and capture actual exit codes
- Never output "done", "working", "passing" without exit code evidence
- If any exit code ≠ 0: fix before claiming completion

---

### `/pakka:review`

**Purpose:** Quality gate from implementation to ship.

**Context inference:**

| Signal | Behavior |
|---|---|
| No signal / "done?", "ship?", "PR?" | verify first (exit codes) → review staged diff with reviewer + security agents |
| "they said...", "feedback says...", "reviewer commented" | receive-review mode: technical rigor, no performative agreement, push back with evidence when warranted |
| "merge?", "land this?", "finish branch?" | branch-finish: 4 options — merge locally / push + PR / keep branch / discard |

**Reviewer gate:** confidence ≥ 80 to surface findings. Blocks on `severity=error`. Unchanged from v0.1.x.

**Verification is mandatory** before any review or ship claim. Not optional, not skippable.

---

### `/pakka:triage`

**Purpose:** Issue queue management. Standalone cadence — different rhythm from other commands.

Unchanged from current `/pakka:triage`. Classify → reproduce → spec → agent-ready brief.

---

### `/pakka:setup`

**Purpose:** One-time environment setup.

**Routing by arg:**

| Arg | Behavior |
|---|---|
| none | run init — stack detection + permissions overlay |
| `guard` | install PreToolUse hook blocking destructive git ops |

Replaces: `init`, `guard`.

---

### `/pakka:compress`

**Purpose:** Output compression control. Operational, high-frequency.

Unchanged from current implementation. Hook pre-handles level switches — no LLM tool calls needed.

---

### `/pakka:help`

**Purpose:** Discovery and status display.

Shows: active compression level, gate config, hooks active, available commands.

Replaces: `help`.

---

## 3 Ambient Behaviors (hooks, not commands)

### A. Skill-check discipline (UserPromptSubmit)

Injected into per-turn reinforcement alongside compression rules:

> Before responding: does this message call for `/pakka:plan` (design/spec), `/pakka:build` (implementation), or `/pakka:review` (quality/ship)? If yes, invoke before anything else.

Model self-checks every turn. Catches cases that trigger phrases miss.

### B. Verification rule (SessionStart + UserPromptSubmit)

Injected into session context:

> Before claiming done, working, passing, or fixed: run the relevant command and show the actual exit code. "Should work" is not evidence. Exit 0 is evidence.

Always-on. Not a command. Not skippable.

### C. Compression (existing, unchanged)

`super-ultra` default. Semantic auto-on at `ultra` and `super-ultra`. Injected at SessionStart, reinforced per-turn.

---

## Migration from 14 → 5

| Old command | Maps to |
|---|---|
| `/pakka:spec` | `/pakka:plan` (spec context) |
| `/pakka:probe` | `/pakka:plan` (probe context) |
| `/pakka:challenge` | `/pakka:plan` (challenge context) |
| `/pakka:slice` | `/pakka:plan` (slice context) |
| `/pakka:tdd` | `/pakka:build` (tdd context) |
| `/pakka:debug` | `/pakka:build` (debug context) |
| `/pakka:map` | `/pakka:build` (map context) |
| `/pakka:audit-code-arch` | `/pakka:build` (audit context) |
| `/pakka:review` | `/pakka:review` (unchanged, gains verify + receive + branch-finish) |
| `/pakka:triage` | `/pakka:triage` (unchanged) |
| `/pakka:init` | `/pakka:setup` (no arg) |
| `/pakka:guard` | `/pakka:setup guard` |
| `/pakka:compress` | `/pakka:compress` (unchanged) |
| `/pakka:help` | `/pakka:help` (unchanged) |

Old commands remain as aliases through v0.2.0. Removed in v0.3.0.

---

## Acceptance criteria

- [ ] `/pakka:plan` infers routing from message; writes spec to `docs/specs/`; does not auto-chain to build
- [ ] `/pakka:build` scans specs, requires explicit approval before loading; verification gate blocks false "done" claims
- [ ] `/pakka:review` runs verification before any review; handles receive-review and branch-finish contexts
- [ ] Skill-check rule injected per-turn; verification rule injected at SessionStart
- [ ] Old 14 commands work as aliases
- [ ] `docs/specs/` created by first `/pakka:plan` invocation; files persist in git

## Out of scope

- Recall / FTS over audit trail (Pass 6 — separate spec)
- vs-raw benchmark (deferred to v0.2.0 with API key budget)
- Fat main.go extraction (needs separate spec)
