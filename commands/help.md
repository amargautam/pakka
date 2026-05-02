---
description: Show pakka status — what's on, what you can run.
allowed-tools: Bash, Read
---

## Instructions

Check `additionalContext` for a line starting with `PAKKA HOOK HANDLED: help`.

- If found: output everything after that line verbatim — stop, no tool calls.
- If NOT found: run the following command and print its stdout verbatim, with no additional commentary:

```
${CLAUDE_PLUGIN_ROOT}/bin/run help
```

If the command fails, print the stderr output and stop.

## Red Flags

- Paraphrasing or reformatting the binary's output → wrong. Print stdout verbatim so the user sees the canonical status.
- Running additional commands to "augment" the output → wrong. This wrapper is one binary call, nothing more.
- Swallowing the binary's stderr on failure → wrong. Surface it so the user can debug.
