---
description: Install a PreToolUse hook that blocks dangerous git commands before Claude executes them.
allowed-tools: Skill
argument-hint: "[--project | --global]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka-guard"`. Pass any user arguments through verbatim via the `args` parameter.

## Red Flags

- Parsing or rewriting args at this layer → wrong. Pass verbatim; the skill owns all logic.
- Running hook installation here instead of delegating → wrong. This is a thin wrapper only.
