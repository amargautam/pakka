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

### 8 commands — context-inferred, discipline-driven

Pakka injects discipline into every session. Commands infer what you need from context — no mode flags, no guessing.

| Command | Infers from | What it does |
|---|---|---|
| `/pakka:plan` | "build X", "design", "challenge this", "probe me", "break into tickets" | Design hub. Writes spec to `docs/specs/`. Routes to spec · challenge · probe · slice based on context. Never auto-chains to build. |
| `/pakka:build` | "implement", "write tests", "broken", "how does X work", "hard to test" | Implementation hub. Checks for spec approval first. Routes to TDD · debug · map · audit based on context. Blocks completion claims without exit-code evidence. |
| `/pakka:review` | "done?", "ship?", "they said...", "merge?" | Quality hub. Verifies first (exit codes), then runs reviewer + security + architect agents in parallel. Handles incoming feedback and branch landing. |
| `/pakka:triage` | "triage", "look at issue #N", "what needs attention" | Issue queue. Routes bugs and features through classification state machine. Produces agent-ready briefs. |
| `/pakka:setup` | one-time setup | Detects stack, writes permissions overlay. `setup guard` installs git guard hook. |
| `/pakka:compress` | — | Compression control. `[lite\|strict\|ultra\|super-ultra\|status]`. Default: `super-ultra`. Hook-handled — instant, no LLM round-trip. |
| `/pakka:recall [query]` | — | search audit trail across sessions via FTS5 index |
| `/pakka:help` | — | Show pakka status — active level, gate config, hooks. |

### Ambient disciplines (always active, no invocation needed)

**Verification:** before any "done", "working", "passing" claim — pakka requires actual exit-code evidence. Injected at session start.

**Skill-check:** before each response — pakka checks whether the message calls for `/pakka:plan`, `/pakka:build`, or `/pakka:review`. Catches cases that explicit invocation misses.

**4-vector compression:** output tokens · input context · tool results · subagent returns — all compressed independently.

**Review gate:** reviewer + security + architect subagents run in parallel on every Claude-authored commit. Confidence threshold ≥ 80. Blocks on `severity=error` findings.

**Deny-by-default permissions:** secrets, destructive git, shell-fetched-then-executed commands blocked at the permission layer.

**Audit trail:** every tool call appended to `~/.pakka/audit/<session>.jsonl`. No dial-home.

**recall:** `/pakka:recall` searches your audit trail. cross-session memory backed by local FTS5 index (SQLite). no remote storage.

**skill-check:** `UserPromptSubmit` hook keyword-scans every message. if a build/plan/review signal matches, targeted alert fires before the model responds. no more relying on model memory.

**Status line:** `pakka [super-ultra] · ~$64.16 saved · 7 bugs caught` — compression level, token savings, and bugs caught, always visible.

## Results (v0.5.0)

Three absolute numbers, each verifiable from artifacts in this repo.

1. **Bug catch rate: 9/10.** Combined reviewer + security + architect agents caught 9 of 10 seeded bugs on the Pass 5b in-session corpus.

2. **Bytes saved: 242,664 cumulative** since 2026-04-24. Estimated tokens: 69,515 (bytes ÷ 3.5). Total estimated savings: ~$64.16. Source: `RECEIPTS.md`, regenerated via `make self-report`.

3. **Gate enforcement: every Claude-authored commit path.** Architectural claim — gate runs and blocks on findings. Verify: `git log --format='%H' | while read sha; do git show -s --format='%(trailers:key=Reviewed-by-pakka,valueonly=true)' "$sha" | grep -q . && echo "$sha"; done | wc -l`.

## Attribution

Every commit pakka reviews carries:

```
Reviewed-by-pakka: v0.5.0 (gate: passed)
Co-authored-by: pakka <279024857+pakka-bot@users.noreply.github.com>
```

Opt out: `pakka.signature: false` or `pakka.coAuthor: false` in `settings.json`.

## Development

Built using pakka. See [`DESIGN.md`](./DESIGN.md) and [`CLAUDE.md`](./CLAUDE.md).

## License

Apache-2.0. See [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE).
