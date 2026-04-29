<article id="md-body" class="markdown-body" contenteditable="true">

# Pakka

Claude Code harness: fewer tokens, fewer bugs, audit-ready.

## Install

```
/plugin marketplace add amargautam/pakka-marketplace
/plugin install pakka@pakka-marketplace
```

## What it does today

- **Audit trail.** Every tool call appends a structured line to `~/.pakka/audit/<session>.jsonl` (hashed input, tokens, latency, result).

- **4-vector compression.** Four compression surfaces, each independently configurable:
  - V1 Output: prompt-injected rules that train the model to emit terse output. Levels: `lite|strict|ultra|super-ultra`. Default `ultra` — pakka's brand thesis is fewer tokens. Per-turn reinforcement prevents drift.
  - V2 Input: session-start context compressed before the model sees it. Modes: `strict` (structural + linguistic) and `audit` (passthrough).
  - V3 Tool results: `Read`/`Grep`/`Bash` results over 10 KB truncated to head+tail with notice. `Edit`/`Write` and errors pass through.
  - V4 Subagent returns: structural + linguistic compression on returns over 1 KB.

- **Token meter.** Per-event token counts accumulate to `~/.pakka/meter/<session>.jsonl`.

- **Status line.** One-line per-session summary rendered via Claude Code's `statusLine` feature.

- **Deny-by-default permissions.** Secrets, destructive git, shell-fetched-then-executed commands blocked at the permission layer.

- **Review gate.** Reviewer + security subagents run in parallel on every `git commit`. Confidence threshold ≥ 80. Blocks on `severity=error` findings. `[skip pakka]` for escape hatch.

- **Stack detection.** `/pakka:init` detects project stack (Go, TypeScript, Python, Rust, Ruby), writes settings overlay with stack-specific permissions.

- **Stack gate.** PostToolUse hook runs lint on every `Edit`/`Write`. Fast feedback — the model sees lint errors and fixes before you do.

- **Eval harness.** `/pakka:eval` runs static checks on skills and agents (frontmatter, banned words, Red Flags section). `make bench` corpus scaffold ready for Pass 5.

All local. No dial-home. No dashboards.

## Commands

| Command | Purpose |
|---|---|
| `/pakka:help` | Show pakka status — what's on, what you can run. |
| `/pakka:review` | Run reviewer + security on staged diff, print verdicts. `[--base=<ref>]` `[--install-hook]`. |
| `/pakka:init` | One-time setup. Detect stack, write overlay, verify hooks. `[--force]`. |
| `/pakka:eval` | 3-layer eval gate (static, LLM-judge, Monte Carlo) on skill/agent files. `[targets...] [--layer=N] [--n=N]`. |
| `/pakka:compress` | Switch output level, restore originals, show stats. `[lite\|strict\|ultra\|super-ultra\|restore\|status]`. Default level: `ultra`. |

User-facing commands are bare (`/pakka:init`) for uniformity. Underlying skills keep the `pakka-` prefix (`pakka-init`, `pakka-eval`, `pakka-compress`) so they stay collision-safe in the global skill registry. Commands are thin wrappers — they pass `$ARGUMENTS` straight to the skill.

## Claims (v0.1.0)

Three numbers will ship on this README when v0.1.0 lands, each reproducible via `make bench`:

1. Tokens per merged PR vs raw Claude Code.

2. Bug-class catch rate on 10 seeded-bug PRs (target: pakka ≥ 8/10 vs raw Claude Code ≤ 3/10).

3. Self-verification: `make bench` reproduces both end-to-end.

No claim without a benchmark. No benchmark without a commit hash.

## Attribution

Every commit pakka reviews carries two trailers:

```
Reviewed-by-pakka: v0.1.0 (gate: passed)
Co-authored-by: pakka <279024857+pakka-bot@users.noreply.github.com>
```

`pakka-bot` is a machine account owned by the project maintainer. No repo write access. No automated actions. It exists only so GitHub renders the review as a contributor on every repo where pakka runs.

Opt out per-trailer via `pakka.signature: false` or `pakka.coAuthor: false` in `settings.json`.

Profile: https://github.com/pakka-bot

## Status

v0.1.0 in progress. See [`DESIGN.md`](./DESIGN.md) for scope, file specs, and build order.

## Development

Pakka is built using Pakka. See [`DESIGN.md`](./DESIGN.md) §10 (Build order) and [`CLAUDE.md`](./CLAUDE.md).

## License

Apache-2.0. See [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE).

</article>
