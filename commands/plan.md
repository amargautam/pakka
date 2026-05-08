---
description: Design hub — spec, probe, challenge, or slice. Infers mode from context. Writes spec to docs/specs/.
allowed-tools: Read, Bash
argument-hint: "[feature description or no argument]"
---

## Instructions

### 1. Infer mode from context

Read the user's message and pick ONE mode. Do not ask which mode — infer it:

| Signal | Mode |
|--------|------|
| "build X", "implement", "add feature", "I want to", "design", "we need to" | **spec** — full design session → spec file |
| "challenge this", "poke holes", "stress test", "what's wrong with" | **challenge** — cross-examine plan against domain model |
| "what am I missing", "probe me", "question my design", "interview me" | **probe** — one question at a time until all branches resolved |
| "break into tickets", "slice this", "create issues", "decompose" | **slice** — decompose spec into vertical slices, publish as issues |

If signal is ambiguous, default to **spec**.

---

### Mode: spec

Produce a spec file. Do not write code. Do not defer to "we'll figure it out later."

**Step 1 — Gather**
Ask the minimum questions needed to fill the spec. One round only. Do not back-and-forth.

**Step 2 — Write spec file**

Synthesize the spec content from the conversation into the six-section format:

```
# <Feature name>
Date: YYYY-MM-DD
Status: draft

## Problem
One paragraph. What breaks or is missing without this.

## User stories
- As a <role>, I want <action> so that <outcome>.

## Module decisions
Key technical choices. One line each. No prose.

## Acceptance criteria
Numbered list. Each item: testable, verifiable, binary pass/fail.
No "should", "probably", "seems". Observable outcomes only.

## Out of scope
Explicit list. Prevents scope creep.

## Open questions
Anything unresolved that will block implementation. Empty if none.
```

Then write it via `pakka-core spec-generate` — do NOT use the Write tool:

```bash
printf '%s' "<spec content>" | ${CLAUDE_PLUGIN_ROOT}/bin/run spec-generate --slug <kebab-name>
```

Where `<kebab-name>` is a short descriptive slug derived from the feature name (e.g. `spec-generation`, `auth-refresh-token`).

If `CLAUDE_PLUGIN_ROOT` is not set, fall back to:
```bash
printf '%s' "<spec content>" | pakka-core spec-generate --slug <kebab-name>
```

If `spec-generate` exits non-zero: show the error and stop — do not retry or fall back to Write.

**Step 3 — End**

`pakka-core spec-generate` prints the result line. Do not add any additional output after it.

Stop. Do not auto-chain to build. Do not write any code.

---

### Mode: challenge

Cross-examine the plan in context against:
- Domain vocabulary (are terms used consistently?)
- Recorded decisions in `memory/DECISIONS.md` (does this contradict anything?)
- Existing code (does the plan assume interfaces that don't exist?)

For each contradiction: state the conflict, cite the source, propose resolution.
Update `memory/DECISIONS.md` inline as decisions harden.

---

### Mode: probe

One targeted question per turn. Each question must have a recommended answer.
Continue until every branch is resolved and no assumption is left fuzzy.
Do not ask multiple questions at once. Do not summarize between questions.

---

### Mode: slice

Read `docs/specs/` — find the most relevant spec for the current task (match by topic, confirm with user before loading).
Decompose into vertical slices: each slice cuts through every layer end-to-end, is independently runnable, and has verifiable acceptance criteria.
Publish each slice as a GitHub issue.

---

## Red Flags

- Writing code in this command → wrong. This is design only.
- Auto-chaining to `/pakka:build` after spec → wrong. User reads the spec first.
- Asking "which mode?" instead of inferring → wrong. Read the context.
- Spec acceptance criteria that say "should", "probably", "seems" → not verifiable. Rewrite as observable outcomes.
