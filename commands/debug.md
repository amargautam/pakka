---
description: Disciplined debug loop — build a feedback signal, reproduce, hypothesize, instrument, fix, regression-test.
allowed-tools: Skill
argument-hint: "[description of bug]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka:debug"`. Pass any user arguments through verbatim via the `args` parameter.

## Red Flags

- Parsing or rewriting args at this layer → wrong. Pass verbatim; the skill owns all logic.
- Running debug logic here instead of delegating → wrong. This is a thin wrapper only.
