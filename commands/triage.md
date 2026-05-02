---
description: Move issues through the triage state machine — classify, reproduce bugs, write agent briefs, manage issue workflow.
allowed-tools: Read, Write, Bash, Agent
argument-hint: "[issue number, or blank to show queue]"
---

## Instructions

Read `${CLAUDE_PLUGIN_ROOT}/skills/pakka-triage/SKILL.md` and follow those instructions. Pass any user arguments verbatim.

## Red Flags

- Invoking the Skill tool → causes infinite loop. Read the SKILL.md file directly instead.
- Parsing or rewriting args → wrong. Pass verbatim to the skill instructions.
