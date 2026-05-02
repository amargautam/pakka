---
description: Map relevant modules and callers before navigating unfamiliar code — go up a layer of abstraction first.
allowed-tools: Read, Write, Bash, Agent
argument-hint: "[area or module to map]"
---

## Instructions

Read `${CLAUDE_PLUGIN_ROOT}/skills/pakka-map/SKILL.md` and follow those instructions. Pass any user arguments verbatim.

## Red Flags

- Invoking the Skill tool → causes infinite loop. Read the SKILL.md file directly instead.
- Parsing or rewriting args → wrong. Pass verbatim to the skill instructions.
