---
description: Move issues through the triage state machine — classify, reproduce bugs, write agent briefs, manage issue workflow.
allowed-tools: Skill
argument-hint: "[issue number, or blank to show queue]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka:triage"`. Pass any user arguments through verbatim via the `args` parameter.

## Red Flags

- Parsing or rewriting args at this layer → wrong. Pass verbatim; the skill owns all logic.
- Running triage logic here instead of delegating → wrong. This is a thin wrapper only.
