---
description: One-time pakka setup. Detects stack, writes stack overlay, verifies permissions and hooks work.
allowed-tools: Read, Write, Bash, Agent
argument-hint: "[--force]"
---

## Instructions

Read `${CLAUDE_PLUGIN_ROOT}/skills/pakka-init/SKILL.md` and follow those instructions. Pass any user arguments verbatim.

## Red Flags

- Invoking the Skill tool → causes infinite loop. Read the SKILL.md file directly instead.
- Parsing or rewriting args → wrong. Pass verbatim to the skill instructions.
