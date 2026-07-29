# pakka

Claude Code harness — fewer bugs, audit-ready, fewer tokens. Apache-2.0.

**What carries the value:** review gates, secrets guard, audit trail, cross-session recall — enforcement no prompt paragraph can do. Compression is the cost bonus, not the pitch. If your traffic caches heavily (Bedrock/enterprise), input-side token savings approach zero — that's why input compression is off by default and the meter prices savings at your real cache mix. Output compression still pays everywhere: output tokens are never cached and cost 5× input.

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
| `/pakka:review` | "done?", "ship?", "they said...", "merge?" | Quality hub. Verifies first (exit codes), then runs reviewer + security + architect + performance agents in parallel. Handles incoming feedback and branch landing. |
| `/pakka:triage` | "triage", "look at issue #N", "what needs attention" | Issue queue. Routes bugs and features through classification state machine. Produces agent-ready briefs. |
| `/pakka:setup` | one-time setup | Detects stack, writes permissions overlay. `setup guard` installs git guard hook. |
| `/pakka:compress` | — | Compression control. `[lite\|strict\|ultra\|super-ultra\|status]`. Default: `super-ultra`. Hook-handled — instant, no LLM round-trip. |
| `/pakka:recall [query]` | — | search audit trail across sessions via FTS5 index |
| `/pakka:help` | — | Show pakka status — active level, gate config, hooks. |

### Ambient disciplines (always active, no invocation needed)

**Verification:** before any "done", "working", "passing" claim — pakka requires actual exit-code evidence. Injected at session start.

**Skill-check:** before each response — pakka checks whether the message calls for `/pakka:plan`, `/pakka:build`, or `/pakka:review`. Catches cases that explicit invocation misses.

**4-vector compression:** output tokens · input context · tool results · subagent returns — all compressed independently. Input-context auto-compression is **opt-in, default off** — it rewrites version-controlled context files and sends them to your model provider for near-zero token gain. Enable with env `PAKKA_INPUT_COMPRESS=1` or `pakka.compress.input: true` in `settings.json`; the other three vectors are always on. Manual `/pakka:compress` re-compression is unaffected by the gate.

**Review gate:** reviewer + security + architect + performance subagents run in parallel on every Claude-authored commit. Confidence threshold ≥ 80. Blocks on `severity=error` findings.

**Deny-by-default permissions:** secrets, destructive git, shell-fetched-then-executed commands blocked at the permission layer. Guard covers secret reads **and writes** — the hook matches `Read|Write|Edit|MultiEdit|Bash` (was `Read|Bash` before v0.11.0). Repeated overrides teach a per-repo allowlist (`.pakka/guard-allowlist.json`) with override-count decay; secret categories are never allowlistable.

**Audit trail:** every tool call appended to `~/.pakka/audit/<session>.jsonl`. No telemetry to pakka — nothing is sent to us, ever. One egress path exists: semantic input compression rewrites context files by calling **your configured model provider** — the same provider your session already sends full context to. Disable with `PAKKA_DISABLED=1`, or undo with `/pakka:compress restore`, if that matters in your environment.

**recall:** `/pakka:recall` searches your audit trail. cross-session memory backed by local FTS5 index (SQLite). no remote storage.

**skill-check:** `UserPromptSubmit` hook keyword-scans every message. if a build/plan/review signal matches, targeted alert fires before the model responds. no more relying on model memory.

**Status line:** `pakka [super-ultra] · ~$24.74 saved (est) · 21 bugs caught` — compression level, token savings, and bugs caught, always visible. Savings priced per model — Fable 5, Mythos 5, and Opus 4.x covered; dated model IDs resolve by prefix. Input-side savings are priced **cache-aware** — a blended fresh/cache-write/cache-read rate from session telemetry, not the flat fresh-input rate (which overstated cached environments ~10x); falls back to the flat rate when telemetry is absent.

**Kill-switch:** `PAKKA_DISABLED=1` turns off every pakka hook for the session. Used by `make bench` to isolate the raw arm; works anywhere.

**Benchmarks:** `make bench` A/Bs pakka vs raw Claude Code via `claude -p` on your existing OAuth session. No API key.

## Results (v0.20.0)

Three absolute numbers, each verifiable from artifacts in this repo.

1. **Reviewer calibration: recall 1.00, precision 0.75.** The four live review agents, run headless over the seeded-bug corpus (13 bug seeds + 3 clean fixtures, zero infrastructure errors), caught 13/13 planted bugs at 0.67 false positives per clean run. Reproduce: `make calibrate` (Claude Code OAuth only — no API-key path). Artifact: `benchmarks/results/calibration-2026-07-29.json`.

2. **Bytes saved: 484,644 cumulative** since 2026-04-24. Estimated tokens: 138,922 (sum of per-event bytes ÷ 3.5). Total estimated savings: ~$24.74. Source: `RECEIPTS.md`, regenerated via `make self-report`.

3. **Gate enforcement: every Claude-authored commit path.** Architectural claim — gate runs and blocks on findings. Verify: `git log --format='%H' | while read sha; do git show -s --format='%(trailers:key=Reviewed-by-pakka,valueonly=true)' "$sha" | grep -q . && echo "$sha"; done | wc -l`.

## Attribution

Every commit pakka reviews carries:

```
Reviewed-by-pakka: v0.5.0 (gate: passed)
Co-authored-by: pakka <279024857+pakka-bot@users.noreply.github.com>
```

Opt out: `pakka.signature: false` or `pakka.coAuthor: false` in `settings.json`.

## Sponsor

pakka is built and maintained by [Amar Gautam](https://amargautam.com). If it saves you tokens or catches bugs before they ship, [sponsor its development](https://github.com/sponsors/amargautam).

## Development

Built using pakka. See [`DESIGN.md`](./DESIGN.md) and [`CLAUDE.md`](./CLAUDE.md).

## License

Apache-2.0. See [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE).
