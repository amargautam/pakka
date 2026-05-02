# Pakka Plugin — v0 Design
Target: `github.com/amargautam/pakka` (plugin) + `github.com/amargautam/pakka-marketplace` (catalog). License: Apache-2.0. Internal code: Go. Build window: ~3–5 days over 5 long-running Claude Code passes. Drive with Claude Code. Dogfood from Pass 1.
This doc is spec. Everything else derives from it.
---
## 1. v0 scope — three absolute claims (vs-raw deferred to v0.2.0)
1. **Bug catch rate.** 9/10 seeded bugs caught on Pass 5b in-session corpus (combined `pakka:reviewer` + `pakka:security` over 12 corpus entries). Raw Claude Code A/B comparison deferred to v0.2.0 — `claude -p --bare` requires `ANTHROPIC_API_KEY` and skips OAuth/keychain (see DECISIONS.md "Bench methodology"); honest A/B needs API-key bench budget.
2. **Bytes saved (compression).** Cumulative `bytes_saved` and `tokens_saved_est` (= bytes ÷ 3.5) reported by `make self-report` in `RECEIPTS.md`. Token figure is an estimate from byte count, not a measured token count.
3. **Gate enforcement (architectural).** Review gate runs on every Claude-authored `git commit` against pakka. The `Reviewed-by-pakka` trailer is the visible artifact when the hook fires. Trailer injection fix shipped in Pass 4.7 (v0.1.0). Architectural claim — gate runs, blocks on findings — remains the primary assertion. Verify: `git log --format='%H' | while read sha; do git show -s --format='%(trailers:key=Reviewed-by-pakka,valueonly=true)' "$sha" | grep -q . && echo "$sha"; done | wc -l`.
No claim without a check. Numbers come from `RECEIPTS.md` and `git log`, not narrative. vs-raw measurement returns at v0.2.0 with budget for `ANTHROPIC_API_KEY` headless runs. See §10 build order.
---
## 2. Repos
**`amargautam/pakka`** — plugin. Apache-2.0. Public from commit one.
**`amargautam/pakka-marketplace`** — catalog. Apache-2.0. Public. Two files only: `.claude-plugin/marketplace.json`, `README.md`.
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
│   ├── help.md         # /pakka:help
│   ├── review.md       # /pakka:review (calls reviewer + security agents)
│   ├── init.md         # /pakka:init     → wraps skill pakka-init
│   ├── eval.md         # /pakka:eval     → wraps skill pakka-eval
│   └── compress.md     # /pakka:compress → wraps skill pakka-compress
├── hooks/
│   └── hooks.json
├── rules/
│   └── output-compress.md  # output compression ruleset, injected at SessionStart
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
**Convention:** hooks invoke either `bin/pakka-core <subcommand> [args]` (Go binary) or `node ${CLAUDE_PLUGIN_ROOT}/hooks/<script>.js` (JS hooks). Go binary subcommands: `compress`, `meter`, `audit`, `guard`, `eval`, `stack-detect`, `status-line`. JS hooks: `compress-start.js` (SessionStart output rules), `compress-track.js` (UserPromptSubmit reinforcement). Users install nothing; right binary is selected via Claude Code's `${CLAUDE_PLUGIN_ROOT}` + OS/arch at hook invocation.

**Command/skill naming:** user-facing slash commands are bare (`/pakka:init`, `/pakka:eval`, `/pakka:compress`) for a uniform surface. Each is a thin wrapper that delegates to a skill named with the `pakka-` prefix (`pakka-init`, `pakka-eval`, `pakka-compress`) — the prefix keeps skills collision-safe in the global skill registry while the bare command name keeps the user surface clean. Wrappers pass `$ARGUMENTS` through verbatim; all behavior lives in the skill.
---
## 4. Components — what / where / why
| # | Component | Files | Packaging | Why |
|---|---|---|---|---|
| 1 | Wizard | `skills/pakka-init/SKILL.md` | Skill | Interactive, progressive disclosure, writes config. |
| 2 | Deny-by-default permissions | `settings.json` | Config | Declarative, mergeable. Zero runtime cost. |
| 3 | Context compressor (4-vector) | `hooks.json` (SessionStart, UserPromptSubmit, PostToolUse, SubagentStop) + `skills/pakka-compress/SKILL.md` | Hook + skill | 4 vectors: output tokens (prompt injection, ~60% reduction), input files (.md compression), tool results (PostToolUse truncation), subagent returns. Output tokens are 3-5× more expensive — biggest ROI. See §5.16. |
| 4 | Secrets guard | `hooks.json` (PreToolUse on Read/Bash) → `pakka-core guard` | Hook | Must block before tool runs. O_NOFOLLOW reads. |
| 5 | Parallel review | `agents/reviewer.md`, `agents/security.md`, `commands/pakka-review.md` | Subagents + command | Reasoning → subagents; gate logic → command; confidence ≥ 80. |
| 6 | Stack lint/test/format | `hooks.json` (PostToolUse on Edit/Write, Stop) — overlay written by wizard | Hook | Mechanical, fail-loud, zero context cost. |
| 7 | Token meter | `hooks.json` (PostToolUse, Stop) → `pakka-core meter` | Hook | Append-only JSONL. |
| 8 | Audit trail | Same hooks, `pakka-core audit` | Hook | Same JSONL stream, structured. |
| 9 | Status line | `hooks.json` (Stop) → `pakka-core status-line` | Hook | One-line summary, on by default. |
| 10 | Eval harness | `skills/pakka-eval/SKILL.md` + `pakka-core eval` + `benchmarks/` | Skill + CLI | Runs in `claude -p` headless; CI calls it. |
| 11 | Red-Flags convention | Every `SKILL.md` and agent file | Convention | Blocks anti-patterns, not guides. Superpowers lineage. |
Nothing not in this table ships in v0.
---
## 5. File specs
### 5.1 `plugin.json`
```
{
  "name": "pakka",
  "description": "Claude Code harness — fewer tokens, fewer bugs, audit-ready. Apache-2.0.",
  "version": "0.1.0",
  "author": { "name": "Amar Gautam", "email": "hello@amargautam.com" },
  "homepage": "https://pakka.dev",
  "repository": "https://github.com/amargautam/pakka",
  "license": "Apache-2.0",
  "keywords": ["harness", "review", "audit", "token-economy"]
}
```
### 5.2 `.claude-plugin/marketplace.json` (lives in **marketplace** repo)
```
{
  "name": "pakka-marketplace",
  "owner": { "name": "Amar Gautam", "email": "hello@amargautam.com" },
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
```
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
    "compress": { "outputLevel": "ultra" },
    "review": { "confidenceThreshold": 80 },
    "audit": { "path": "~/.pakka/audit" },
    "meter":  { "path": "~/.pakka/meter" }
  }
}
```
Wizard (`/pakka:init`) adds stack-specific allows (e.g. `Bash(go test ./...)`, `Bash(npm test)`) and stack-specific PostToolUse hooks.
### 5.4 `hooks/hooks.json`
All handlers invoke `${CLAUDE_PLUGIN_ROOT}/bin/pakka-core` (resolved at runtime to right arch binary by wizard on install).
```
{
  "SessionStart": [
    { "matcher": "", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/bin/pakka-core compress --phase=session-start" },
      { "type": "command", "command": "node ${CLAUDE_PLUGIN_ROOT}/hooks/compress-start.js" }
    ]}
  ],
  "UserPromptSubmit": [
    { "matcher": "", "hooks": [
      { "type": "command", "command": "node ${CLAUDE_PLUGIN_ROOT}/hooks/compress-track.js" }
    ]}
  ],
  "SessionEnd": [
    { "matcher": "", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/bin/pakka-core audit --phase=session-end" }
    ]}
  ],
  "PreToolUse": [
    { "matcher": "Read|Bash", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/bin/pakka-core guard" }
    ]}
  ],
  "PostToolUse": [
    { "matcher": "", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/bin/pakka-core meter" },
      { "type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/bin/pakka-core audit --phase=tool-post" }
    ]},
    { "matcher": "Read|Grep|Bash", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/bin/pakka-core compress --phase=tool-result" }
    ]},
    { "matcher": "Edit|Write", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/bin/pakka-core stack-gate" }
    ]}
  ],
  "SubagentStop": [
    { "matcher": "", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/bin/pakka-core compress --phase=subagent-return" }
    ]}
  ],
  "Stop": [
    { "matcher": "", "hooks": [
      { "type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/bin/pakka-core status-line" }
    ]}
  ]
}
```
Exit codes: 0 = pass, 2 = block (stderr → Claude). `pakka-core guard` is only one that blocks. Others never block on their own errors (exit 1 on internal failure, not 2).
### 5.5 `skills/pakka-init/SKILL.md`
Frontmatter:
```
---
name: pakka-init
description: One-time Pakka setup. Detects stack, writes stack overlay, verifies permissions and hooks work.
allowed-tools: Read, Write, Edit, Bash
user-invocable: true
---
```
Body responsibilities:
- Detect stack (language + toolchain) via `pakka-core stack-detect` on cwd.
- Ask only what can't be inferred (test command, coverage target, lint command if nonstandard).
- Write `.claude/settings.local.json` stack overlay (stack-specific `allow` entries + PostToolUse `stack-gate` script path).
- Write `.pakka/stack.json` with detected facts.
- Verify: run no-op tool call, confirm hooks fire, status line renders, audit JSONL written.
- Print three-line summary + next step (`/pakka:review`).
Red Flags section (rejects anti-patterns at run time):
- "Inferred stack but wrote config without confirming." → ask before write.
- "Overwrote user's existing `.claude/settings.local.json`." → merge, never replace.
- "Enabled network allow for wide domain." → deny, ask, or scope narrower.
### 5.6 `skills/pakka-compress/SKILL.md`
```
---
name: pakka-compress
description: Control pakka compression. Switch output intensity (lite|strict|ultra|super-ultra), re-compress input files, restore originals.
allowed-tools: Read, Bash
argument-hint: "[lite|strict|ultra|super-ultra|restore|status]"
user-invocable: true
---
```
Body:
- `lite|strict|ultra|super-ultra` → update `pakka.compress.outputLevel` in session config + emit confirmation. Takes effect on next UserPromptSubmit reinforcement. Default is `super-ultra` (v0.2.0+, see memory/DECISIONS.md).
- `restore` → restore all `.original.md` backups, removing compressed versions.
- `status` → show current compression stats: mode, output level, bytes saved (input), estimated output savings.
- No argument → same as `status`.
- Deterministic rules for input file compression (no LLM calls inside compressor):
- Strip duplicate whitespace, code-fence headers, trailing metadata.
- Collapse repeated section headings (keep first, drop rest).
- Keep all TODO/FIXME/SECURITY markers verbatim.
- Keep code blocks verbatim unless clearly dead (commented-out).
- Engine modes (orthogonal to output `outputLevel`):
- `strict` (default for the deterministic engine): structural + linguistic compression, all non-semantic tokens removed.
- `audit`: no compression, used when debugging eval discrepancies.
- Output level (`pakka.compress.outputLevel`): `lite|strict|ultra|super-ultra`. Default `ultra` (Pass 4.4). See §5.16 + memory/DECISIONS.md.
- Output: compressed text + ratio annotation (`compressed 42.1% · 8.3k → 4.8k`).
### 5.7 `skills/pakka-eval/SKILL.md`
```
---
name: pakka-eval
description: Run the 3-layer eval gate (static → LLM-judge → Monte Carlo) on a proposed skill/agent change.
allowed-tools: Read, Bash
user-invocable: true
---
```
Body: invokes `pakka-core eval` with target file(s) and writes results to `.pakka/eval/<ts>.json`. Details in §8.
### 5.8 `agents/reviewer.md`
```
---
name: reviewer
description: Parallel reviewer for correctness, perf, maintainability. Returns findings with confidence 0-100.
model: sonnet
tools: Read, Bash
---
```
Body (abbreviated):
- Read diff via `git diff --cached` (or provided range).
- For each hunk: identify risks in {logic, error handling, null/undefined, off-by-one, race, perf regression, API contract, test coverage}.
- For each finding, emit JSON line: `{"kind":"correctness","file":"...","line":123,"severity":"warn|error","confidence":0-100,"rationale":"...","fix":"..."}`
- Confidence calibration (explicit rules, Red Flags table below).
- No prose summary. JSON lines only. command wraps and filters.
Red Flags:
- Confidence ≥ 80 on anything stylistic → lower. Style isn't correctness bug.
- Reporting finding without line number → drop.
- Same finding repeated in two forms → dedupe before output.
### 5.9 `agents/security.md`
Same shape as reviewer. Focus: secrets leaks, injection (SQL, shell, path, XSS), auth bypass, unsafe deserialization, crypto misuse, SSRF, TOCTOU, permission escalation. Finding kind = `"security"`. Confidence threshold same: ≥ 80 to report.
### 5.10 `commands/pakka-review.md`
```
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
- Exit 2 if any `severity=error` finding passes threshold (blocks commit when wired into git pre-commit or CI).
### 5.11 `commands/pakka-status.md`
Prints last N status-line entries + aggregate token/savings/verdict counts for current session. Read-only. Useful for debugging.
### 5.12 Go binary — `cmd/pakka-core`
One binary. Subcommands invoked from hooks and skills:
| Subcommand | Inputs | Outputs | Notes |
|---|---|---|---|
| `compress --mode=<strict\|audit> --phase=<session-start\|subagent-return\|tool-result>` | stdin: hook event JSON or text; flags | stdout: compressed text (or truncated tool result); JSON line to `~/.pakka/meter/<session>.jsonl` | Deterministic. No LLM. Phase `tool-result` is new — truncates large Read/Grep/Bash output. |
| `output-rules` | _(deprecated — no longer wired in hooks.json; replaced by `hooks/compress-start.js`)_ | | |
| `output-reinforce` | _(deprecated — no longer wired in hooks.json; replaced by `hooks/compress-track.js`)_ | | |
| `guard` | stdin: hook event JSON | exit 0 (allow), exit 2 + stderr (block). Never `deny`-decides for paths already covered by settings; this is runtime second-line (O_NOFOLLOW symlink resolution, live `.env*` detection inside subdirs). | Cheap. Must be < 5ms p95. |
| `meter` | stdin: hook event JSON | JSONL line to `~/.pakka/meter/<session>.jsonl` | Parses usage from event; accumulates. |
| `audit --phase=<tool-post\|session-end>` | stdin: hook event JSON | JSONL line to `~/.pakka/audit/<session>.jsonl` | Structured schema (§7). |
| `stack-detect` | cwd | JSON to stdout | Looks for `go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml`, etc. |
| `stack-gate` | hook event JSON on stdin; reads `.pakka/stack.json` | Runs stack's lint/test/format sequence; exit 2 + stderr on failure | Mechanical. Fails loud. |
| `status-line` | session id from hook event | One line to stderr (where Claude Code picks it up for display). | Format in §6. |
| `eval <targets...>` | file paths | JSON to `.pakka/eval/<ts>.json`; exit 0/1/2 by layer | 3-layer gate (§8). |
| `report` | reads meter + audit JSONL | RECEIPTS.md to stdout | Already shipped. |
**Go code rules** (enforced via `CLAUDE.md`):
- No external deps beyond Go stdlib + `github.com/tidwall/gjson` (JSON scan) and `github.com/spf13/cobra` (CLI). Keep binary lean.
- Every subcommand has table-driven test.
- Every public function has `// Purpose:` + `// Errors:` comments.
- No goroutines in hook-path subcommands (`guard`, `meter`, `audit`, `status-line`, `stack-gate`) — simplicity > concurrency at hook latency scale.
- Hook-path subcommands MUST return in < 10ms p95 on cold run. Benchmark and gate.
### 5.13 Signature trailer
Every git commit Claude Code makes in session with pakka active gets two trailers appended to its message.
Trailer — claim (baseline):
```
Reviewed-by-pakka: v0.1.0
```
Trailer — claim (strong):
```
Reviewed-by-pakka: v0.1.0 (gate: passed)
```
Baseline = session was observed (audit trail), compressed, and permission-gated. Strong = auto-gate or `/pakka:review` ran and passed (no `severity=error` finding above threshold). `(gate: passed)` suffix is trust marker.
Trailer B — contributor attribution:
```
Co-authored-by: pakka <279024857+pakka-bot@users.noreply.github.com>
```
Why two trailers. `Reviewed-by-pakka:` is machine-searchable audit claim (`"Reviewed-by-pakka:" in:commits` on GitHub returns real usage). `Co-authored-by:` is GitHub-surface attribution — it makes pakka appear in repo Contributors widgets alongside humans and other bots. Different purposes, both honest, both low-friction.
`pakka-bot` GitHub account is attribution-only machine user owned by project maintainer. No repo write access anywhere. Its sole function is to be target of `Co-authored-by:` trailer so GitHub renders contributor link. Documented in README.
Rules:
- Name + version + optional gate suffix on Trailer A. No session id, no machine id, no repo hash, no identifying data.
- One line each, plain ASCII. Compose with `Signed-off-by`, `Co-Authored-By: Claude`, and other standard trailers.
- Default: **both on**. Fine-grained opt-out:
- `settings.json` → `"pakka.signature": false` → skip Trailer A.
- `settings.json` → `"pakka.coAuthor": false` → skip Trailer B.
- Strong form of Trailer only appears when auto-gate or `/pakka:review` verdict was `passed` within last N seconds (default 300). Otherwise baseline form.
Primary implementation — PreToolUse Bash injection (zero per-repo setup):
- Hook matches `Bash` tool calls where command starts with `git commit` (any variant: `-m`, `-F`, no flag, `--amend`).
- Hook rewrites command to add both trailers: `--trailer "Reviewed-by-pakka: v0.1.0[...]"` and `--trailer "Co-authored-by: pakka <279024857+pakka-bot@users.noreply.github.com>"`.
- Per-trailer opt-outs (`pakka.signature`, `pakka.coAuthor`) apply independently. If both false, hook is no-op.
- Hook is idempotent per trailer: if either trailer is already present in message or in existing `--trailer` flag, it is not duplicated.
- Works in any repo Claude Code operates on. No user action required. No `.git/hooks/` modification.
- Applies only to Claude-authored commits (commits made via `Bash` tool). Human commits in terminal are untouched.
Secondary implementation — optional `prepare-commit-msg` git hook (human commits):
- For teams that want trailer on human-authored commits too (audit completeness), `/pakka:review --install-hook` installs `.git/hooks/prepare-commit-msg` hook.
- Same idempotency, same two-tier logic, same opt-out key.
- Explicitly opt-in; never auto-installed.
Why:
- Public adoption signal — searchable via `"Reviewed-by-pakka:" in:commits` on GitHub. Gives us real-use metric distinct from installs.
- Anthropic's own `Claude Code` tool adds `Co-Authored-By: Claude` by default; precedent is established.
- No PII. One line. Opt-out clearly documented.
- Zero-setup default respects token economy principle: if signing requires manual command per repo, adoption → 0. PreToolUse makes it free.
Forward compatibility:
- When/if `pakka-dev` ships, it emits `Reviewed-by-pakka-dev: vX.Y.Z` — distinct namespace, distinct search query, no collision.
### 5.15 Auto-gate on commit
Every `git commit` Claude Code makes triggers review before commit lands. This is primary path. Manual `/pakka:review` is escape hatch for CI, pre-push audit, or mid-flow explicit review.
Flow:
- PreToolUse hook matches `Bash` tool calls where command starts with `git commit` (after leading whitespace; covers `-m`, `-F`, `--amend`, editor variants).
- `pakka-core commit-gate` runs `reviewer` + `security` subagents in parallel on `git diff --cached`.
- Findings filtered by `pakka.review.confidenceThreshold` (default 80).
- If any `severity=error` finding passes threshold → exit 2. Commit blocked. Stderr payload (grouped findings + fix suggestions) visible to model; model iterates.
- If review passes (or only `warning`/`info` findings) → rewrite command to add `--trailer "Reviewed-by-pakka: v0.1.0 (gate: passed)"` and allow.
- Writes verdict to `.pakka/reviews/<ts>.jsonl` regardless of outcome.
Config:
- `pakka.review.autoGate`: bool, default `true`.
- `pakka.review.confidenceThreshold`: int, default `80`.
- `pakka.review.maxDiffBytes`: int, default `200000`. If exceeded, fall back to baseline trailer with audit-log note; do not block. Large-diff reviews are too slow to be trusted as gate.
- `pakka.review.skipPaths`: glob list, default `[]`. Commits whose files all match skip patterns get baseline trailer only.
Opt-outs:
- `pakka.review.autoGate: false` → no auto-review; baseline trailer only; `/pakka:review` remains available.
- Per-commit escape: include `[skip pakka]` in commit message subject or body. Trailer downgraded to baseline; review skipped; skip is logged in audit trail. Transparent, not silent.
Why this design:
- Thesis dependency. "Bugs and token cost are same disease: context waste" requires review to run. Opt-in review = unreviewed code with trust trailer = trailer lies.
- Matches precedent set by `pre-commit`, `husky`, `lefthook` — review gate is git-time event, not human-initiated one.
- Blocking feedback flows back to model via stderr on exit 2; model sees finding and can fix + retry without human mediation.
- Honest escape hatches exist (`autoGate: false`, `[skip pakka]`) so we don't bully users — but defaults enforce thesis.
### 5.14 Guard rules
`pakka-core guard` runs as PreToolUse hook on `Read|Bash`. It's second-line defense after settings.json `deny` rules.
What it blocks at runtime (on top of settings-level deny list):
- Any `Read` where resolved path (after symlink expansion with `O_NOFOLLOW` at each hop) lands inside `.env*`, `~/.ssh/**`, `~/.aws/**`, `~/.gnupg/**`, `~/.netrc`, or any path matching user-configured `pakka.guard.deny_paths` glob.
- Any `Bash` command matching deny pattern that settings.json can't express cleanly (e.g., pipes through `eval`, dynamic `$()` expansion of untrusted strings, curl-pipe-sh via intermediate variables).
- Directory traversal attempts (`../../..`) that escape cwd subtree.
Why separate from settings.json deny:
- Settings.json denies are static glob matches. Guard resolves symlinks, expands `~`, and checks live filesystem state.
- Guard can introspect Bash commands semantically (e.g., detect `eval` + network fetch combo) where settings can only pattern-match.
Guard exit codes:
- `0` → allow (fall through to Claude Code's normal permission flow).
- `2` → block, stderr message shown to model as blocking feedback.
- Never `1` on policy decisions — `1` is reserved for internal guard errors (don't block user on our bug).
Config:
```
"pakka": {
  "guard": {
    "deny_paths": [],      // user-added globs, union with built-ins
    "allow_paths": [],     // explicit carve-outs (rare; require comment)
    "audit_all_blocks": true
  }
}
```
All guard blocks are logged to audit JSONL with `kind: "guard_block"` and `reason` string. Never logs attempted content; only hashed input.
### 5.16 Context compressor — 4-vector spec
Thesis: "context waste → token burn + bugs." Compression must target every channel tokens flow through, not just static files. Output tokens cost 3-5× input tokens — biggest ROI.
**Approach:** Prompt-injection output compression is a proven technique (~65% output reduction, zero LLM calls). Pakka extends it to tool results and subagent returns — vectors no existing tool covers.
#### Vector 1 — Output token compression (prompt injection)
**Hook:** SessionStart (full ruleset) + UserPromptSubmit (per-turn reinforcement).
**Mechanism:** Pure prompt injection. No LLM calls, no post-processing. Model is instructed to emit fewer tokens per response while preserving all technical substance.
**Hooks (JS — `hooks/` directory):**
- `hooks/compress-start.js` (SessionStart) — reads active level from `hooks/compress-config.js`, writes `.pakka-level` flag to `$CLAUDE_CONFIG_DIR`, reads `rules/output-compress.md`, filters to active level (strips other levels' table rows + example lines), emits filtered ruleset as stdout context. Hardcoded fallback if file missing.
- `hooks/compress-track.js` (UserPromptSubmit) — reads stdin JSON, handles `/pakka:compress <level>` (writes flag) and deactivation phrases (`"pakka verbose"`, `"normal mode"`, deletes flag), then reads flag and emits `hookSpecificOutput.additionalContext` reinforcement every turn. Prevents drift after many turns or context compaction.
- `hooks/compress-config.js` — shared module: `VALID_LEVELS`, `getDefaultLevel` (env `PAKKA_DEFAULT_LEVEL` → `~/.config/pakka/config.json` → `settings.json outputLevel` → `'super-ultra'`), `getSemanticEnabled` (level-based semantic auto-enable), `safeWriteFlag`, `readFlag`, `filterRuleset`.

**Flag file:** `$CLAUDE_CONFIG_DIR/.pakka-level` — written at SessionStart, updated on `/pakka:compress <level>`, deleted on deactivation. Per-turn reinforcement reads this to reflect level switches immediately within a session.

**Ruleset (`rules/output-compress.md`):** Full text in the file. Contains all 4 level rows + per-level examples (4 scenarios × 4 levels). `compress-start.js` and `filterRuleset()` strip non-active level rows/examples before injection — keeps context tight. Go `filterToLevel()` in `pakka-core` applies the same filter (kept for parity; no longer wired in hooks).

**Per-turn reinforcement (from `compress-track.js`):**
```
PAKKA COMPRESSION ACTIVE (<level>). Drop articles/filler/pleasantries/hedging. Fragments OK. Code/commits/security: write normal.
```
**Config:**
```
"pakka": {
  "compress": {
    "input": true,
    "output": true,
    "outputLevel": "ultra",
    "toolResult": true,
    "subagentReturn": true
  }
}
```
- `input: false` → skips SessionStart auto-compression of CLAUDE.md/DESIGN.md/BUILD.md.
- `output: false` → disables output compression entirely (no SessionStart injection, no per-turn reinforcement).
- `outputLevel` → `lite|strict|ultra|super-ultra`. Overrides level in ruleset. Default `ultra` (Pass 4.4).
- `toolResult: false` → disables PostToolUse Read/Grep/Bash output truncation.
- `subagentReturn: false` → disables SubagentStop return compression.

Note: the engine-mode field (`compress.mode`) was removed in Pass 4.1.1. It conflated the engine on/off signal with `outputLevel` and was redundant with the per-vector booleans.
**Measurement:** Output token savings are measured by comparing session's output tokens against a baseline estimate. Baseline = median output tokens per tool call from sessions without output compression (captured during benchmarking). Reported in status line and meter.
#### Vector 2 — Input file compression (existing, improved)
**Hook:** SessionStart → `pakka-core compress --phase=session-start`.
**What it compresses:** CLAUDE.md, DESIGN.md, BUILD.md in CWD and immediate subdirectories. Compresses in place, backs up as `.original.md`, meters savings.
**Current state:** Deterministic structural + linguistic rules. Effective on verbose prose (~40% on conversational text). Near-zero on already-terse files. This is fine — files written by agents following pakka's own voice rules are already compressed at authoring time.
**No changes planned.** Input file compression is the lowest-leverage vector. Keep as-is. The win is elsewhere.
#### Vector 3 — Tool result compression (new)
**Hook:** PostToolUse on `Read|Grep|Bash` → `pakka-core compress --phase=tool-result`.
**Problem:** Tool results are the largest context consumer (30-50% of session tokens). A `Read` of a 2000-line file dumps ~50KB into context. A `Bash` output can be arbitrarily large. Most of this is noise — the model needs a subset.
**Mechanism:** Deterministic truncation. No LLM calls.
**Rules:**
| condition | action |
|---|---|
| output ≤ `toolResultMaxBytes` (default 10KB) | pass through unchanged |
| output > `toolResultMaxBytes` | truncate: keep first `headLines` (default 80) + last `tailLines` (default 20) + insert `[pakka: truncated N lines, M bytes. Use offset/limit to read specific ranges.]` |
| output is error (exit code ≠ 0) | never truncate — model needs full error |
| output is from Edit/Write | never truncate — model needs confirmation |
**How it works with Claude Code hooks:**
PostToolUse hook receives the tool result on stdin. If result exceeds threshold, `pakka-core compress --phase=tool-result` emits truncated version to stdout. Claude Code uses hook stdout to annotate/replace tool result context via `hookSpecificOutput`.
**Config:**
```
"pakka": {
  "compress": {
    "toolResult": true,
    "toolResultMaxBytes": 10240,
    "toolResultHeadLines": 80,
    "toolResultTailLines": 20
  }
}
```
**Savings estimate:** On a typical session with 50+ Read/Bash calls, tool result compression saves 30-60% of input tokens from this vector. Combined with output compression, total session savings are material.
#### Vector 4 — Subagent return compression (existing hook, never built)
**Hook:** SubagentStop → `pakka-core compress --phase=subagent-return`.
**Mechanism:** Apply structural + linguistic compression (same as input file compression) to subagent return text before it enters parent context.
**Rules:**
- Strip blank lines, collapse whitespace, drop articles/filler (same linguistic rules as Vector 2).
- Preserve code blocks, paths, URLs, identifiers verbatim.
- Never compress if return text ≤ 1KB (not worth it).
**Config:**
```
"pakka": {
  "compress": {
    "subagentReturn": true
  }
}
```
#### Compression budget — where the savings come from

**Calibrated output reduction by level (Sonnet 4.6, single-turn bench, 2026-05-02):**
| level | output tokens baseline | output tokens compressed | reduction |
|---|---|---|---|
| lite | 213 words | 171 words | ~27% |
| strict | 131 words | 131 words | ~33% |
| ultra | 100 words | 100 words | ~55% (Opus token run) |
| super-ultra | 213 words | 72 words | **~66%** (Sonnet) / ~56% (Opus) |

Sonnet is the dominant model in Claude Code sessions. Default claim: **~66% output reduction at super-ultra**. Bench recorded in `benchmarks/compress-samples/`.

```
Typical session without pakka:
  Input:  ~200k tokens (CLAUDE.md ~10k, tool results ~120k, conversation ~60k, system ~10k)
  Output: ~80k tokens (at 5× cost = 400k input-equivalent)
  Effective cost: ~600k input-equivalent tokens

With 4-vector compression at super-ultra:
  V1 output:       80k → ~27k output tokens (66% reduction = 265k input-equiv saved)
  V2 input files:  10k → ~9k (near-zero on terse files, ~40% on verbose)
  V3 tool results: 120k → ~60k (50% reduction via truncation)
  V4 subagent:     variable, ~20% of remaining
  Effective cost:  ~250-300k input-equivalent tokens

Net: ~50-55% total cost reduction, dominated by output compression (V1).
```
#### Auth resolution (Pass 4.6)
Semantic compression and the SessionStart auto-orchestrator both pick a rewriter via the same resolution chain:
1. **`claude` CLI subprocess (default).** When `claude` is on `PATH`, pakka shells out via `claude -p --output-format text --permission-mode bypassPermissions` and pipes the per-level prompt template to stdin. Reuses the user's existing Claude Code OAuth/keychain auth — zero-config for any Claude Code user. Note: `--bare` is deliberately NOT used; `--bare` strips OAuth and forces `ANTHROPIC_API_KEY`, defeating the point of this path (Pass 5b finding).
2. **`ANTHROPIC_API_KEY` HTTP fallback.** If `claude` is missing from PATH but the env var is set, pakka calls the documented `/v1/messages` endpoint via `net/http`. Same templates, same validator gate, same retry budget.
3. **Deterministic strict.** If neither auth path is available, semantic mode falls back to deterministic structural+linguistic compression and logs a one-line note to `~/.pakka/debug.log`. The orchestrator no-ops silently. Calls never fail because of missing auth.
Forced via `pakka.compress.engine: "claude-cli" | "anthropic-http" | "auto"` (default `auto`). When forced, the orchestrator refuses to fall back — useful for debugging which path is actually running.
---
## 6. Status-line format
Printed to **stderr** by `pakka-core status-line` on `Stop`:
```
UTF-8: pakka [ultra] · ↑12.4K (43%) / ↓7.1K (33%) tok saved · 2 bugs caught
ASCII: pakka [ultra] | in 12.4K (43%) / out 7.1K (33%) tok saved | 2 bugs caught
```
Both absolute saved-token counts AND percentages are shown. Percent alone hides scale — 50% of 200 reads identical to 50% of 200K. Counts humanize via floor truncation: <1000 raw integer, K/M with one decimal (12450 → "12.4K", 1234567 → "1.2M").
Parts:
- `[super-ultra]`: active output compression level (lite/strict/ultra/super-ultra). Default is `super-ultra` (v0.2.0+); was `ultra` per Pass 4.4 — see memory/DECISIONS.md.
- `↑<abs> (<pct>%)`: input token savings this session (file compression + tool result truncation + subagent return compression). Sum of `tokens_saved_est` from meter entries; pct = saved / cost-weighted input denominator.
- `↓<abs> (<pct>%)`: output token savings estimate (session output tokens × reduction factor from calibrated baseline). High-value number — output tokens cost 3-5×.
- `bugs caught`: count of reviewer/security findings with `severity=error` and confidence ≥ threshold this session.
Output savings measurement:
- Baseline: median output tokens per session from `make bench` runs without output compression.
- Session: actual output tokens from meter.
- Savings: `baseline_median - actual`. Reported as `↓<abs> (<pct>%)`.
- Until baseline is calibrated, savings render as `↓0 (0%)` (no fake numbers).
Togglable: `settings.json` → `"pakka.display.statusLine": false`.
---
## 7. Audit JSONL schema
One file per session at `~/.pakka/audit/<session-id>.jsonl`. Append-only. Each line:
```
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
- `redacted: true` when input matched secrets pattern (still hashed; content never stored).
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
- Runs `claude -p` in headless mode on 10 sampled prompts from `benchmarks/corpus.json` scoped to target skill/agent.
- Measures: trigger rate (skill auto-invokes when it should), false-positive rate, token cost, verdict agreement.
- Pass: trigger rate ≥ 0.8, false-positive ≤ 0.1, cost within ±10% of last green run.
Output: `.pakka/eval/<ts>.json` — full results, per-layer verdicts, diffs vs previous green run. Gate: no skill change merges without layer-3 green.
---
## 9. Benchmark corpus — 40 cases
`benchmarks/corpus.json` indexes everything. Entries:
```
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
Each seed has expected finding; `pakka:review` must surface it with confidence ≥ 80. Baseline: run raw Claude Code on same prompts; record what it catches.
`make bench` executes all 40 via `claude -p`, writes `benchmarks/results/<ts>.json`, updates `README.md` claim numbers via post-script.
### 9.1 `make self-report` — receipts from building Pakka with Pakka
Companion target to `make bench`. Reads pakka-build's own `~/.pakka/meter/*.jsonl` + `~/.pakka/audit/*.jsonl` from every session that contributed to repo, emits `RECEIPTS.md`:
- Session count and total wall-clock.
- Total tokens consumed across build.
- Estimated tokens saved by compressor (ratio × baseline).
- Count of reviewer/security verdicts run; count that passed threshold.
- Anti-patterns pakka caught in its own code (grouped by kind).
- Cases that started as "pakka missed this in itself" and became seeded-bug benchmark entries.
Implementation: `pakka-core report --format=md --since=<git-first-commit-date>`. Runs at release time. Commit `RECEIPTS.md` at repo root. build's own proof points live alongside code.
---
## 10. Build order — 5 long-running passes
Driven via long-running local Claude Code sessions. Pakka runs on its own build from Pass 1 onward — flywheel. Approve at phase boundaries, not per-commit.
### Pass 1 — skeleton + install + dogfood loop (~2h)
- `.claude-plugin/plugin.json`, `settings.json` (deny-by-default baseline).
- Marketplace repo `.claude-plugin/marketplace.json`.
- `cmd/pakka-core` scaffold; `status-line` and `audit` subcommands implemented.
- `hooks/hooks.json` wired for Stop (status line) + PostToolUse (audit).
- Prebuilt binaries committed to `bin/` (darwin arm64/amd64, linux arm64/amd64, windows amd64).
- Paste-ready README with install steps.
Kick it off:
```
claude --permission-mode acceptEdits \
       --allowedTools "Bash(go *),Bash(git *),Bash(make *),Read,Edit,Write" \
       "Execute Pass 1 from DESIGN.md. Stop when /plugin install works and status-line renders on a fresh session."
```
**Gate:** `/plugin marketplace add amargautam/pakka-marketplace && /plugin install pakka@pakka-marketplace` on fresh session produces status line. Install pakka locally. Flywheel starts.
### Pass 2 — compressor + meter (thesis claim 1) (~4h)
- `pakka-core compress` (strict/audit modes, deterministic).
- `skills/pakka-compress/SKILL.md`.
- `pakka-core meter` (parses usage from hook events, appends JSONL).
- SessionStart + SubagentStop hooks wired to compress.
Run:
```
claude --resume <session-id-from-pass-1> \
       "Execute Pass 2. Pakka is installed — it's running on you. Commit with compression ratio in the message."
```
**Gate:** status line shows non-zero "saved vs baseline" on real work.
### Pass 2.1 — linguistic compression
Layered on top of Pass 2's structural compression. Deterministic, rule-based. Applied before model ever sees text — not asked of model.
Rules (applied in order, idempotent):
1. Drop articles: "the", "a", "an" — but never inside code blocks, identifiers, or quoted strings.
2. Drop filler: "just", "really", "", "simply", "very", "", "kind of", "sort of".
3. Drop hedging: "I think", "maybe", "perhaps", "it seems", "I believe", "".
4. Drop pleasantries: "please", "", "", "happy to".
5. Fragment where possible: drop leading "That is", "This is", "There is/are", "It is" when fragment still reads.
6. Preserve (never touch): code blocks (`` ``` ``...`` ``` ``), inline code (`` ` ``...`` ` ``), URLs, file paths, identifiers with underscores or camelCase, TODO/FIXME/SECURITY markers, numbers, SPDX-like tags.
Modes:
- strict: structural + linguistic (default).
- audit: off.
Tests: table-driven test covers each rule with pair (input → expected output), plus 5 "must not touch" cases (code, URLs, identifiers, numbers, FIXME-markers).
Benchmark: compress 1KB sample of typical CLAUDE.md prose and 2KB sample of verbose subagent return. Reduction % for both modes committed into `benchmarks/compress-samples/` for reproducibility.
### Pass 3 — review gate + secrets guard (thesis claim 2) (~1 day incl. threshold tuning)
**Deliverables:**
1. `agents/reviewer.md` — correctness reviewer subagent. Emits JSON-line findings with `kind`, `severity`, `confidence` (0–100), `file`, `line`, `rationale`, `fix`. Red Flags section rejects stylistic-as-correctness, missing line numbers, duplicate findings.
2. `agents/security.md` — security reviewer subagent. Same JSON shape, `kind="security"`. Focus: injection (SQL/shell/path/XSS), auth bypass, unsafe deserialization, crypto misuse, SSRF, TOCTOU, permission escalation, secret leaks in logs.
3. `commands/review.md` — `/pakka:review [--base=<ref>]` runs both agents in parallel over `git diff --cached` (or `--base` target), filters by `pakka.review.confidenceThreshold` (default 80), groups findings by file, prints verdicts, writes full log to `.pakka/reviews/<commit-or-ts>.jsonl`. Exit 2 if any `severity=error` passes threshold. (Filename is `review.md`, not `pakka-review.md`: Claude Code renders slash commands as `/<plugin>:<filename>`, so `review.md` → `/pakka:review`.)
4. `pakka-core guard` — §5.14 ruleset. PreToolUse hook on `Read|Bash`. O_NOFOLLOW resolution, live `.env*` detection, Bash semantic checks.
5. `pakka-core sign` — PreToolUse Bash hook per §5.13. Matches commands starting with `git commit`, rewrites them to inject `--trailer "Reviewed-by-pakka: v0.1.0"` (or strong `(gate: passed)` form if recent passed review is on file). Idempotent. Registered in `hooks/hooks.json`. `/pakka:review --install-hook` flag remains as opt-in `prepare-commit-msg` install for human-authored commits only.
6. `benchmarks/seeds/` — 10 seeded-bug cases, one bug class each (N+1, null deref, off-by-one, TOCTOU, SQLi, shell injection, secret literal, ignored err, race, permission escalation). Each seed has: diff (`*.patch`), expected finding (`kind`, approximate `line`, `severity`), and prompt used to feed reviewer.
7. Confidence threshold calibration: run 10 seeds at thresholds 70/75/80/85/90. Pick value giving ≥ 8/10 catches with ≤ 1 false positive on small negative corpus (2–3 clean diffs). Record chosen value + curve in `benchmarks/calibration.md`.
**New settings.json keys added by this pass:**
```
"pakka": {
  "review": { "confidenceThreshold": 80 },
  "signature": true,
  "guard": {
    "deny_paths": [],
    "allow_paths": [],
    "audit_all_blocks": true
  }
}
```
Run:
```
Execute Pass 3 from DESIGN.md on branch v0.1.0-dev.
Build reviewer + security subagents, /pakka:review command,
guard subcommand, 10 seeded-bug cases, signature trailer hook.
Calibrate confidence threshold. Squash to one commit at end.
Push v0.1.0-dev.
```
**Gate:** seeded corpus catches ≥ 8/10 at chosen threshold; false-positive rate ≤ 1 on clean corpus; `Reviewed-by-pakka: v0.1.0` appears in commit message after test review passes; `pakka-core guard` blocks test read of `~/.ssh/id_rsa`.
### Pass 3.1 — auto-gate, auto-trailer, discoverability (~4h)
**Deliverables:**
1. Rename `commands/pakka-review.md` → `commands/review.md` so slash path becomes `/pakka:review`.
2. `commands/help.md` → `/pakka:help`. One-screen mono status per §5.13 preamble: version, session id, auto-systems on/off, available commands, audit/meter paths + current counts, docs link. Reads live from `settings.json` + `~/.pakka/audit/<session>.jsonl` + `~/.pakka/meter/<session>.jsonl`.
3. `pakka-core commit-gate` subcommand — PreToolUse hook per §5.15. Matches `Bash(git commit*)`, runs reviewer + security agents parallel on staged diff, blocks on error findings above threshold, rewrites command to append `(gate: passed)` trailer on success. Idempotent on `--amend` and on commits already carrying trailer.
4. Register `commit-gate` in `hooks/hooks.json` as `PreToolUse` hook with `matcher: "Bash"`.
5. Baseline trailer injection when auto-gate is disabled or skipped (`[skip pakka]` in message, or `pakka.review.autoGate: false`) — same `pakka-core commit-gate` subcommand, shorter path, injects `Reviewed-by-pakka: v0.1.0` without gate suffix.
6. `internal/commitgate/` package with table-driven tests: `git commit -m "x"` → rewritten; `git commit --amend` → rewritten; already-trailered → no-op; `[skip pakka]` → baseline trailer; review-fails → exit 2 with stderr payload; `pakka.review.autoGate: false` → baseline trailer no review; non-`git commit` Bash → exit 0.
7. Keep `/pakka:review --install-hook` as opt-in `prepare-commit-msg` installer for human-authored commits (per §5.13 secondary implementation). Update its help text: "Optional. For teams that want trailer on human commits too. Claude-authored commits are auto-signed via PreToolUse — no hook install needed."
8. New settings.json keys:
```
   "pakka": {
     "review": {
       "confidenceThreshold": 80,
       "autoGate": true,
       "maxDiffBytes": 200000,
       "skipPaths": []
     },
     "signature": true
   }
   ```
**Gate:**
- `/pakka:review` and `/pakka:help` tab-complete in fresh session after install.
- test `git commit` with clean diff (via Claude Code) carries `Reviewed-by-pakka: v0.1.0 (gate: passed)`.
- test `git commit` with seeded-bug diff (via Claude Code) is blocked at PreToolUse with reviewer's finding as stderr.
- Setting `pakka.review.autoGate: false` and re-running clean commit produces baseline trailer and skips review.
- `[skip pakka]` in commit message produces baseline trailer with audit-log skip entry.
- `commit-gate` p95 overhead on 10-line clean diff < 2s (review itself dominates; rewrite path < 5ms).
Run:
```
Execute Pass 3.1 from DESIGN.md on branch v0.1.0-dev.
Rename review command, add /pakka:help, wire auto-gate via
PreToolUse on Bash(git commit*), add baseline + strong trailer
logic, add tests, calibrate gate latency. Squash to one commit.
Push v0.1.0-dev --force-with-lease.
```
### Pass 3.2 — co-author attribution (~1h)
**Deliverables:**
1. Extend `internal/commitgate/` rewrite logic to append second `--trailer` flag per DESIGN.md §5.13:
```
   Co-authored-by: pakka <279024857+pakka-bot@users.noreply.github.com>
   ```
2. Respect independent opt-outs: `pakka.signature: false` skips Trailer A; `pakka.coAuthor: false` skips Trailer B. If both false, hook is no-op on command.
3. Idempotency: do not duplicate Trailer B if commit message or existing `--trailer` flag already contains `Co-authored-by: pakka <279024857+pakka-bot@users.noreply.github.com>`. Exact match only (case-insensitive on `Co-authored-by:` key per RFC).
4. Add table-driven tests for Trailer B: both-present → no-op; A-only present → add B; B-only present → add A; coAuthor=false → skip B; both opted out → no-op; `[skip pakka]` → still skip B (consistency with full-skip semantics).
5. Update `/pakka:help` output: add `contributors` or `attribution` line referencing `pakka-bot` co-author target, e.g. ` attribution pakka <279024857+pakka-bot@users.noreply.github.com>`.
6. Add short `## Attribution` section to `README.md`: plain-language explanation of `pakka-bot` GitHub account — attribution-only machine user, owned by project maintainer, no repo write access anywhere, exists solely so GitHub renders review as contributor. Link to pakka-bot profile.
7. New settings.json key:
```
   "pakka": {
     "coAuthor": true
   }
   ```
8. Cleanup: remove dead `Stop` hook entry from `hooks/hooks.json` that still invokes `status-line`. dedicated `statusLine` config in `settings.json` already handles rendering; Stop-hook invocation is dead code from Pass 1 debug cycle and was routing stderr to model as spurious blocking feedback. `hooks.json` change has already been applied in DESIGN-authoring workspace; Pass 3.2 squash should pick it up along with other changes.
**Gate:**
- No Stop-hook errors observed in Claude Code session stderr after install.
- test `git commit` made via Claude Code on `pakka` itself carries both trailers.
- `git log -1 --format=%B` shows `Reviewed-by-pakka: v0.1.0 (gate: passed)` and `Co-authored-by: pakka <279024857+pakka-bot@users.noreply.github.com>`.
- `pakka.coAuthor: false` + clean commit → only Trailer present.
- `pakka.signature: false` + clean commit → only Trailer B present.
- Both false + clean commit → commit goes through with no pakka trailers.
- README `## Attribution` section renders; link resolves to `github.com/pakka-bot`.
- After push, `github.com/amargautam/pakka/graphs/contributors` shows `pakka` alongside `claude` on commits that pass through Pass 3.2.
Run:
```
Execute Pass 3.2 from DESIGN.md on branch v0.1.0-dev.
Add Co-authored-by trailer for pakka-bot, per-trailer opt-outs,
idempotency tests, /pakka:help attribution line, README section.
Squash to one commit. Push v0.1.0-dev --force-with-lease.
```
### Pass 4 — wizard + stack overlay + eval (~6h)
- `skills/pakka-init/SKILL.md` + `pakka-core stack-detect` + `stack-gate`.
- TypeScript stack overlay first (largest Claude Code audience).
- `pakka-core eval` — all three layers, runnable via `/pakka:eval`.
Run:
```
claude --resume <id> \
       "Execute Pass 4. Round-trip /pakka:init on a fresh TS repo. Confirm /pakka:eval is green on everything shipped."
```
**Gate:** `/pakka:init` works end-to-end on clean TS repo. `/pakka:eval` passes.
### Pass 4.1 — 4-vector compression (~4h)
Compression pivot. Prior approach (compress CLAUDE.md only) produced near-zero savings. This pass implements all 4 vectors from §5.16.
**Deliverables:**
1. `rules/output-compress.md` — output compression ruleset (full text in §5.16).
2. `pakka-core output-rules` subcommand — reads ruleset, emits to stdout for SessionStart injection. Hardcoded fallback if file missing. Reads `pakka.compress.outputLevel` to filter intensity.
3. `pakka-core output-reinforce` subcommand — emits short reinforcement JSON to stdout for UserPromptSubmit. Format: `{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"..."}}`.
4. `pakka-core compress --phase=tool-result` — reads PostToolUse hook event, checks output size against `toolResultMaxBytes` (default 10KB). If over: emit truncated version (head 80 + tail 20 lines + notice). If error (exit ≠ 0): pass through unchanged. If Edit/Write: pass through unchanged.
5. `pakka-core compress --phase=subagent-return` — apply structural + linguistic compression to subagent return text. Skip if ≤ 1KB.
6. Update `hooks/hooks.json`: add UserPromptSubmit hook, add `output-rules` to SessionStart, add `compress --phase=tool-result` to PostToolUse on Read|Grep|Bash.
7. Update `skills/pakka-compress/SKILL.md` to support `lite|strict|ultra|super-ultra|restore|status` arguments. Default `outputLevel` is `ultra` (Pass 4.4).
8. Update `pakka-core status-line` to show `in saved` + `out saved` separately.
9. Tests: table-driven for each new subcommand. Output-rules: file-present → emits, file-missing → fallback. Output-reinforce: emits valid JSON. Tool-result: under-threshold → passthrough, over-threshold → truncated, error → passthrough. Subagent-return: small → passthrough, large → compressed.
**Gate:**
- Fresh session with pakka installed: SessionStart emits output compression ruleset (visible in `~/.pakka/debug.log`).
- UserPromptSubmit reinforcement fires on every user message.
- Large `Read` output (>10KB) is truncated in context with notice.
- Status line shows separate `in saved` and `out saved` values.
- Output compression visibly reduces Claude's response verbosity (manual check: compare response length with `pakka.compress.output: false` vs `true`).
Run:
```
Execute Pass 4.1 from DESIGN.md on branch v0.1.0-dev.
Implement 4-vector compression per §5.16. Priority order:
V1 (output-rules + output-reinforce) first — biggest ROI.
Then V3 (tool-result truncation). Then V4 (subagent-return).
V2 (input files) already works. Tests for all. Squash to one commit.
Push v0.1.0-dev.
```
### Pass 5 — benchmarks + self-report + release (~1 day wall-clock)
- `benchmarks/corpus.json` with 30 real PRs (10 each TS/Go/Python).
- `make bench` reproduces claims 1 and 2 end-to-end.
- `make self-report` (§9.1) — reads pakka-build's own audit + meter, emits `RECEIPTS.md`.
- Tag `v0.1.0`; update `marketplace.json` to pin version.
- Submit to Anthropic's official marketplace.
Run:
```
claude --resume <id> \
       "Execute Pass 5. Run make bench, write numbers into README, run make self-report into RECEIPTS.md, tag v0.1.0."
```
**Gate:** README shows three numbers with commit-hash provenance. `RECEIPTS.md` exists. v0.1.0 tagged.
### Total
~2.5 days of active dev + ~1 day wall-clock on benchmark runs. Calendar 3–5 days with daily review cadence.
**Stretch (not blocking v0.1):** Go and Python stack overlays; `pakka-status` command.
---
## 11. Dogfood protocol
1. Clone `pakka` locally. Run `/plugin install ./pakka` in Claude Code session.
2. Every pakka-dev session runs with pakka installed. If pakka breaks its own session, fix first.
3. Every anti-pattern pakka **catches in itself** → add entry to Red Flags of originating skill/agent.
4. Every anti-pattern pakka **misses in itself** (you caught it manually) → add new seeded-bug entry to `benchmarks/`, then fix pakka until it catches.
5. Every skill/agent change goes through `/pakka:eval` locally before commit.
6. Any skill/agent change that regresses cost by > 10% vs last green run is blocked.
---
## 12. Open decisions during build (resolve as you hit them)
1. Binary size: one fat `pakka-core` with all subcommands, or split into small binaries? Start fat, split only if cold start suffers.
2. Where `~/.pakka/` lives on Windows: `%USERPROFILE%\.pakka\`. Confirm on first Windows test.
3. Marketplace SHA-pinning strategy: pin `sha` in marketplace.json for release versions; leave `ref: main` unpinned for pre-release channel (add `pakka@pre` entry later).
4. Red Flags format: start as markdown table at end of each SKILL.md. Revisit if skill bodies bloat.
5. First LLM-judge prompt template lives at `internal/eval/judge_prompt.md` — iterate with benchmark feedback.
---
## 13. Not in v0 — explicit deferrals
| Deferred | Why |
|---|---|
| Cross-session memory + semantic decay | Need real usage data to compress meaningfully |
| Performance subagent | Start with correctness + security |
| Multi-harness sync (Cursor/Copilot emitters) | Not needed to prove thesis |
| Pattern library / predictive intervention | Requires real audit corpus first |
| `pakka-status` command | Convenience; not load-bearing for v0 claims |
Anything else not listed in §4 is out of scope for v0.
---
## 14. Next move
Execute Pass 1 from §10 in long-running Claude Code session. Each pass has gate defined at its end. Ship `v0.1.0` when all five passes complete, `RECEIPTS.md` exists, and three claim numbers are in this repo's README.
---
## References
- None published externally at this time.
