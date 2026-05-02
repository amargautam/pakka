---
description: Implementation hub — TDD, debug, map, or architecture audit. Infers mode from context. Checks for spec before starting.
allowed-tools: Read, Write, Bash, Edit
argument-hint: "[task description or no argument]"
---
## Instructions
### 1. Spec approval gate (always runs first)
Before any implementation:
1. Scan `docs/specs/` — find files whose name or first heading matches current task topic
2. If match found: show `filename + first heading + age in days` → ask **"Use this spec? [y/n]"** — stop and wait
3. If user says yes: load spec. Its acceptance criteria are hard constraints throughout — do not deviate
4. If user says no or no match found: ask "Should I run `/pakka:plan` first?" — do not silently proceed without spec when one seems needed
5. If user says no spec needed: proceed without one
---
### 2. Infer mode from context
| Signal | Mode |
|--------|------|
| "write tests", "TDD", "test first", "red green" | **tdd** |
| "broken", "failing", "debug", "fix this bug", "not working", "error" | **debug** |
| "how does X work", "explain this module", "I don't know this code", "walk me through" | **map** |
| "coupling", "hard to test", "architecture", "refactor", "too much in one place" | **audit** |
| No clear signal | ask once: "What are we building?" then infer from answer |
---
### Mode: tdd
One failing test → minimal code → repeat. Vertical slices only.
Rules:
- Write ONE test. Watch it fail. Write minimal code to pass. Repeat.
- Never write all tests then all code.
- Tests assert on observable behavior through public interfaces only — never private methods or internals.
- Only enough code to pass current test. No speculative code for future tests.
- After all tests pass: refactor. Never refactor while RED.
---
### Mode: debug
Build deterministic feedback signal first. No fixes without reproduction.
1. Reproduce failure — get reliable fail/pass signal (test, command, log line)
2. Hypothesize — rank causes by probability, one line each
3. Instrument — change ONE variable at time
4. Fix — minimal change that makes signal green
5. Regression test — add test that would have caught this
Never change multiple things at once. If signal won't reproduce, that IS first problem to solve.
---
### Mode: map
Map before navigating. Read widely before touching anything.
1. Find all modules relevant to current task — callers, dependencies, related types
2. Produce one-view summary: what each module does, how they connect
3. Identify right insertion point before writing line
4. Only then proceed to implementation (switch to tdd or debug mode)
---
### Mode: audit
Find shallow modules. Propose targeted refactors.
shallow module: its interface costs as much to understand as reading implementation. Signs: thin wrappers, pass-through functions, one-line methods that call another.
For each finding:
- Name module
- State coupling or testability cost
- Propose concrete refactor (merge, inline, deepen interface)
Do not refactor during audit. Produce proposal. User approves before changes.
---
### 3. Verification gate (runs before claiming done)
Before outputting "done", "working", "fixed", "passing", or any completion claim:
1. Run relevant test/lint/build command
2. Show actual exit code and output
3. Exit code 0 = evidence. "Should work" is not evidence.
4. If exit code ≠ 0: fix before claiming completion
---
## Red Flags
- Skipping spec approval gate → wrong. Always check `docs/specs/` first.
- Claiming done without showing exit code → wrong. Run command, show result.
- Writing multiple tests before any implementation → horizontal slicing. One test at time.
- Speculative code "for next feature" → delete it.
- Refactoring during RED test cycle → wrong. GREEN first, always.
