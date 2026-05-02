---
name: audit-code-arch
description: Audit code architecture — find shallow modules, propose refactors to improve testability and reduce bug surface. Use when understanding one concept requires reading ten files, testability is low, or coupling is making changes expensive.
allowed-tools: Read, Glob, Grep, Agent
argument-hint: "[area to explore, or blank for full scan]"
user-invocable: false
---

## Thesis

Shallow modules hide nothing — callers do all the work, bugs spread across all of them. Deep modules concentrate behavior behind small interfaces. As depth grows, context waste shrinks: fewer files to read, one place to fix, one seam to test.

Use vocabulary in [LANGUAGE.md](LANGUAGE.md) exactly. Don't drift into "service," "component," or "boundary."

## Process

### 1. Read domain context first

Look for `CONTEXT.md` (domain glossary) and `docs/adr/` (decisions already made). These tell you which seams exist, which vocabulary to use, and which refactors have been rejected — don't re-litigate closed decisions.

### 2. Explore

Use Agent tool with `subagent_type=Explore` to walk the codebase. Note friction:

- Understanding one concept requires reading five modules?
- **Interface** nearly as complex as **implementation** (shallow)?
- Extracted for testability, but bugs hide in composition?
- Untested or hard to test through current interface?

Apply **deletion test** to anything suspect: delete the module mentally. Complexity vanishes → pass-through. Complexity reappears across N callers → earning its keep.

### 3. Present candidates

Numbered list. Per candidate:

- **Modules** — which modules are involved
- **Problem** — friction in plain terms
- **Solution** — what changes, in plain terms
- **Benefits** — locality (one place to fix), leverage (N callers benefit), testability (how tests improve)

Use `CONTEXT.md` vocabulary for domain terms. Use [LANGUAGE.md](LANGUAGE.md) for architecture terms.

ADR conflicts: surface only when friction is real enough to reopen the decision. Mark clearly: _"contradicts ADR-0007 — worth reopening because…"_

Do NOT propose interfaces yet. Ask: "Which of these would you like to explore?"

### 4. Design loop

Once user picks a candidate, enter deep conversation. Walk the design tree:

- Where does the seam live?
- What sits behind it?
- What tests survive the refactor?
- What are failure modes at the new interface?

Side effects as decisions crystallize:
- New term not in `CONTEXT.md` → add it inline, same discipline as `/pakka:challenge`
- User rejects candidate with load-bearing reason → offer ADR so next review doesn't re-suggest it

## Red Flags

- Proposing interfaces before user selects a candidate → wrong. Present candidates first.
- Using "service," "component," "boundary" instead of module/seam/interface → vocabulary drift kills precision.
- Suggesting a refactor that contradicts an ADR without flagging the conflict → wrong.
- Deletion test not applied → shallow detection is unreliable without it.
- Naming a deepened module with implementation jargon (XyzHandler, FooProcessor) instead of domain vocabulary → wrong. Use CONTEXT.md terms.
- One adapter at a seam → hypothetical seam, not real. Don't design for it yet.
