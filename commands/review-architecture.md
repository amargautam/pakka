---
description: Review codebase architecture — find shallow modules, propose refactors to improve testability and reduce bug surface.
allowed-tools: Skill
argument-hint: "[area to explore, or blank for full scan]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka:review-architecture"`. Pass any user arguments through verbatim via the `args` parameter.

## Red Flags

- Parsing or rewriting args at this layer → wrong. Pass verbatim; the skill owns all logic.
- Running architecture analysis here instead of delegating → wrong. This is a thin wrapper only.
