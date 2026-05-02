---
description: Control pakka compression. Switch output intensity (lite|strict|ultra|super-ultra), re-compress input files, restore originals. Default level is `ultra`.
allowed-tools: Read, Write, Bash
argument-hint: "[lite|strict|ultra|super-ultra|restore|status]"
---

## Instructions

### Determine action from argument

- `lite` | `strict` | `ultra` | `super-ultra` → switch output compression level (see below).
- `restore` → restore all `.original.md` backups.
- `status` → show current compression stats.
- No argument → same as `status`.

### Switch output level (`lite|strict|ultra|super-ultra`)

1. Validate the argument is exactly one of: `lite`, `strict`, `ultra`, `super-ultra`. If not, report invalid level and stop.
2. Read `${CLAUDE_PLUGIN_ROOT}/settings.json`.
3. Update `pakka.compress.outputLevel` to the validated level string.
4. Write settings back. Read it again to confirm the value was written correctly.
5. Run the orchestrator with the validated level. Use the validated literal string from step 1 as the `--level` flag value — e.g. if the validated level is `ultra`, run: `${CLAUDE_PLUGIN_ROOT}/bin/run compress --orchestrator-run --level=ultra`. Never pass the raw user argument without prior allowlist validation.
6. Confirm: "Output compression set to [level]. Takes effect next response."

Level effects:
- `lite`: No filler/hedging. Keep articles + full sentences. Professional tight.
- `strict`: Drop articles, fragments OK, short synonyms.
- `ultra`: Default. Abbreviate (DB/auth/config/req/res/fn/impl), strip conjunctions, arrows for causality.
- `super-ultra`: Maximum density. One token where one suffices, symbols (→ = &).

### Status (default action)

1. Read `${CLAUDE_PLUGIN_ROOT}/settings.json` for current output level and semantic mode.
2. Find the most recent meter file: `ls -t ~/.pakka/meter/*.jsonl 2>/dev/null | head -1` and read it for bytes saved.
3. Report:
   - Output level: `lite` | `strict` | `ultra` | `super-ultra`
   - Semantic mode: `on` | `off`
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
