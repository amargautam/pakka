---
description: Run the 3-layer eval gate (static, LLM-judge, Monte Carlo) on skill/agent files. Ensures quality before commit.
allowed-tools: Skill
argument-hint: "[targets...] [--layer=1|2|3] [--n=10]"
---

## Instructions

Invoke the Skill tool with `skill: "pakka-eval"`. If the user supplied arguments, pass them through verbatim via the skill's `args` parameter. Do not parse, rewrite, or interpret args at this layer.

This command is a thin wrapper. The skill owns all behavior (target resolution, layer execution, result summarization).

## Red Flags

- Parsing flags (`--layer`, `--n`) at the command layer → wrong. Pass through verbatim; the skill and underlying binary parse them.
- Running eval logic here instead of delegating → wrong. This file is a wrapper only.
- Invoking a different skill name (e.g. `eval`, `pakka:eval`) → wrong. The skill is registered as `pakka-eval`.
