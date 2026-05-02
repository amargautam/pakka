---
name: init
description: One-time pakka setup. Detects stack, writes stack overlay, verifies permissions and hooks work.
allowed-tools: Read, Write, Edit, Bash, Glob, Grep
argument-hint: "[--force]"
user-invocable: false
---

## Instructions

### 1. Check for existing setup

Look for `.pakka/stack.json` in the project root.
- If it exists and `--force` was NOT passed: print current stack config, tell the user to use `--force` to re-run. Stop.
- If it exists and `--force` was passed: continue (will overwrite).
- If it does not exist: continue.

### 2. Detect stack

Run `${CLAUDE_PLUGIN_ROOT}/bin/run stack-detect` from the project root. It outputs JSON:

```json
{
  "stacks": ["typescript", "go"],
  "package_manager": "npm",
  "test_command": "npm test",
  "lint_command": "npx eslint .",
  "format_command": "npx prettier --write .",
  "has_tsconfig": true,
  "has_eslint": true,
  "has_prettier": true,
  "monorepo": false
}
```

If detection fails or returns empty stacks, ask the user what stack they use.

### 3. Confirm with user

Show the detected stack and ask for confirmation:

```
pakka detected:
  stack:     typescript (npm)
  test:      npm test
  lint:      npx eslint .
  format:    npx prettier --write .

Correct? (y to confirm, or type corrections)
```

Wait for user response before writing any config.

### 4. Write stack config

Write `.pakka/stack.json` with the confirmed values:

```json
{
  "stacks": ["typescript"],
  "test_command": "npm test",
  "lint_command": "npx eslint .",
  "format_command": "npx prettier --write .",
  "created": "2025-01-15T10:00:00Z"
}
```

### 5. Write settings overlay

Read `.claude/settings.local.json` if it exists. **Merge** the stack overlay into it — never replace.

The overlay adds stack-specific `allow` permissions. Per detected stack:

**TypeScript/JavaScript (npm/yarn/pnpm/bun):**
```json
{
  "permissions": {
    "allow": [
      "Bash(npm test*)",
      "Bash(npm run lint*)",
      "Bash(npm run build*)",
      "Bash(npx eslint*)",
      "Bash(npx prettier*)",
      "Bash(npx tsc*)"
    ]
  }
}
```

**Go:**
```json
{
  "permissions": {
    "allow": [
      "Bash(go test*)",
      "Bash(go build*)",
      "Bash(go vet*)",
      "Bash(go run*)",
      "Bash(golangci-lint*)"
    ]
  }
}
```

**Python (pip/poetry/uv):**
```json
{
  "permissions": {
    "allow": [
      "Bash(python -m pytest*)",
      "Bash(pytest*)",
      "Bash(ruff check*)",
      "Bash(ruff format*)",
      "Bash(mypy*)"
    ]
  }
}
```

**Rust:**
```json
{
  "permissions": {
    "allow": [
      "Bash(cargo test*)",
      "Bash(cargo build*)",
      "Bash(cargo clippy*)",
      "Bash(cargo fmt*)"
    ]
  }
}
```

For other/unknown stacks, ask the user for their test/lint/format commands and generate matching allow entries.

### 6. Verify setup

Run a quick verification:
1. Check that `settings.local.json` was written and is valid JSON.
2. Check that `.pakka/stack.json` exists and is valid JSON.
3. Print summary:

```
pakka init complete.
  stack:     typescript (npm)
  overlay:   .claude/settings.local.json (4 permissions added)
  stack cfg: .pakka/stack.json

Next: make a change and commit — pakka will auto-review.
Run /pakka:help to see what's active.
```

## Red Flags

- Inferred stack but wrote config without confirming with user → ask before write.
- Overwrote user's existing `.claude/settings.local.json` without merging → merge, never replace.
- Enabled network allow for wide domain → deny, ask, or scope narrower.
- Added `allow` entry for a command that could be destructive (rm, docker rm, etc.) → never.
- Detected monorepo but only configured one stack → warn user, offer to configure each workspace.
