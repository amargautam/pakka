---
description: Break a plan into independent vertical slices and publish them as issues — each slice complete end-to-end.
allowed-tools: Read, Write, Bash, Agent
argument-hint: "[issue number, URL, or plan description]"
---

## Instructions

Read `${CLAUDE_PLUGIN_ROOT}/skills/pakka-slice/SKILL.md` and follow those instructions. Pass any user arguments verbatim.

## Red Flags

- Invoking the Skill tool → causes infinite loop. Read the SKILL.md file directly instead.
- Parsing or rewriting args → wrong. Pass verbatim to the skill instructions.
