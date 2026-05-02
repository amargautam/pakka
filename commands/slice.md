---
description: Break a plan into independent vertical slices and publish them as issues — each slice complete end-to-end.
allowed-tools: Skill
argument-hint: "[issue number, URL, or plan description]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka:slice"`. Pass any user arguments through verbatim via the `args` parameter.

## Red Flags

- Parsing or rewriting args at this layer → wrong. Pass verbatim; the skill owns all logic.
- Running slice logic here instead of delegating → wrong. This is a thin wrapper only.
