# pakka

Claude Code harness — fewer tokens, fewer bugs, audit-ready. Apache-2.0.

## Install

```
/plugin marketplace add amargautam/pakka-marketplace
/plugin install pakka@pakka-marketplace
```

Zero-config. Uses your existing Claude Code auth. No API key required.

## Upgrade

```
/plugin marketplace update
/plugin install pakka@pakka-marketplace
/reload-plugins
```

`/plugin marketplace update` must run first — it pulls the latest catalog ref. Without it, install resolves to a stale cached version.

## What it does

<a id="skills"></a>

### 10 engineering skills — auto-invoked by trigger phrase

Skills encode best practices. Pakka injects them into the session — Claude invokes automatically when it recognises the trigger. Call explicitly any time with `/pakka:<skill>`.

| Command | Triggers automatically when you say… | What it does |
|---|---|---|
| `/pakka:spec` | "build X", "implement X", "add feature" | Spec before code. Synthesizes PRD from conversation, publishes to issue tracker. Hard rule: runs before any implementation. |
| `/pakka:debug` | "debug", "fix this bug", "broken", "failing" | Builds a deterministic fail/pass feedback loop first. Reproduce → hypothesize → instrument → fix → regression test. |
| `/pakka:tdd` | "write tests", "TDD", "test first" | One failing test → minimal code → repeat. Vertical slices only. Tests verify behavior through public interfaces. |
| `/pakka:audit-code-arch` | "architecture", "coupling", "hard to test", "refactor" | Finds modules where the interface costs as much to learn as the implementation. Proposes targeted refactors. |
| `/pakka:challenge` | "challenge this", "stress test my plan", "poke holes" | Cross-examines a plan against project docs and domain vocabulary. Updates CONTEXT.md inline as decisions harden. |
| `/pakka:probe` | "probe me", "question my design", "what am I missing" | One question at a time — each with a recommended answer — until every design branch is resolved. |
| `/pakka:map` | "how does X work", "explain this module", "I don't know this code" | Maps all relevant modules and callers before navigating. Context cost: one view, not ten files. |
| `/pakka:triage` | "triage", "look at issue #N", "what needs attention" | Routes bugs and feature requests through a classification state machine. Produces agent-ready briefs. |
| `/pakka:slice` | "break into tickets", "create issues", "slice this" | Decomposes a plan into thin vertical slices — each end-to-end, independently runnable — and publishes as issues. |
| `/pakka:guard` | "protect git", "block force push" | Wires a PreToolUse hook that blocks force-push, hard-reset, and branch deletion before Claude executes them. |

### Core harness

| Command | What it does |
|---|---|
| `/pakka:review` | Run reviewer + security agents on staged diff in parallel. Confidence ≥ 80 to surface. `[--base=<ref>]` `[--install-hook]`. |
| `/pakka:init` | One-time setup. Detect stack, write permissions overlay, verify hooks. `[--force]`. |
| `/pakka:compress` | Switch output compression level. `[lite|strict|ultra|super-ultra|restore|status]`. Default: `ultra`. |
| `/pakka:help` | Show pakka status — what's on, what you can run. |

**4-vector compression:** output tokens · input context · tool results · subagent returns — all compressed independently.

**Review gate:** reviewer + security subagents run in parallel on every Claude-authored commit. Confidence threshold ≥ 80. Blocks on `severity=error` findings.

**Deny-by-default permissions:** secrets, destructive git, shell-fetched-then-executed commands blocked at the permission layer.

**Audit trail:** every tool call appended to `~/.pakka/audit/<session>.jsonl`. No dial-home.

**Status line:** `pakka [ultra]` — active compression level, always visible.

## Results (v0.1.0)

Three absolute numbers, each verifiable from artifacts in this repo.

1. **Bug catch rate: 9/10.** Combined reviewer + security agents caught 9 of 10 seeded bugs on the Pass 5b in-session corpus. vs-raw A/B deferred to v0.2.0.

2. **Bytes saved: 75,955 cumulative** since 2026-04-24. Estimated tokens: 21,763 (bytes ÷ 3.5). Source: `RECEIPTS.md`, regenerated via `make self-report`.

3. **Gate enforcement: every Claude-authored commit path.** Architectural claim — gate runs and blocks on findings. Verify: `git log --format='%H' | while read sha; do git show -s --format='%(trailers:key=Reviewed-by-pakka,valueonly=true)' "$sha" | grep -q . && echo "$sha"; done | wc -l`.

## Attribution

Every commit pakka reviews carries:

```
Reviewed-by-pakka: v0.1.0 (gate: passed)
Co-authored-by: pakka <279024857+pakka-bot@users.noreply.github.com>
```

Opt out: `pakka.signature: false` or `pakka.coAuthor: false` in `settings.json`.

## Development

Built using pakka. See [`DESIGN.md`](./DESIGN.md) and [`CLAUDE.md`](./CLAUDE.md).

## License

Apache-2.0. See [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE).
