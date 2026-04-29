---
description: Control pakka compression. Switch output intensity (lite|strict|ultra|super-ultra), re-compress input files, restore originals. Default level is `ultra`.
allowed-tools: Skill
argument-hint: "[lite|strict|ultra|super-ultra|restore|status]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka-compress"`. If the user supplied arguments, pass them through verbatim via the skill's `args` parameter. Do not parse or rewrite args at this layer.

This command is a thin wrapper. The skill owns all behavior (level switching, restore, status reporting, file compression).

## Red Flags

- Parsing the action argument (`lite|strict|ultra|super-ultra|restore|status`) at the command layer → wrong. Pass through verbatim; the skill dispatches.
- Running compression logic here instead of delegating → wrong. This file is a wrapper only.
- Invoking a different skill name (e.g. `compress`, `pakka:compress`) → wrong. The skill is registered as `pakka-compress`.
