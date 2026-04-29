<article id="md-body" class="markdown-body" contenteditable="true">

# Pakka

Claude Code harness: fewer tokens, fewer bugs, audit-ready.

## Install

```
/plugin marketplace add amargautam/pakka-marketplace
/plugin install pakka@pakka-marketplace
```

Zero-config. Pakka uses your existing Claude Code authentication via `claude -p` subprocess for semantic compression. `ANTHROPIC_API_KEY` is optional — only needed as an HTTP fallback if `claude` is not on `PATH`. Resolution order: `claude` CLI → `ANTHROPIC_API_KEY` HTTP → deterministic strict (no LLM). Override via `pakka.compress.engine` (`claude-cli` | `anthropic-http` | `auto`, default `auto`).

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

## Results (v0.1.0)

Three absolute numbers. Each verifiable from artifacts in this repo. vs-raw A/B comparison is deferred to v0.2.0 (requires API-key bench budget; see `DECISIONS.md` "Bench methodology").

1. **Bug catch rate: 9/10.** Combined `pakka:reviewer` + `pakka:security` agents caught 9 of 10 seeded bugs on the Pass 5b in-session corpus (12 entries). Methodology: in-session benchmark, single skilled reviewer arm. A raw-Claude A/B is the v0.2.0 deliverable.

2. **Bytes saved (compression): 75,955 cumulative since 2026-04-24.** Estimated tokens saved: 21,763 (bytes ÷ 3.5 — estimate, not a measured token count). Source: `RECEIPTS.md`, regenerated via `make self-report`. Meter is cumulative across pakka's own development; counter resets are logged.

3. **Gate enforcement: every Claude-authored commit runs the review gate.** Verifiable trailer count on `v0.1.0-dev` today: `2` of 24 commits carry `Reviewed-by-pakka`. The discrepancy is the trailer-injection hook itself: it failed on wrapped commit shapes (`cd && git`, `git -C`) until `45af7b3` (Pass 4.5 phase 2). Historical commits before that fix have no trailer, and Pass 4.6 docs-sync (`9b838ac`) silently skipped — known in-flight diagnostic. Reproduce: `git log --format='%H %s%n%b' v0.1.0-dev | grep -c Reviewed-by-pakka`.

No claim without a check. No check without a path to the artifact.

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
