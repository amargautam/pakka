---
description: One-time environment setup — stack detection, permissions overlay, git guard hook.
allowed-tools: Bash, Read, Write
argument-hint: "[guard]"
---

## Instructions

### Check additionalContext

Check `additionalContext` for `PAKKA HOOK HANDLED`. If present, output verbatim and stop.

---

### Route by argument

**No argument → run init**

1. Run `${CLAUDE_PLUGIN_ROOT}/bin/run stack-detect` from project root
2. Show detected stack and proposed permissions overlay
3. Ask for confirmation before writing any config
4. On confirmation: write stack config + permissions overlay per detected stack
5. Confirm: "pakka initialised for <stack>."

**`guard` → install git guard hook**

1. Ask: project only (`.claude/settings.json`) or all projects (`~/.claude/settings.json`)?
2. Write the PreToolUse hook that blocks: `git push --force`, `git reset --hard`, `git clean -f`, `git branch -D`
3. Confirm: "Guard hook installed in <scope>."

---

## Red Flags

- Writing config without user confirmation → wrong. Always confirm first.
- Running init twice without `--force` → warn, show current config, stop.
