# Spec: Fat main.go Extraction
**Date:** 2026-05-02
**Status:** approved
**Ships in:** v0.2.0

---

## Problem

`cmd/pakka-core/main.go` is 1625 lines, 45 functions. All 14 subcommands dispatch via a flat `switch` in `main()` with their implementations in the same file. This makes each command hard to test in isolation, creates coupling between unrelated commands, and makes `main.go` difficult to navigate.

---

## Goal

Extract each subcommand into its own file with a consistent interface. `main.go` becomes a thin dispatcher (~50 lines). Each command is independently testable.

---

## Command interface pattern

```go
// Command is the interface every subcommand implements.
type Command interface {
    Name() string
    Run(args []string) error
}
```

Each command:
- Lives in `cmd/pakka-core/<name>.go`
- Implements `Command`
- Has its own `<name>_test.go`
- Does NOT import or call other command files

`main.go` after extraction:
```go
func main() {
    cmds := []Command{
        &CompressCmd{},
        &MeterCmd{},
        &GuardCmd{},
        // ... all 14
    }
    // dispatch by os.Args[1]
}
```

---

## Extraction map

| Subcommand | Current location | Target file |
|---|---|---|
| `compress` | `main.go:runCompress()` + helpers | `compress_cmd.go` |
| `meter` | `main.go:runMeter()` | `meter_cmd.go` |
| `guard` | `main.go:runGuard()` | `guard_cmd.go` |
| `commit-gate` | `main.go:runCommitGate()` | `commit_gate_cmd.go` |
| `stack-detect` | `main.go:runStackDetect()` | `stack_detect_cmd.go` |
| `stack-gate` | `main.go:runStackGate()` | `stack_gate_cmd.go` |
| `eval` | `main.go:runEval()` + helpers | `eval_cmd.go` |
| `report` | `main.go:runReport()` | `report_cmd.go` |
| `bench` | `main.go:runBench()` | `bench_cmd.go` |
| `audit` | `main.go:runAudit()` | `audit_cmd.go` |
| `status-line` | `main.go:runStatusLine()` | `status_line_cmd.go` |
| `help` | `main.go:runHelp()` | `help_cmd.go` |
| `output-rules` | `main.go:runOutputRules()` | `output_rules_cmd.go` |
| `output-reinforce` | `main.go:runOutputReinforce()` | `output_reinforce_cmd.go` |
| `orchestrator-status` | `orchestrator.go:runOrchestratorStatus()` | stays in `orchestrator.go` |
| `install-git-hook` | `main.go` | `install_git_hook_cmd.go` |

**Shared helpers** (used by multiple commands) move to `helpers.go`:
- `parseStrict`, `parseLenient`
- `debugLogf`
- `loadSettings`, `loadOutputLevel`, `resolveOutputLevel`
- `pluginRoot`, `claudeConfigDir`

---

## What stays in main.go

```go
package main

func main() {
    // register commands
    // match os.Args[1]
    // dispatch
    // handle unknown subcommand
}
```

~50 lines maximum.

---

## TDD approach

One command at a time. For each extraction:

1. Write test for `<Name>Cmd.Run()` via public interface — exercise the behavior, not the implementation
2. RED: function doesn't exist on the struct yet
3. Move implementation from `main.go` into the new file
4. GREEN: test passes
5. Remove the `run<Name>()` function from `main.go`, update `main()` switch to use `<Name>Cmd.Run()`
6. `go test ./cmd/pakka-core/... -count=1` — all pass before moving to next command

Order (lowest risk first, highest coupling last):
1. `meter` — simplest, reads stdin + writes file
2. `help` — reads binary output, pure display
3. `report` — reads data, formats
4. `guard` — PreToolUse, well-tested already
5. `commit-gate` — PreToolUse, well-tested
6. `stack-detect` — reads cwd
7. `stack-gate` — PostToolUse
8. `audit` — PostToolUse
9. `status-line` — reads multiple config sources
10. `output-rules` + `output-reinforce` — simple output
11. `eval` — has helpers
12. `bench` — has helpers
13. `install-git-hook` — file writes
14. `compress` — largest, most helpers, extract last

---

## Constraints

- `go test ./... -count=1` green after every single extraction
- No behavior change — this is a pure structural refactor
- Shared helpers extracted to `helpers.go` before any command that needs them
- `orchestrator.go` and its tests are not touched — already extracted
- `output_test.go` tests remain valid throughout

---

## Acceptance criteria

- [ ] `main.go` ≤ 80 lines
- [ ] Each subcommand in its own `<name>_cmd.go` file
- [ ] Each command file has a corresponding `<name>_cmd_test.go` with at least one behavioral test
- [ ] `go test ./... -count=1` passes throughout (no red intermediate states pushed)
- [ ] `parseStrict`, `parseLenient`, `debugLogf`, `loadSettings` in `helpers.go`
- [ ] No functional behavior change — verified by existing test suite

## Out of scope

- Changing subcommand names or CLI interface
- Adding new subcommands
- Moving commands to separate packages (stays in `package main`)
