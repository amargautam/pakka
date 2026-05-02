---
description: Audit code architecture — find shallow modules, propose refactors to improve testability and reduce bug surface.
allowed-tools: Read, Write, Bash, Agent
argument-hint: "[area to explore, or blank for full scan]"
---

## Instructions

Read `${CLAUDE_PLUGIN_ROOT}/skills/pakka-audit-code-arch/SKILL.md` and follow those instructions. Pass any user arguments verbatim.

## Red Flags

- Invoking the Skill tool → causes infinite loop. Read the SKILL.md file directly instead.
- Parsing or rewriting args → wrong. Pass verbatim to the skill instructions.
