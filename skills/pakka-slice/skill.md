---
name: pakka-slice
description: Decompose a plan into thin vertical slices — each cutting through every layer end-to-end, independently runnable and self-certifiable. Horizontal slices block each other and hide progress; vertical slices ship. Use when breaking down a spec into issues for parallel or sequential AFK execution.
allowed-tools: Bash, Read, Glob, Grep, Agent
argument-hint: "[issue number, URL, or plan description]"
user-invocable: true
---

## Thesis

Horizontal slices (all schema → all API → all UI) block each other, produce brittle tickets, make progress invisible. Vertical slices — thin cuts through ALL layers end-to-end — are independently runnable, independently demoable, independently reviewable. Context waste drops because each slice is self-contained.

## Process

### 1. Gather context

Work from what's already in the conversation. If user passes an issue reference, fetch and read body + all comments.

### 2. Explore codebase (if not already done)

Issue titles and descriptions must use project's domain vocabulary (`CONTEXT.md`). Respect existing ADRs.

### 3. Draft vertical slices

Each slice cuts through ALL integration layers end-to-end. Not a layer, a slice.

Slices are either:
- **AFK** — implementable and mergeable without human interaction; acceptance criteria clear enough for agent to verify independently
- **HITL** — requires human involvement (architectural decision, design review, external access, manual testing)

Prefer AFK. Make acceptance criteria explicit enough that an agent can self-certify completion.

Rules:
- Each slice delivers narrow but COMPLETE path through every layer (schema, API, business logic, tests)
- Completed slice is independently demoable or verifiable
- Prefer many thin slices over few thick ones

### 4. Quiz the user

Present proposed breakdown as numbered list. Per slice:
- **Title** — short, domain vocabulary
- **Type** — AFK / HITL
- **Blocked by** — which slices must complete first (or "none")
- **Scope** — one line: end-to-end behavior

Ask:
- Granularity right? (too coarse / too fine)
- Dependencies correct?
- HITL/AFK designations right?

Iterate until approved.

### 5. Publish issues

Publish in dependency order (blockers first) so you can reference real issue identifiers in "Blocked by" fields. Apply `needs-triage` label so each enters normal triage flow.

```markdown
## Parent

[Link to parent issue, if source was an existing issue]

## What to build

One paragraph: end-to-end behavior of this slice. Not layer-by-layer implementation.

## Acceptance criteria

- [ ] Criterion 1
- [ ] Criterion 2

## Blocked by

[Issue reference] or "None — can start immediately."
```

Do NOT close or modify any parent issue.

## Red Flags

- Slices that only touch one layer (schema-only, UI-only, API-only) → horizontal slicing. Reject and re-slice.
- AFK slice with vague acceptance criteria → agent cannot verify completion. Sharpen before publishing.
- Publishing without user approval → wrong. Quiz first, always.
- Issues using implementation jargon instead of domain vocabulary → wrong. Use CONTEXT.md terms.
- HITL slice where human need isn't explicit → clarify before publishing.
