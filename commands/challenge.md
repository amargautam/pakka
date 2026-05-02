---
description: Stress-test a plan against the project's domain model — challenge assumptions, sharpen terminology, update CONTEXT.md inline.
allowed-tools: Read, Write, Bash, Agent
argument-hint: "[plan or topic to stress-test]"
---

## Instructions

Read `${CLAUDE_PLUGIN_ROOT}/skills/pakka-challenge/SKILL.md` and follow those instructions. Pass any user arguments verbatim.

## Red Flags

- Invoking the Skill tool → causes infinite loop. Read the SKILL.md file directly instead.
- Parsing or rewriting args → wrong. Pass verbatim to the skill instructions.
