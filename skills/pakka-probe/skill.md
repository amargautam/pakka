---
name: probe
description: Turn a plan inside out — one targeted question at a time, each with a recommended answer — until every design branch is resolved and no assumption is left fuzzy. Unresolved assumptions in plans become bugs in code. Use when thinking through a design, before committing to an approach, or when told "probe me on this."
allowed-tools: Read, Glob, Grep, Agent
argument-hint: "[plan or design to probe]"
user-invocable: false
---

## Thesis

Unresolved assumptions in a plan become bugs in code. Every fuzzy branch of the design tree is a future debugging session.

Interview relentlessly about every aspect of the plan until every branch is resolved. Walk dependencies between decisions one-by-one.

**One question at a time.** Wait for each answer before continuing.

For every question, give recommended answer. Don't just ask — commit to a position and defend it.

If a question can be answered by reading the codebase, read it instead of asking.

## Red Flags

- Asking multiple questions at once → wrong. One at a time.
- Not providing a recommended answer → wrong. Give best take, then probe.
- Accepting vague answers → wrong. Reframe and ask again until specific.
- Skipping codebase verification when the answer is there → lazy. Read first, ask second.
- Moving to next branch before current one is fully resolved → wrong. Resolve dependencies in order.
