---
name: pakka-challenge
description: Cross-examine a plan against the project's recorded decisions and domain vocabulary. Surfaces contradictions between plan and code, locks down terminology, and updates CONTEXT.md and ADRs inline as each decision hardens. Use before writing code when the plan needs to survive contact with existing architecture.
allowed-tools: Read, Write, Edit, Glob, Grep, Agent
argument-hint: "[plan or topic to stress-test]"
user-invocable: true
---

## Thesis

Vague language in plans becomes vague code becomes bugs. If "account" means two different things in the same conversation, the implementation encodes that ambiguity. Resolve terminology before writing a line.

## Process

Interview relentlessly. **One question at a time.** Wait for each answer before continuing.

For every question, provide recommended answer based on what you know. Lead, don't just ask.

If a question can be answered by reading the codebase or existing docs, read them instead of asking.

## During the session

**Challenge terminology.** When user uses a term conflicting with `CONTEXT.md`: "Your glossary defines 'cancellation' as X, but you seem to mean Y — which is it?"

**Sharpen vague language.** When user uses overloaded terms, propose a precise canonical term. "You're saying 'account' — do you mean Customer or User? Those are distinct in this codebase."

**Cross-reference the code.** When user states how something works, verify against code. Contradiction → surface it: "Your code cancels entire Orders, but you just said partial cancellation is possible — which is right?"

**Stress-test with scenarios.** When domain relationships come up, invent edge cases that force precision. "What happens if the Order is partially fulfilled when the cancellation arrives?"

**Update CONTEXT.md inline.** When a term resolves, update `CONTEXT.md` immediately — don't batch. Use format in [CONTEXT-FORMAT.md](CONTEXT-FORMAT.md). Create file lazily if it doesn't exist.

Only include terms meaningful to domain experts. General programming concepts don't belong.

**Offer ADRs sparingly.** Only when all three are true:
1. Hard to reverse
2. Surprising without context — future reader would wonder "why?"
3. Real trade-off — genuine alternatives existed, one chosen for specific reasons

If any condition is missing, skip the ADR. See [ADR-FORMAT.md](ADR-FORMAT.md).

## Domain file structure

Single context (most repos):
```
/
├── CONTEXT.md
└── docs/adr/
```

Multi-context repos:
```
/
├── CONTEXT-MAP.md
└── src/
    ├── ordering/CONTEXT.md
    └── billing/CONTEXT.md
```

If `CONTEXT-MAP.md` exists, read it first to find which context applies to current topic.

## Red Flags

- Asking multiple questions at once → wrong. One question, wait for answer.
- Updating CONTEXT.md in a batch at the end → wrong. Update inline as each term resolves.
- Creating an ADR for an obvious or easily-reversible decision → ADR spam. All three conditions must hold.
- Adding general programming terms (retries, timeouts) to CONTEXT.md → wrong. Domain-specific only.
- Accepting the user's language without checking the codebase → lazy. Cross-reference first.
- Not providing a recommended answer with each question → wrong. Lead, don't just interrogate.
