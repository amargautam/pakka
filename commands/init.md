---
description: One-time pakka setup. Detects stack, writes stack overlay, verifies permissions and hooks work.
allowed-tools: Skill
argument-hint: "[--force]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka:init"`. If the user supplied arguments, pass them through verbatim via the skill's `args` parameter. Do not parse, rewrite, or interpret args at this layer.

This command is a thin wrapper. The skill owns all behavior (stack detection, settings overlay merge, verification, summary output).

## Red Flags

- Parsing or rewriting `$ARGUMENTS` before handing to the skill → wrong. Pass through verbatim.
- Running setup logic here instead of delegating → wrong. This file is a wrapper only.
- Invoking a different skill name (e.g. `init`) → wrong. The skill is registered as `pakka:init`.
