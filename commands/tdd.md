---
description: Test-driven development via vertical slices — one test, one impl, repeat. Behavior through public interfaces only.
allowed-tools: Skill
argument-hint: "[feature or behavior to implement]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka-tdd"`. Pass any user arguments through verbatim via the `args` parameter.

## Red Flags

- Parsing or rewriting args at this layer → wrong. Pass verbatim; the skill owns all logic.
- Running TDD logic here instead of delegating → wrong. This is a thin wrapper only.
