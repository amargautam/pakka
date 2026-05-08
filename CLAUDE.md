# CLAUDE.md — pakka plugin

## Mission
Ship Claude Code plugin: (a) fewer tokens, (b) catches bugs raw Claude Code misses, (c) proves both via `RECEIPTS.md` and gate enforcement. Apache-2.0.

## Rules
- Terse. One word when it works.
- JSON where authored. YAML only in Claude Code frontmatter. MD for skill/agent/command bodies.
- Go stdlib-first. Deps only if stdlib is awful.
- Every skill/agent has Red Flags section. No exceptions.
- Every change runs `/pakka:eval` before commit.
- No claim without benchmark. No benchmark without commit hash.
- Deny-by-default stays. Expanding allow list requires threat note.

## Out of scope for v0
Cross-session memory. Multi-harness sync. Dashboards. Anything pretty.