---
description: Search pakka's audit trail across sessions via FTS5 full-text index.
allowed-tools: Bash
---

## Instructions

Run `pakka-core query` with the user's query arguments and format the results.

**No arguments** (`/pakka:recall`):
```
${CLAUDE_PLUGIN_ROOT}/bin/run query
```
Show the last 10 indexed entries as a markdown table with columns: `ts | kind | file_path | preview`.

**With query** (`/pakka:recall <query>`):
```
${CLAUDE_PLUGIN_ROOT}/bin/run query <query args>
```
Show matching entries as a markdown table with columns: `ts | kind | file_path | preview`.

Each line of output from `pakka-core query` is a JSON object:
```json
{"session_id":"...","ts":"...","kind":"...","file_path":"...","preview":"...","score":0.0}
```

Parse each line and render as a markdown table row. If the command produces no output, respond: "No matching entries found in audit index."

If the command fails, print the stderr and stop.

## Red Flags

- Fabricating entries not present in the binary's output → wrong. Only show what the index returns.
- Skipping the table format and dumping raw JSON → wrong. Parse and render as markdown.
- Running `pakka-core index` before querying — unnecessary; index runs at SessionEnd automatically.
- Modifying the query text before passing to the binary → wrong. Pass the user's words verbatim.
