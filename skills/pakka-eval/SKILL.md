---
name: eval
description: Run the 3-layer eval gate (static, LLM-judge, Monte Carlo) on skill/agent files. Ensures quality before commit.
allowed-tools: Read, Bash, Glob, Grep
argument-hint: "[targets...] [--layer=1|2|3] [--n=10]"
user-invocable: false
---

## Instructions

### Determine targets

- If file paths are given as arguments: use those.
- If no arguments: find all `skills/*/SKILL.md` and `agents/*.md` files in the plugin root.

### Run eval layers

Run `${CLAUDE_PLUGIN_ROOT}/bin/run eval <targets>` with any flags passed through (`--layer`, `--n`).

The binary runs three layers sequentially and exits:
- Exit 0: all layers passed.
- Exit 1: internal error.
- Exit 2: one or more layers failed.

Report the output to the user. The binary writes full results to `.pakka/eval/<timestamp>.json`.

### Interpret results

Layer results are printed as JSON lines to stderr:

```json
{"layer": 1, "target": "skills/pakka-init/SKILL.md", "passed": true, "details": "schema valid, red flags present, no banned words"}
{"layer": 2, "target": "skills/pakka-init/SKILL.md", "passed": true, "score": 85, "details": "matches description, clear instructions"}
{"layer": 3, "target": "skills/pakka-init/SKILL.md", "passed": true, "trigger_rate": 0.9, "false_positive_rate": 0.0, "cost_delta": "+2%"}
```

Summarize as a table for the user:

```
eval results:
  target                          | L1    | L2 (score) | L3 (trigger/FP/cost)
  skills/pakka-init/SKILL.md      | pass  | pass (85)  | pass (0.9/0.0/+2%)
  agents/reviewer.md              | pass  | pass (78)  | pass (0.8/0.1/-3%)
```

### Layer details

**Layer 1 — static (fast, mechanical):**
- Frontmatter schema valid (name, description, allowed-tools present).
- No banned words (see banned list in eval.go).
- Red Flags section present.
- No lines over 200 characters (excluding code blocks and URLs).

**Layer 2 — LLM judge (one call per target):**
- Only runs if `--layer` is not set or `--layer=2` or `--layer=3`.
- Prompt: "Does this skill/agent match its description? Score 0-100. Cite missing pieces."
- Pass threshold: score >= 75.

**Layer 3 — Monte Carlo (N runs per target):**
- Only runs if `--layer=3` or no `--layer` flag.
- Default N=10, configurable via `--n`.
- Requires `benchmarks/corpus.json` to exist with test prompts for each target.
- Pass: trigger rate >= 0.8, false positive rate <= 0.1, cost within +/-10% of last green run.

### Red Flags

- Running Layer 3 without a corpus file → skip Layer 3, warn user, do not fail.
- Claiming a pass when any layer actually failed → never. Report honestly.
- Running eval on files outside the plugin directory → refuse.
- Layer 2 or 3 taking more than 5 minutes per target → timeout, report partial results.
