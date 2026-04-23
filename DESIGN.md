# Pakka Plugin — v0 Design

Target: `github.com/amargautam/pakka` (plugin) + `github.com/amargautam/pakka-marketplace` (catalog). License: Apache-2.0. Internal code: Go. Build window: ~3–5 days over 5 long-running Claude Code passes. Drive with Claude Code. Dogfood from Pass 1.

This doc is the spec. Everything else derives from it.

---

## 1. v0 scope — three claims, each reproducible

1. **Fewer tokens.** On the v0 benchmark corpus, `pakka` uses fewer tokens per merged PR than raw Claude Code.
2. **Catches a bug class raw Claude Code misses.** On 10 seeded-bug PRs, `pakka`'s review gate catches ≥ 8/10; raw Claude Code catches ≤ 3/10. Numbers published.
3. **Self-verifiable.** `make bench` reproduces both claims end-to-end on any Mac/Linux host with Claude Code installed.

No claim without a benchmark. No benchmark without a commit hash. Three numbers on the README. That's the release bar.

---

## 2. Repos

**`amargautam/pakka`** — the plugin. Apache-2.0. Public from commit one.
**`amargautam/pakka-marketplace`** — the catalog. Apache-2.0. Public. Two files only: `.claude-plugin/marketplace.json`, `README.md`.

User install flow:
```
/plugin marketplace add amargautam/pakka-marketplace
/plugin install pakka@pakka-marketplace
```
Dev install (local path, for dogfooding):
```
/plugin install ./path/to/pakka
```

---

## 3. Plugin repo layout (`pakka/`)

```
pakka/
├── .claude-plugin/
│   └── plugin.json
├── skills/
│   ├── pakka-init/SKILL.md
│   ├── pakka-compress/SKILL.md
│   └── pakka-eval/SKILL.md
├── agents/
│   ├── reviewer.md
│   └── security.md
├── commands/
│   ├── pakka-review.md
│   └── pakka-status.md
├── hooks/
│   └── hooks.json
├── settings.json
├── cmd/
│   └── pakka-core/        # Go binary, one entrypoint, sub-commands
│       └── main.go
├── internal/              # Go packages (compressor, meter, audit, eval, etc.)
├── benchmarks/
│   ├── corpus.json        # 30 real PRs + 10 seeded bugs
│   ├── seeds/             # bug-seed patches
│   └── README.md
├── bin/                   # prebuilt binaries, one per arch, committed at release
│   ├── pakka-core-darwin-arm64
│   ├── pakka-core-darwin-amd64
│   ├── pakka-core-linux-arm64
│   ├── pakka-core-linux-amd64
│   └── pakka-core-windows-amd64.exe
├── Makefile
├── LICENSE              # Apache-2.0
├── NOTICE
├── CLAUDE.md            # house rules for pakka's own dev
└── README.md
```

**Convention:** every script invoked by a hook is `bin/pakka-core <subcommand> [args]`. One binary, many subcommands — `compress`, `meter`, `audit`, `guard`, `eval`, `stack-detect`, `status-line`. Users install nothing; the right binary is selected via Claude Code's `${CLAUDE_PLUGIN_DIR}` + OS/arch at hook invocation.

---

## 4. Components — what / where / why

| # | Component | Files | Packaging | Why |
|---|---|---|---|---|
| 1 | Wizard | `skills/pakka-init/SKILL.md` | Skill | Interactive, progressive disclosure, writes config. |
| 2 | Deny-by-default permissions | `settings.json` | Config | Declarative, mergeable. Zero runtime cost. |
| 3 | Context compressor | `hooks.json` (SessionStart, SubagentStop) + `skills/pakka-compress/SKILL.md` | Hook + skill | Deterministic → hook. Skill exposes `strict\|fast\|audit` modes. |
| 4 | Secrets guard | `hooks.json` (PreToolUse on Read/Bash) → `pakka-core guard` | Hook | Must block before tool runs. O_NOFOLLOW reads. |
| 5 | Parallel review | `agents/reviewer.md`, `agents/security.md`, `commands/pakka-review.md` | Subagents + command | Reasoning → subagents; gate logic → command; confidence ≥ 80. |
| 6 | Stack lint/test/format | `hooks.json` (PostToolUse on Edit/Write, Stop) — overlay written by wizard | Hook | Mechanical, fail-loud, zero context cost. |
| 7 | Token meter | `hooks.json` (PostToolUse, Stop) → `pakka-core meter` | Hook | Append-only JSONL. |
| 8 | Audit trail | Same hooks, `pakka-core audit` | Hook | Same JSONL stream, structured. |
| 9 | Status line | `hooks.json` (Stop) → `pakka-core status-line` | Hook | One-line summary, on by default. |
| 10 | Eval harness | `skills/pakka-eval/SKILL.md` + `pakka-core eval` + `benchmarks/` | Skill + CLI | Runs in `claude -p` headless; CI calls it. |
| 11 | Red-Flags convention | Every `SKILL.md` and agent file | Convention | Blocks anti-patterns, not just guides. Superpowers lineage. |

Nothing not in this table ships in v0.

---

## 5. File specs

### 5.1 `plugin.json`

```json
{
  "name": "pakka",
  "description": "Claude Code harness — fewer tokens, fewer bugs, audit-ready. Apache-2.0.",
  "version": "0.1.0",
  "author": { "name": "Amar Gautam", "email": "amar@gautamfamily.com" },
  "homepage": "https://pakka.dev",
  "repository": "https://github.com/amargautam/pakka",
  "license": "Apache-2.0",
  "keywords": ["harness", "review", "audit", "token-economy"]
}
```

### 5.2 `.claude-plugin/marketplace.json` (lives in the **marketplace** repo)

```json
{
  "name": "pakka-marketplace",
  "owner": { "name": "Amar Gautam", "email": "amar@gautamfamily.com" },
  "metadata": {
    "description": "Pakka plugins for Claude Code",
    "version": "0.1.0"
  },
  "plugins": [
    {
      "name": "pakka",
      "source": { "source": "github", "repo": "amargautam/pakka" },
      "description": "Claude Code harness — fewer tokens, fewer bugs, audit-ready.",
      "license": "Apache-2.0",
      "keywords": ["harness", "review", "audit"],
      "category": "engineering"
    }
  ]
}
```

Reserved marketplace names to avoid: `claude-code-marketplace`, `anthropic-marketplace`, etc. `pakka-marketplace` is clear.

### 5.3 `settings.json` — deny-by-default baseline

```json
{
  "permissions": {
    "deny": [
      "Read(./.env*)",
      "Read(~/.ssh/**)",
      "Read(~/.aws/**)",
      "Read(~/.gnupg/**)",
      "Read(~/.netrc)",
      "Bash(git reset --hard*)",
      "Bash(git push --force*)",
      "Bash(rm -rf /*)",
      "Bash(curl * | sh*)",
      "Bash(wget * | sh*)",
      "WebFetch(domain:raw.githubusercontent.com)"
    ],
    "ask": [
      "Bash(git push*)",
      "Bash(npm publish*)",
      "Bash(cargo publish*)",
      "Bash(gh release*)",
      "WebFetch"
    ],
    "allow": [
      "Read(./**)",
      "Edit(./**)",
      "Write(./**)",
      "Bash(git status)",
      "Bash(git diff*)",
      "Bash(git log*)",
      "Bash(git branch*)",
      "Bash(git add*)",
      "Bash(git commit*)",
      "Bash(git stash*)",
      "Bash(git checkout*)"
    ]
  },
  "pakka": {
    "display": { "statusLine": true },
    "compress": { "mode": "fast" },
    "review": { "confidenceThreshold": 80 },
    "audit": { "path": "~/.pakka/audit" },
    "meter":  { "path": "~/.pakka/meter" }
  }
}
```

Wizard (`/pakka:init`) adds stack-specific allows (e.g. `Bash(go test ./...)`, `Bash(npm test)`) and stack-specific PostToolUse hooks.

### 5.4 `hooks/hooks.json`

All handlers invoke `${CLAUDE_PLUGIN_DIR}/bin/pakka-core` (resolved at runtime to the right arch binary by the wizard on install).

```json
{
  "SessionStart": [
    { "matcher": "", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_DIR}/bin/pakka-core compress --phase=session-start" }
    ]}
  ],
  "SessionEnd": [
    { "matcher": "", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_DIR}/bin/pakka-core audit --phase=session-end" }
    ]}
  ],
  "PreToolUse": [
    { "matcher": "Read|Bash", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_DIR}/bin/pakka-core guard" }
    ]}
  ],
  "PostToolUse": [
    { "matcher": "", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_DIR}/bin/pakka-core meter" },
      { "type": "command", "command": "${CLAUDE_PLUGIN_DIR}/bin/pakka-core audit --phase=tool-post" }
    ]},
    { "matcher": "Edit|Write", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_DIR}/bin/pakka-core stack-gate" }
    ]}
  ],
  "SubagentStop": [
    { "matcher": "", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_DIR}/bin/pakka-core compress --phase=subagent-return" }
    ]}
  ],
  "Stop": [
    { "matcher": "", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_DIR}/bin/pakka-core status-line" }
    ]}
  ]
}
```

Exit codes: 0 = pass, 2 = block (stderr → Claude). `pakka-core guard` is the only one that blocks. Others never block on their own errors (exit 1 on internal failure, not 2).

### 5.5 `skills/pakka-init/SKILL.md`

Frontmatter:
```yaml
---
name: pakka-init
description: One-time Pakka setup. Detects stack, writes stack overlay, verifies permissions and hooks work.
allowed-tools: Read, Write, Edit, Bash
user-invocable: true
---
```

Body responsibilities:
- Detect stack (language + toolchain) via `pakka-core stack-detect` on the cwd.
- Ask only what can't be inferred (test command, coverage target, lint command if nonstandard).
- Write `.claude/settings.local.json` stack overlay (stack-specific `allow` entries + PostToolUse `stack-gate` script path).
- Write `.pakka/stack.json` with detected facts.
- Verify: run a no-op tool call, confirm hooks fire, status line renders, audit JSONL written.
- Print three-line summary + next step (`/pakka:review`).

Red Flags section (rejects anti-patterns at run time):
- "Inferred stack but wrote config without confirming." → ask before write.
- "Overwrote user's existing `.claude/settings.local.json`." → merge, never replace.
- "Enabled network allow for wide domain." → deny, ask, or scope narrower.

### 5.6 `skills/pakka-compress/SKILL.md`

```yaml
---
name: pakka-compress
description: Compress CLAUDE.md, skill bodies, and subagent returns. Modes: strict|fast|audit.
allowed-tools: Read, Bash
argument-hint: "[mode]"
user-invocable: true
---
```

Body:
- Invokes `pakka-core compress --mode=$1` with the current context.
- Deterministic rules (no LLM calls inside compressor):
  - Strip duplicate whitespace, code-fence headers, trailing metadata.
  - Collapse repeated section headings (keep first, drop rest).
  - Keep all TODO/FIXME/SECURITY markers verbatim.
  - Keep code blocks verbatim unless clearly dead (commented-out).
- Modes:
  - `strict`: maximum compression, all non-semantic tokens removed.
  - `fast` (default): balanced, preserves readability.
  - `audit`: no compression, used when debugging eval discrepancies.
- Output: compressed text + ratio annotation (`compressed 42.1% · 8.3k → 4.8k`).

### 5.7 `skills/pakka-eval/SKILL.md`

```yaml
---
name: pakka-eval
description: Run the 3-layer eval gate (static → LLM-judge → Monte Carlo) on a proposed skill/agent change.
allowed-tools: Read, Bash
user-invocable: true
---
```

Body: invokes `pakka-core eval` with the target file(s) and writes results to `.pakka/eval/<ts>.json`. Details in §8.

### 5.8 `agents/reviewer.md`

```yaml
---
name: reviewer
description: Parallel reviewer for correctness, perf, maintainability. Returns findings with confidence 0-100.
model: sonnet
tools: Read, Bash
---
```

Body (abbreviated):
- Read the diff via `git diff --cached` (or provided range).
- For each hunk: identify risks in {logic, error handling, null/undefined, off-by-one, race, perf regression, API contract, test coverage}.
- For each finding, emit JSON line: `{"kind":"correctness","file":"...","line":123,"severity":"warn|error","confidence":0-100,"rationale":"...","fix":"..."}`
- Confidence calibration (explicit rules, Red Flags table below).
- No prose summary. JSON lines only. The command wraps and filters.

Red Flags:
- Confidence ≥ 80 on anything stylistic → lower. Style isn't a correctness bug.
- Reporting a finding without a line number → drop.
- Same finding repeated in two forms → dedupe before output.

### 5.9 `agents/security.md`

Same shape as reviewer. Focus: secrets leaks, injection (SQL, shell, path, XSS), auth bypass, unsafe deserialization, crypto misuse, SSRF, TOCTOU, permission escalation. Finding kind = `"security"`. Confidence threshold same: ≥ 80 to report.

### 5.10 `commands/pakka-review.md`

```yaml
---
description: Run reviewer + security in parallel on staged diff, filter by confidence, print grouped verdicts.
allowed-tools: Agent, Bash, Read
argument-hint: "[--base=<ref>]"
---
```

Body:
- Run `reviewer` and `security` agents in parallel over `git diff --cached` (or `--base` target).
- Collect JSON findings.
- Filter: `confidence >= $pakka.review.confidenceThreshold` (default 80).
- Group by file. Print: one line per finding, plus "proposed fix" snippet.
- Write full output to `.pakka/reviews/<commit-or-ts>.jsonl`.
- Exit 2 if any `severity=error` finding passes threshold (blocks commit when wired into a git pre-commit or CI).

### 5.11 `commands/pakka-status.md`

Prints last N status-line entries + aggregate token/savings/verdict counts for current session. Read-only. Useful for debugging.

### 5.12 Go binary — `cmd/pakka-core`

One binary. Subcommands invoked from hooks and skills:

| Subcommand | Inputs | Outputs | Notes |
|---|---|---|---|
| `compress --mode=<strict\|fast\|audit> --phase=<session-start\|subagent-return>` | stdin: text; flags | stdout: compressed text + ratio comment; JSON line to `~/.pakka/meter/<session>.jsonl` | Deterministic. No LLM. |
| `guard` | stdin: hook event JSON | exit 0 (allow), exit 2 + stderr (block). Never `deny`-decides for paths already covered by settings; this is the runtime second-line (O_NOFOLLOW symlink resolution, live `.env*` detection inside subdirs). | Cheap. Must be < 5ms p95. |
| `meter` | stdin: hook event JSON | JSONL line to `~/.pakka/meter/<session>.jsonl` | Parses usage from event; accumulates. |
| `audit --phase=<tool-post\|session-end>` | stdin: hook event JSON | JSONL line to `~/.pakka/audit/<session>.jsonl` | Structured schema (§7). |
| `stack-detect` | cwd | JSON to stdout | Looks for `go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml`, etc. |
| `stack-gate` | hook event JSON on stdin; reads `.pakka/stack.json` | Runs the stack's lint/test/format sequence; exit 2 + stderr on failure | Mechanical. Fails loud. |
| `status-line` | session id from hook event | One line to stderr (where Claude Code picks it up for display). | Format in §6. |
| `eval <targets...>` | file paths | JSON to `.pakka/eval/<ts>.json`; exit 0/1/2 by layer | 3-layer gate (§8). |

**Go code rules** (enforced via `CLAUDE.md`):
- No external deps beyond Go stdlib + `github.com/tidwall/gjson` (JSON scan) and `github.com/spf13/cobra` (CLI). Keep binary lean.
- Every subcommand has a table-driven test.
- Every public function has `// Purpose:` + `// Errors:` comments.
- No goroutines in hook-path subcommands (`guard`, `meter`, `audit`, `status-line`, `stack-gate`) — simplicity > concurrency at hook latency scale.
- Hook-path subcommands MUST return in < 10ms p95 on a cold run. Benchmark and gate.

---

## 6. Status-line format

Printed to **stderr** by `pakka-core status-line` on `Stop`:

```
pakka · 4.2k tokens used · ~8.6k saved vs baseline · 2/3 verdicts passed · audit: ~/.pakka/audit/<short-session>.jsonl
```

Parts:
- `tokens used`: sum from session meter.
- `saved vs baseline`: delta vs a baseline estimate (baseline = what the same prompts/diff would cost without pakka compression, computed from meter's tracked compression ratios).
- `verdicts passed`: count of reviewer/security runs this session that passed gate (post-threshold) over total runs.
- `audit`: relative path to the session's audit JSONL.

Togglable: `settings.json` → `"pakka.display.statusLine": false`.

---

## 7. Audit JSONL schema

One file per session at `~/.pakka/audit/<session-id>.jsonl`. Append-only. Each line:

```json
{
  "ts": "2026-04-23T18:05:12.123Z",
  "session_id": "01JXYZ...",
  "kind": "tool_use | tool_result | subagent_start | subagent_stop | review_verdict | guard_block | status",
  "tool": "Read|Edit|Write|Bash|Agent|WebFetch|mcp__*__*",
  "input_hash": "sha256:...",
  "output_size": 2048,
  "tokens": { "in": 0, "out": 0, "cache_read": 0, "cache_create": 0 },
  "latency_ms": 42,
  "result": "ok|blocked|error",
  "reason": "string, only if blocked/error",
  "redacted": true
}
```

Rules:
- Never log tool input verbatim — hash it. Inputs may contain secrets.
- `redacted: true` when input matched a secrets pattern (still hashed; content never stored).
- Schema version pinned via top-line preamble: first line of every audit file is `{"schema":"pakka.audit.v1"}`.
- Rotation: new file per `session_id`. No global growth.

---

## 8. Eval harness

`pakka-core eval <targets...>` runs three layers sequentially. Exit 2 if any layer fails.

**Layer 1 — static (fast, mechanical)**
- Frontmatter schema valid.
- No banned words (configurable: "guarantee," "100%", "revolutionary").
- Red Flags section present in every skill/agent file.
- Line-length caps, link-liveness (offline check, allowlisted domains).

**Layer 2 — LLM judge (one sonnet call per target)**
- Prompt: "Does this skill/agent match its description? Score 0-100. Cite missing pieces."
- Pass threshold: ≥ 75.

**Layer 3 — Monte Carlo (N=10 by default, configurable to 50 for release)**
- Runs `claude -p` in headless mode on 10 sampled prompts from `benchmarks/corpus.json` scoped to the target skill/agent.
- Measures: trigger rate (skill auto-invokes when it should), false-positive rate, token cost, verdict agreement.
- Pass: trigger rate ≥ 0.8, false-positive ≤ 0.1, cost within ±10% of last green run.

Output: `.pakka/eval/<ts>.json` — full results, per-layer verdicts, diffs vs previous green run. Gate: no skill change merges without layer-3 green.

---

## 9. Benchmark corpus — 40 cases

`benchmarks/corpus.json` indexes everything. Entries:

```json
{
  "id": "bench-001",
  "kind": "real-pr|seeded-bug",
  "language": "go|ts|py",
  "repo": "gin-gonic/gin",
  "pr": 3421,
  "diff": "benchmarks/diffs/bench-001.patch",
  "prompt": "benchmarks/prompts/bench-001.md",
  "expected": { "should_block": false, "findings_required": ["..."] },
  "baseline_tokens": 12400
}
```

**30 real PRs** (10 each: Go, TS, Python): small-to-medium diffs (50–500 LOC), pulled from well-maintained repos. Suggested sources:
- Go: `gin-gonic/gin`, `labstack/echo`, `hashicorp/consul`
- TS: `vercel/next.js`, `tRPC/tRPC`, `honojs/hono`
- Python: `pydantic/pydantic`, `encode/httpx`, `fastapi/fastapi`

**10 seeded-bug PRs**: clean PRs mutated with one known-bug-class each:
1. N+1 query (DB)
2. Missing null/undefined check
3. Off-by-one in slice/loop
4. TOCTOU in file access
5. SQL injection via string concat
6. Shell injection via unquoted arg
7. Secret literal committed
8. Missing error check (ignored `err`)
9. Race: shared state without lock
10. Permission escalation (sudo/chmod 777)

Each seed has an expected finding; `pakka:review` must surface it with confidence ≥ 80. Baseline: run raw Claude Code on the same prompts; record what it catches.

`make bench` executes all 40 via `claude -p`, writes `benchmarks/results/<ts>.json`, updates `README.md` claim numbers via a post-script.

### 9.1 `make self-report` — receipts from building Pakka with Pakka

Companion target to `make bench`. Reads the pakka-build's own `~/.pakka/meter/*.jsonl` + `~/.pakka/audit/*.jsonl` from every session that contributed to the repo, emits `RECEIPTS.md`:

- Session count and total wall-clock.
- Total tokens consumed across the build.
- Estimated tokens saved by the compressor (ratio × baseline).
- Count of reviewer/security verdicts run; count that passed threshold.
- Anti-patterns pakka caught in its own code (grouped by kind).
- Cases that started as "pakka missed this in itself" and became seeded-bug benchmark entries.

Implementation: `pakka-core report --format=md --since=<git-first-commit-date>`. Runs at release time. Commit `RECEIPTS.md` at the repo root. The build's own proof points live alongside the code.

---

## 10. Build order — 5 long-running passes

Driven via long-running local Claude Code sessions. Pakka runs on its own build from Pass 1 onward — the flywheel. Approve at phase boundaries, not per-commit.

### Pass 1 — skeleton + install + dogfood loop (~2h)
- `.claude-plugin/plugin.json`, `settings.json` (deny-by-default baseline).
- Marketplace repo `.claude-plugin/marketplace.json`.
- `cmd/pakka-core` scaffold; `status-line` and `audit` subcommands implemented.
- `hooks/hooks.json` wired for Stop (status line) + PostToolUse (audit).
- Prebuilt binaries committed to `bin/` (darwin arm64/amd64, linux arm64/amd64, windows amd64).
- Paste-ready README with install steps.

Kick it off:
```bash
claude --permission-mode acceptEdits \
       --allowedTools "Bash(go *),Bash(git *),Bash(make *),Read,Edit,Write" \
       "Execute Pass 1 from DESIGN.md. Stop when /plugin install works and status-line renders on a fresh session."
```
**Gate:** `/plugin marketplace add amargautam/pakka-marketplace && /plugin install pakka@pakka-marketplace` on a fresh session produces a status line. Install pakka locally. Flywheel starts.

### Pass 2 — compressor + meter (thesis claim 1) (~4h)
- `pakka-core compress` (strict/fast/audit modes, deterministic).
- `skills/pakka-compress/SKILL.md`.
- `pakka-core meter` (parses usage from hook events, appends JSONL).
- SessionStart + SubagentStop hooks wired to compress.

Run:
```bash
claude --resume <session-id-from-pass-1> \
       "Execute Pass 2. Pakka is installed — it's running on you. Commit with compression ratio in the message."
```
**Gate:** status line shows non-zero "saved vs baseline" on real work.

### Pass 3 — review gate + secrets guard (thesis claim 2) (~1 day incl. threshold tuning)
- `agents/reviewer.md`, `agents/security.md` with Red Flags sections.
- `commands/pakka-review.md` with confidence threshold.
- `pakka-core guard` (secrets + O_NOFOLLOW).
- 10 seeded-bug cases in `benchmarks/seeds/`.
- Calibrate confidence threshold against the 10 seeds.

Run:
```bash
claude --resume <id> \
       "Execute Pass 3. Calibrate confidence threshold: report the value giving ≥8/10 catches with ≤1 false positive."
```
**Gate:** seeded corpus catches ≥ 8/10.

### Pass 4 — wizard + stack overlay + eval (~6h)
- `skills/pakka-init/SKILL.md` + `pakka-core stack-detect` + `stack-gate`.
- TypeScript stack overlay first (largest Claude Code audience).
- `pakka-core eval` — all three layers, runnable via `/pakka:eval`.

Run:
```bash
claude --resume <id> \
       "Execute Pass 4. Round-trip /pakka:init on a fresh TS repo. Confirm /pakka:eval is green on everything shipped."
```
**Gate:** `/pakka:init` works end-to-end on a clean TS repo. `/pakka:eval` passes.

### Pass 5 — benchmarks + self-report + release (~1 day wall-clock)
- `benchmarks/corpus.json` with 30 real PRs (10 each TS/Go/Python).
- `make bench` reproduces claims 1 and 2 end-to-end.
- `make self-report` (§9.1) — reads pakka-build's own audit + meter, emits `RECEIPTS.md`.
- Tag `v0.1.0`; update `marketplace.json` to pin version.
- Submit to Anthropic's official marketplace.

Run:
```bash
claude --resume <id> \
       "Execute Pass 5. Run make bench, write numbers into README, run make self-report into RECEIPTS.md, tag v0.1.0."
```
**Gate:** README shows three numbers with commit-hash provenance. `RECEIPTS.md` exists. v0.1.0 tagged.

### Total
~2.5 days of active dev + ~1 day wall-clock on benchmark runs. Calendar 3–5 days with daily review cadence.

**Stretch (not blocking v0.1):** Go and Python stack overlays; `pakka-status` command.

---

## 11. Dogfood protocol

1. Clone `pakka` locally. Run `/plugin install ./pakka` in a Claude Code session.
2. Every pakka-dev session runs with pakka installed. If pakka breaks its own session, fix first.
3. Every anti-pattern pakka **catches in itself** → add an entry to the Red Flags of the originating skill/agent.
4. Every anti-pattern pakka **misses in itself** (you caught it manually) → add a new seeded-bug entry to `benchmarks/`, then fix pakka until it catches.
5. Every skill/agent change goes through `/pakka:eval` locally before commit.
6. Any skill/agent change that regresses cost by > 10% vs last green run is blocked.

---

## 12. Open decisions during build (resolve as you hit them)

1. Binary size: one fat `pakka-core` with all subcommands, or split into small binaries? Start fat, split only if cold start suffers.
2. Where `~/.pakka/` lives on Windows: `%USERPROFILE%\.pakka\`. Confirm on first Windows test.
3. Marketplace SHA-pinning strategy: pin `sha` in marketplace.json for release versions; leave `ref: main` unpinned for pre-release channel (add `pakka@pre` entry later).
4. Red Flags format: start as a markdown table at the end of each SKILL.md. Revisit if skill bodies bloat.
5. First LLM-judge prompt template lives at `internal/eval/judge_prompt.md` — iterate with benchmark feedback.

---

## 13. Not in v0 — explicit deferrals

| Deferred | Why |
|---|---|
| Cross-session memory + semantic decay | Need real usage data to compress meaningfully |
| Performance subagent | Start with correctness + security |
| Multi-harness sync (Cursor/Copilot emitters) | Not needed to prove the thesis |
| Pattern library / predictive intervention | Requires a real audit corpus first |
| `pakka-status` command | Convenience; not load-bearing for v0 claims |

Anything else not listed in §4 is out of scope for v0.

---

## 14. Next move

Execute Pass 1 from §10 in a long-running Claude Code session. Each pass has a gate defined at its end. Ship `v0.1.0` when all five passes complete, `RECEIPTS.md` exists, and the three claim numbers are in this repo's README.
