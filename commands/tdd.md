---
description: Test-driven development via vertical slices — one test, one impl, repeat. Behavior through public interfaces only.
allowed-tools: Read, Write, Bash, Agent
argument-hint: "[feature or behavior to implement]"
---

## Instructions

Read `${CLAUDE_PLUGIN_ROOT}/skills/pakka-tdd/SKILL.md` and follow those instructions. Pass any user arguments verbatim.

## Red Flags

- Invoking the Skill tool → causes infinite loop. Read the SKILL.md file directly instead.
- Parsing or rewriting args → wrong. Pass verbatim to the skill instructions.
