---
name: compress
description: Control pakka compression. Switch output intensity (lite|strict|ultra|super-ultra), re-compress input files, restore originals.
allowed-tools: Read, Bash
argument-hint: "[lite|strict|ultra|super-ultra|restore|status]"
user-invocable: false
---

## Instructions

### Determine action from argument

- `lite` | `strict` | `ultra` | `super-ultra` → switch output compression level (see below).
- `restore` → restore all `.original.md` backups.
- `status` → show current compression stats.
- No argument → same as `status`.

### Switch output level (`lite|strict|ultra|super-ultra`)

1. Read `~/.config/pakka/config.json` (treat as `{}` if missing or malformed).
2. Set `defaultLevel` to the requested level.
3. Write the updated JSON back to `~/.config/pakka/config.json` (create parent dirs if needed).
4. Write the requested level string to the flag file so per-turn reinforcement updates immediately:
   - Path: `${CLAUDE_CONFIG_DIR}/.pakka-level` if `$CLAUDE_CONFIG_DIR` is set, else `~/.claude/.pakka-level`.
   - Overwrite atomically (write to `.pakka-level.tmp`, rename). Mode 0600.
5. Only if `pakka.compress.semantic` is `true` in `${CLAUDE_PLUGIN_ROOT}/settings.json`:
   run `${CLAUDE_PLUGIN_ROOT}/bin/run compress --orchestrator-run --level=<new-level>`
   and report progress (one line per file).
6. Confirm: "Output compression set to [level]. Active now."

Level effects:
- `lite`: No filler/hedging. Keep articles + full sentences. Professional tight.
- `strict`: Drop articles, fragments OK, short synonyms.
- `ultra`: Default. Abbreviate (DB/auth/config/req/res/fn/impl), strip conjunctions, arrows for causality.
- `super-ultra`: Maximum density. One token where one suffices, drop non-load-bearing words, symbols (→ for "leads to", = for "is", & for "and"). Pass 4.2 tier.

Default level is `ultra` — pakka's brand thesis is fewer tokens, and the
default reflects it. See memory/DECISIONS.md "Default output level: ultra".

### Status (default action)

Show current compression stats:

1. Read `${CLAUDE_PLUGIN_ROOT}/settings.json` for current mode and output level.
2. Run `${CLAUDE_PLUGIN_ROOT}/bin/run meter` or read `~/.pakka/meter/<session>.jsonl` for bytes saved.
3. Report:
   - Compression mode: `strict` | `audit`
   - Output level: `lite` | `strict` | `ultra` | `super-ultra`
   - Semantic mode: `on` | `off` (from `pakka.compress.semantic`)
   - Input bytes saved this session (from file compression + tool result truncation)
   - Estimated input tokens saved (bytes_saved / 3.5)
   - Output savings: `--` (requires baseline calibration)

### Restore

If `restore` is specified:
1. Find all `.original.md` files in project root + one level deep.
2. For each: copy `.original.md` back over the compressed file and delete the `.original.md`.
3. Report each restored file.

### Compress input files (legacy, still supported)

If a filepath is given explicitly:

1. Check if `<stem>.original.md` exists alongside it. If yes, skip — already compressed.
2. Read the file content.
3. Pipe content to: `${CLAUDE_PLUGIN_ROOT}/bin/run compress --mode=<mode>` (deterministic) or `${CLAUDE_PLUGIN_ROOT}/bin/run compress --semantic --level=<level>` (LLM rewrite).
4. Save the original file as `<stem>.original.md` (backup).
5. Write the compressed output to the original file path.
6. Report: filename, original size, compressed size, ratio, tokens saved estimate.

### Auto-orchestration

When `pakka.compress.semantic: true`, SessionStart automatically re-compresses
allowlisted memory files in the background. The orchestrator forks a detached
`pakka-core compress --orchestrator-bg` process so the SessionStart hook
returns under 50ms; the child writes progress to `~/.pakka/orchestrator.log`.

Allowlist (default):
- `CLAUDE.md`
- `DESIGN.md`
- `BUILD.md`
- `memory/LOG.md`
- `memory/DECISIONS.md`

Override via `pakka.compress.semanticTargets` in user `settings.json`. Paths
are relative to the repo root (cwd at hook fire time). Files not present are
silently skipped. Symlinks, files outside the repo, and non-`.md` files are
rejected for safety.

State lives at `<repo>/.pakka/compress-state.json`. Each entry records
`sourceSHA`, `level`, `compressedAt`, and `validatorPasses`. A file is
re-compressed when (a) it is new to the state file, (b) the level changed,
(c) the source SHA changed, or (d) the previous validator pass failed.

Status-line surfaces failed entries as `! N stale`. To retry stale entries,
re-run `/pakka:compress <level>` — that path runs synchronously and re-writes
state on success. Inspect state directly with:

```
${CLAUDE_PLUGIN_ROOT}/bin/run orchestrator-status
```

Auth: by default the orchestrator uses your Claude Code session auth via a
`claude -p` subprocess — no API key required. If `claude` is not on PATH it
falls back to `ANTHROPIC_API_KEY` HTTP. If neither is available the
orchestrator silently no-ops (logged but never warned during a session).
Force a path via `pakka.compress.engine` (`claude-cli` | `anthropic-http` |
`auto`).

### Semantic mode

Semantic mode invokes an LLM rewriter (Anthropic Messages API) to compress
prose. Use it when:

- The file is prose-heavy and the deterministic engine returns near-zero ratio.
- The user explicitly asks for aggressive rewrite (`/pakka:compress super-ultra`).
- A subagent return is verbose enough that prompt-injection rules alone won't help.

Semantic mode runs a deterministic Validator gate on every rewrite. The
validator confirms that fenced code blocks, inline backticks, URLs, file
paths, ISO dates, version strings, environment variables, and TODO/FIXME/SECURITY/HACK/BUG/XXX
markers all survive the rewrite verbatim. If any region is missing, semantic
mode runs a cherry-pick retry (max 2 retries) asking the model to put the
dropped regions back. If retries exhaust, semantic mode returns the ORIGINAL
input unchanged — never ships a partially corrupted file.

Auth resolution: (1) `claude` CLI subprocess (zero-config; reuses your
Claude Code OAuth/keychain), (2) `ANTHROPIC_API_KEY` HTTP fallback, (3)
deterministic strict fallback. Calls never fail because of a missing key —
the worst case is dropping to deterministic compression with a one-line
note in `~/.pakka/debug.log`.

Model: when the HTTP fallback is used, defaults to
`claude-haiku-4-5-20251001` (override via `PAKKA_COMPRESS_MODEL`). The
`claude -p` path inherits the user's local Claude Code model preference
unless the orchestrator settings pin one.

### Red Flags

- Compressing a file inside a code repo's source tree (not CLAUDE.md/DESIGN.md/BUILD.md) — ask first.
- Compressing a file that has no .original.md backup and is already very short (<500 bytes) — skip, not worth it.
- Losing TODO/FIXME/SECURITY markers after compression — bug, report immediately.
- Deterministic mode never calls LLM. Semantic mode calls LLM and MUST run the validator gate. A semantic compress that bypasses the validator is a bug, not a feature.
- Setting output level without confirming the change took effect — verify by reading `~/.config/pakka/config.json` and `~/.claude/.pakka-level` after write.
