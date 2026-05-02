---
description: Self-grilling session — interview the user relentlessly about their own plan until every branch of the design tree is resolved.
allowed-tools: Read, Write, Bash, Agent
argument-hint: "[plan or design to probe]"
---

## Instructions

Read `${CLAUDE_PLUGIN_ROOT}/skills/pakka-probe/SKILL.md` and follow those instructions. Pass any user arguments verbatim.

## Red Flags

- Invoking the Skill tool → causes infinite loop. Read the SKILL.md file directly instead.
- Parsing or rewriting args → wrong. Pass verbatim to the skill instructions.
