---
description: Synthesize a PRD from current conversation context and publish it to the issue tracker — spec before code.
allowed-tools: Skill
argument-hint: "[optional: additional context or constraints]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka-spec"`. Pass any user arguments through verbatim via the `args` parameter.

## Red Flags

- Parsing or rewriting args at this layer → wrong. Pass verbatim; the skill owns all logic.
- Running spec synthesis here instead of delegating → wrong. This is a thin wrapper only.
