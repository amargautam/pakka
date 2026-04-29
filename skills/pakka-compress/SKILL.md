---
name: pakka-compress
description: Control pakka compression. Switch output intensity (lite|strict|ultra), re-compress input files, restore originals.
allowed-tools: Read, Bash
argument-hint: "[lite|strict|ultra|restore|status]"
user-invocable: false
---

## Instructions

### Determine action from argument

- `lite` | `strict` | `ultra` → switch output compression level (see below).
- `restore` → restore all `.original.md` backups.
- `status` → show current compression stats.
- No argument → same as `status`.

### Switch output level (`lite|strict|ultra`)

1. Read `${CLAUDE_PLUGIN_ROOT}/settings.json`.
2. Update `pakka.compress.outputLevel` to the requested level.
3. Write settings back.
4. Confirm: "Output compression set to [level]. Takes effect next response."

Level effects:
- `lite`: No filler/hedging. Keep articles + full sentences. Professional tight.
- `strict`: Drop articles, fragments OK, short synonyms. Default.
- `ultra`: Abbreviate (DB/auth/config/req/res/fn/impl), strip conjunctions, arrows for causality.

### Status (default action)

Show current compression stats:

1. Read `${CLAUDE_PLUGIN_ROOT}/settings.json` for current mode and output level.
2. Run `${CLAUDE_PLUGIN_ROOT}/bin/run meter` or read `~/.pakka/meter/<session>.jsonl` for bytes saved.
3. Report:
   - Compression mode: `strict` | `audit`
   - Output level: `lite` | `strict` | `ultra`
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
3. Pipe content to: `${CLAUDE_PLUGIN_ROOT}/bin/run compress --mode=<mode>`
4. Save the original file as `<stem>.original.md` (backup).
5. Write the compressed output to the original file path.
6. Report: filename, original size, compressed size, ratio, tokens saved estimate.

### Red Flags

- Compressing a file inside a code repo's source tree (not CLAUDE.md/DESIGN.md/BUILD.md) — ask first.
- Compressing a file that has no .original.md backup and is already very short (<500 bytes) — skip, not worth it.
- Losing TODO/FIXME/SECURITY markers after compression — bug, report immediately.
- Making an LLM call during compression — wrong. Compression is deterministic only.
- Setting output level without confirming the change took effect — verify by checking settings.json after write.
