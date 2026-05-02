---
name: map
description: Draw the map before navigating. Surfaces all relevant modules, callers, and dependencies in one view using the project's domain vocabulary — so the next move costs one file, not ten. Use when entering unfamiliar code or needing to see where a module sits in the whole system.
allowed-tools: Read, Glob, Grep, Agent
argument-hint: "[area or module to map]"
user-invocable: false
---

## Thesis

Navigating without a map wastes context. Every wrong turn is tokens on wrong files. Map first, dive second.

Go up a layer of abstraction. Map all relevant modules and callers. Use domain vocabulary from `CONTEXT.md` if it exists. Show how the area connects to the rest of the system.

**Format:** table or nested list. One line per module. Include: what it does, who calls it, what it calls.

## Red Flags

- Reading deeply into implementation before mapping the surface → wrong. Map first, dive second.
- Using internal file names or class names instead of domain vocabulary → wrong. Use CONTEXT.md terms where they exist.
- Mapping only the target module without its callers and dependencies → incomplete. The connections are the point.
