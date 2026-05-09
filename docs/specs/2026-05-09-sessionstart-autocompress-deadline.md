# SessionStart auto-compress deadline fix
Date: 2026-05-09
Status: draft

## Problem
`autoCompressContextFiles` runs synchronously in the SessionStart hook at line 97 of `compress_cmd.go`, before `forkOrchestrator` at line 104. It performs ReadDir + ReadFile + compress.Run + WriteFile for every CLAUDE.md/DESIGN.md/BUILD.md found in CWD and one level of subdirectories. On a project with several context files this can exceed 50ms — the hard deadline for SessionStart hooks documented in the same code. Exceeding the deadline causes Claude Code to log a warning and degrade hook reliability.

## User stories
- As a pakka user, I want SessionStart to complete within 50ms so that the hook never causes Claude Code warnings or degraded performance on session open.
- As a pakka developer, I want context file compression to run in a detached child process so that the SessionStart path is bounded and predictable regardless of project size.

## Module decisions
- Remove the synchronous `autoCompressContextFiles` call from the SessionStart handler (compress_cmd.go line 97).
- Add `--auto-context=<dir>` flag to the `pakka-core compress` subcommand; when present, the process runs `autoCompressContextFiles(dir, "strict", sessionID)` and exits.
- In the SessionStart handler, spawn a detached child (`exec.Command` with `SysProcAttr{Setsid: true}`) running `pakka-core compress --auto-context=<cwd> --session=<sessionID>` before `forkOrchestrator`. Child is fire-and-forget.
- Use the existing `forkDetached` helper (or equivalent in `cmd/pakka-core`) to avoid duplicating detach logic. If no helper exists, inline the same `os/exec` + `SysProcAttr` pattern used by `forkOrchestrator`.
- `autoCompressContextFiles` logic itself is unchanged — only the call site moves.
- Both the auto-context child and the orchestrator child run concurrently; SessionStart returns after spawning both.

## Acceptance criteria
1. `go test -run TestSessionStartDeadline ./cmd/pakka-core/...` passes: a test that calls the SessionStart handler and asserts it returns in ≤50ms on a project with 5 context files.
2. After SessionStart returns, `autoCompressContextFiles` output (backup files, meter entries) eventually appears on disk — verified by a test that waits up to 2s for the child to write the backup.
3. `autoCompressContextFiles` is no longer called synchronously anywhere in the SessionStart code path — verified by `grep -n "autoCompressContextFiles" cmd/pakka-core/compress_cmd.go` showing no call before the `return` on the SessionStart branch.
4. `go build ./...` exits 0.
5. `go test ./...` exits 0.

## Out of scope
- Changing the compression mode used by auto-context (stays strict/deterministic).
- Merging auto-context child with orchestrator child (different modes, keep separate).
- Per-file concurrency inside `autoCompressContextFiles`.
- Windows support for `Setsid` (document as Linux/macOS only, consistent with existing `forkOrchestrator`).

## Open questions
