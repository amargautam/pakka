---
description: Author a new pakka skill — writes skill.md with frontmatter, Red Flags section, and supporting files.
allowed-tools: Skill
argument-hint: "[skill name and purpose]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka-skill"`. Pass any user arguments through verbatim via the `args` parameter.

## Red Flags

- Parsing or rewriting args at this layer → wrong. Pass verbatim; the skill owns all logic.
- Running skill authoring logic here instead of delegating → wrong. This is a thin wrapper only.
