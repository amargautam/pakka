# pakka recall — section 6 spec adjustments
Date: 2026-05-02
Status: draft

## Problem

Section 6 (pakka recall) was written before verifying two Claude Code specifics: (1) where plugin persistent data should live, and (2) whether a SessionEnd hook exists for final index flush. Without these fixes the recall.db path would be wrong and the last session's data would only be indexed on the *next* session start.

## User stories

- As a user, I want `/pakka:recall` to find events from my current session immediately after it ends, not only after the next session starts.
- As a user, I want `recall.db` to survive plugin updates without path changes.

## Module decisions

- `tool_response` field in hookevent.go — verified correct. Non-zero `output_size` in live audit JSONL confirms it works. No change.
- `${CLAUDE_PLUGIN_DATA}` resolves to `~/.claude/plugins/data/pakka-pakka-marketplace/` — persistent across plugin updates. Use this for `recall.db`, not `~/.pakka/`.
- Binary resolves path via env var `CLAUDE_PLUGIN_DATA` when set; falls back to `~/.pakka/` for local dev / missing env.
- `SessionEnd` hook exists (confirmed in Claude Code docs). Wire `pakka-core index` there in addition to SessionStart.
- `SessionEnd` index run is the same idempotent call — no special end-of-session logic needed.

## Acceptance criteria

1. `recall.db` is created at `$CLAUDE_PLUGIN_DATA/recall.db` when `CLAUDE_PLUGIN_DATA` env var is set.
2. `recall.db` falls back to `~/.pakka/recall.db` when `CLAUDE_PLUGIN_DATA` is unset.
3. `SessionEnd` hook fires `pakka-core index` and new session entries are queryable before the next session starts.
4. Running `pakka-core index` twice in sequence (SessionStart then SessionEnd on same files) inserts 0 duplicate rows.
5. `tool_response` field — no code change. Existing audit output_size values confirm it works.

## Out of scope

- Migrating existing `~/.pakka/recall.db` to new path — users on v0.3.0 start fresh; no migration.
- Changing any other hook event field names.

## Open questions

None. Proceed to build.
