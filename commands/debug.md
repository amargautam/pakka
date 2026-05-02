---
description: Disciplined debug loop — build a feedback signal, reproduce, hypothesize, instrument, fix, regression-test.
allowed-tools: Read, Write, Bash, Agent
argument-hint: "[description of bug]"
---

## Instructions

Read `${CLAUDE_PLUGIN_ROOT}/skills/pakka-debug/SKILL.md` and follow those instructions. Pass any user arguments verbatim.

## Red Flags

- Invoking the Skill tool → causes infinite loop. Read the SKILL.md file directly instead.
- Parsing or rewriting args → wrong. Pass verbatim to the skill instructions.
