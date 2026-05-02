---
description: Control pakka compression. Switch output intensity (lite|strict|ultra|super-ultra), re-compress input files, restore originals. Default level is `ultra`.
allowed-tools: Read, Write, Bash
argument-hint: "[lite|strict|ultra|super-ultra|restore|status]"
---

## Instructions

### Hook pre-handling (check first, always)

The UserPromptSubmit hook runs before this command and handles level switches, status, and help directly — no tool calls needed on your part.

Check `additionalContext` for a line starting with `PAKKA HOOK HANDLED:`.

- If found for `compress level set to <level>`: output exactly "Output compression set to <level>. Active now." — stop, no tool calls.
- If found for `compress status`: output the pre-computed table verbatim — stop, no tool calls.
- If found for `compress off`: output exactly "Output compression turned off." — stop, no tool calls.
- If NOT found: the hook did not run (e.g. direct invocation) — proceed with the steps below.

### Determine action from argument (only if hook did not pre-handle)

- `lite` | `strict` | `ultra` | `super-ultra` → switch output compression level (see below).
- `restore` → restore all `.original.md` backups.
- `status` → show current compression stats.
- No argument → same as `status`.

### Switch output level (`lite|strict|ultra|super-ultra`)

1. Validate the argument is exactly one of: `lite`, `strict`, `ultra`, `super-ultra`. If not, report invalid level and stop.
2. Read `~/.config/pakka/config.json` (treat as `{}` if missing or malformed).
3. Set `defaultLevel` to the validated level string.
4. Write the updated JSON back to `~/.config/pakka/config.json` (create parent dirs if needed).
5. Write the validated level string to the flag file for immediate per-turn effect:
   - Path: `${CLAUDE_CONFIG_DIR}/.pakka-level` if `$CLAUDE_CONFIG_DIR` is set, else `~/.claude/.pakka-level`.
   - Overwrite atomically (write to `.pakka-level.tmp`, rename). Mode 0600.
6. Determine whether semantic orchestration runs using this logic:
   - `super-ultra` → always run (enforced, cannot be disabled)
   - `ultra` → run unless `pakka.compress.semantic` is explicitly `false` in `${CLAUDE_PLUGIN_ROOT}/settings.json`
   - `lite` | `strict` → only run if `pakka.compress.semantic` is explicitly `true`
   If semantic should run: `${CLAUDE_PLUGIN_ROOT}/bin/run compress --orchestrator-run --level=<validated-level>`.
   Never pass the raw user argument without prior allowlist validation.
7. Confirm: "Output compression set to [level]. Active now."

Level effects:
- `lite`: No filler/hedging. Keep articles + full sentences. Professional tight.
- `strict`: Drop articles, fragments OK, short synonyms.
- `ultra`: Default. Abbreviate (DB/auth/config/req/res/fn/impl), strip conjunctions, arrows for causality.
- `super-ultra`: Maximum density. One token where one suffices, symbols (→ = &).

### Status (default action)

1. Read `~/.config/pakka/config.json` for current `defaultLevel`. Fall back to `${CLAUDE_PLUGIN_ROOT}/settings.json` `pakka.compress.outputLevel` if config.json is absent. Read `${CLAUDE_PLUGIN_ROOT}/settings.json` for semantic mode.
2. Find the most recent meter file: `ls -t ~/.pakka/meter/*.jsonl 2>/dev/null | head -1` and read it for bytes saved.
3. Report:
   - Output level: `lite` | `strict` | `ultra` | `super-ultra`
   - Semantic mode: `on` | `off` (derived: super-ultra=always on, ultra=on unless explicit false, lite/strict=off unless explicit true)
   - Input bytes saved this session
   - Estimated input tokens saved (bytes_saved / 3.5)

### Restore

1. Find all `.original.md` files in project root + one level deep.
2. List them to the user and ask for explicit confirmation before overwriting anything.
3. After confirmation: copy each `.original.md` back over the compressed file.
4. Do NOT delete the `.original.md` backup — leave it in place. User can delete manually if desired.
5. Report each restored file.

## Red Flags

- Invoking the Skill tool from this command → causes infinite loop. This command is self-contained; use Read/Write/Bash only.
- Interpolating raw user input into a Bash command without allowlist validation → shell injection vector. Always validate level against the 4 known values first.
- Using `<placeholder>` syntax literally in a Bash command → shell interprets as file redirect. Always resolve to actual values before running.
- Deleting `.original.md` backup files without explicit user confirmation → irreversible data loss. Always ask first.
