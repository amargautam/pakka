---
description: Self-grilling session — interview the user relentlessly about their own plan until every branch of the design tree is resolved.
allowed-tools: Skill
argument-hint: "[plan or design to probe]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka-probe"`. Pass any user arguments through verbatim via the `args` parameter.

## Red Flags

- Parsing or rewriting args at this layer → wrong. Pass verbatim; the skill owns all logic.
- Running probe logic here instead of delegating → wrong. This is a thin wrapper only.
