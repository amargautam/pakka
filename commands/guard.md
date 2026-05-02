---
description: Install a PreToolUse hook that blocks dangerous git commands before Claude executes them.
allowed-tools: Read, Write, Bash, Agent
argument-hint: "[--project | --global]"
---

## Instructions

Read `${CLAUDE_PLUGIN_ROOT}/skills/pakka-guard/SKILL.md` and follow those instructions. Pass any user arguments verbatim.

## Red Flags

- Invoking the Skill tool → causes infinite loop. Read the SKILL.md file directly instead.
- Parsing or rewriting args → wrong. Pass verbatim to the skill instructions.
