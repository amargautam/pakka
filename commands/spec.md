---
description: Synthesize a PRD from current conversation context and publish it to the issue tracker — spec before code.
allowed-tools: Read, Write, Bash, Agent
argument-hint: "[optional: additional context or constraints]"
---

## Instructions

Read `${CLAUDE_PLUGIN_ROOT}/skills/pakka-spec/SKILL.md` and follow those instructions. Pass any user arguments verbatim.

## Red Flags

- Invoking the Skill tool → causes infinite loop. Read the SKILL.md file directly instead.
- Parsing or rewriting args → wrong. Pass verbatim to the skill instructions.
