---
description: Map relevant modules and callers before navigating unfamiliar code — go up a layer of abstraction first.
allowed-tools: Skill
argument-hint: "[area or module to map]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka-map"`. Pass any user arguments through verbatim via the `args` parameter.

## Red Flags

- Parsing or rewriting args at this layer → wrong. Pass verbatim; the skill owns all logic.
- Running mapping logic here instead of delegating → wrong. This is a thin wrapper only.
