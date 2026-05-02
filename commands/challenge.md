---
description: Stress-test a plan against the project's domain model — challenge assumptions, sharpen terminology, update CONTEXT.md inline.
allowed-tools: Skill
argument-hint: "[plan or topic to stress-test]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka-challenge"`. Pass any user arguments through verbatim via the `args` parameter.

## Red Flags

- Parsing or rewriting args at this layer → wrong. Pass verbatim; the skill owns all logic.
- Running grilling logic here instead of delegating → wrong. This is a thin wrapper only.
