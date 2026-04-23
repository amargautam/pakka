# CLAUDE.md — pakka plugin

## Mission
Ship a Claude Code plugin that visibly (a) spends fewer tokens, (b) catches bugs raw Claude Code misses, (c) proves both via `make bench`. Three numbers. Apache-2.0.

## Rules
- Terse. One word when it works.
- JSON everywhere we author. YAML only in Claude Code frontmatter. MD for skill/agent/command bodies.
- Go stdlib-first. Deps only if stdlib is awful at the job.
- Every skill/agent has a Red Flags section. No exceptions.
- Every change runs `/pakka:eval` before commit.
- No claim without a benchmark. No benchmark without a commit hash.
- Deny-by-default stays deny-by-default. Expanding the allow list requires a threat note.

## Out of scope for v0
Cross-session memory. Multi-harness sync. Dashboards. Anything pretty.
