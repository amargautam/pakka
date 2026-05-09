# Compress orchestrator — user edit preservation on level change
Date: 2026-05-09
Status: implemented

## Problem
When a user edits the live compressed file (e.g. CLAUDE.md) and then changes the compression level, the orchestrator silently overwrites those edits. The root cause: `sourceSHA` is computed from the `.original.md` snapshot, not the live file. On a level change, `state.Stale()` returns true, `origBytes` is read from the snapshot, and the result is atomically written over the live file — the user edits are permanently lost with no warning and no recovery path.

A naive fix (`sha256Hex(live) != sha256Hex(origBytes)`) fails because after successful compression the live file IS different from the snapshot by design. The discriminator must compare the live file against the last compression OUTPUT, not the original input.

## User stories
- As a pakka user, I want my edits to CLAUDE.md to survive a compression level change so that switching from ultra to super-ultra does not silently discard my work.
- As a pakka developer, I want the orchestrator to adopt user-edited content as the new compression baseline so that the next compression run starts from the correct source.

## Module decisions
- Add `OutputSHA string` field to `Entry` struct in `internal/compress/orchestrator/state.go` (JSON key `outputSHA`; empty string for legacy entries — treated as "no prior output known, skip user-edit check").
- Extend `State.Record()` signature: add `outputSHA string` parameter after `sourceSHA`. All call sites in orchestrator.go must pass `sha256Hex([]byte(out))` after successful `atomicWrite`.
- Add `State.GetOutputSHA(absPath string) string` method — returns empty string for unknown entries (safe fallback for legacy state).
- In `processOne`, after reading `origBytes` from existing snapshot: call `GetOutputSHA(abs)`; read live file; if `outputSHA != ""` and `sha256Hex(live) != outputSHA`, the user has edited the live file — call `atomicWrite(originalPath, live)` and set `origBytes = live`.
- If snapshot refresh fails (e.g. write error), log and continue with the old `origBytes` — do not abort the compression pass.
- OutputSHA is only set on validator-passing compressions (when `validatorPasses=true`); failed/partial compressions leave OutputSHA unchanged.

## Acceptance criteria
1. `go build ./...` exits 0 after all changes.
2. `go test ./internal/compress/orchestrator/...` exits 0 with all existing tests passing.
3. New test `TestUserEditPreservedOnLevelChange`: compress file at level A, manually overwrite live file with edited content, run orchestrator at level B, verify compression input equals the edited content (not the original snapshot). Test must pass.
4. New test `TestNoEditNoSnapshotRefresh`: compress file at level A, do NOT edit live file, run orchestrator at level B, verify compression input equals the original snapshot (no spurious snapshot refresh). Test must pass.
5. Legacy state entries (no `outputSHA` field) do not trigger snapshot refresh — verified by a test that creates a state file without the `outputSHA` field and confirms orchestrator runs without error.
6. `State.Record()` call in `processOne` passes `sha256Hex([]byte(out))` as `outputSHA` — verified by grep: `grep -c "sha256Hex" internal/compress/orchestrator/orchestrator.go` returns ≥ 2.

## Out of scope
- Merging user edits with original content (take live file wholesale as new baseline).
- User-facing stderr warning when edit detected (log-only via o.logf).
- Detecting edits when OutputSHA is empty (legacy state: skip check, no action).
- Three-way merge between snapshot, compressed output, and user edits.
- Windows-specific file locking behavior.

## Open questions
