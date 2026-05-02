---
name: pakka-guard
description: Wire a PreToolUse hook that intercepts force-push, hard-reset, branch deletion, and clean before Claude can run them. Irreversible git operations are high blast-radius; this hook makes them impossible without explicit human approval. Use when locking down a repo against Claude-initiated destructive operations.
allowed-tools: Read, Write, Edit, Bash
argument-hint: "[--project | --global]"
user-invocable: true
---

## What gets blocked

- `git push` (all variants including `--force`)
- `git reset --hard`
- `git clean -f` and `git clean -fd`
- `git branch -D`
- `git checkout .` and `git restore .`

When blocked, Claude sees: `"Blocked: you do not have authority to run this command."`

## Steps

### 1. Determine scope

Ask: **project only** (`.claude/settings.json`) or **all projects** (`~/.claude/settings.json`)?

### 2. Write the hook script

```bash
#!/usr/bin/env bash
# pakka-guard: block dangerous git commands in Claude Code

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // ""')

BLOCKED_PATTERNS=(
  "git push"
  "git reset --hard"
  "git clean -f"
  "git branch -D"
  "git checkout \."
  "git restore \."
)

for pattern in "${BLOCKED_PATTERNS[@]}"; do
  if echo "$COMMAND" | grep -qE "$pattern"; then
    echo "Blocked: you do not have authority to run: $COMMAND" >&2
    exit 2
  fi
done

exit 0
```

Write to:
- Project: `.claude/hooks/pakka-guard.sh`
- Global: `~/.claude/hooks/pakka-guard.sh`

Make executable: `chmod +x <path>`

### 3. Merge into settings

Never overwrite — merge into existing settings file.

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "<absolute-path-to-script>"
          }
        ]
      }
    ]
  }
}
```

### 4. Ask about customization

Offer to add or remove patterns. Edit script accordingly.

### 5. Verify

```bash
echo '{"tool_input":{"command":"git push origin main"}}' | <path-to-script>
```

Should exit 2 and print BLOCKED message to stderr.

## Red Flags

- Overwriting existing settings file instead of merging → data loss. Merge only.
- Script not made executable → hook won't run. `chmod +x` is mandatory.
- Using `exit 1` instead of `exit 2` to block → wrong exit code. Claude interprets exit 2 as "blocked by hook."
- Installing globally without confirming scope first → ask first.
- Adding patterns that block read-only commands (`git status`, `git log`, `git diff`) → never block diagnostic commands.
