---
name: pakka-tdd
description: One failing test → minimal code → repeat. Vertical slices only — never write all tests then all code. Tests assert on observable behavior through public interfaces; implementation can change completely without breaking them. Use when building features test-first or applying red-green-refactor discipline.
allowed-tools: Bash, Read, Edit, Write, Glob, Grep
argument-hint: "[feature or behavior to implement]"
user-invocable: true
---

## Thesis

Tests that verify behavior survive refactors. Tests that verify implementation rot. Every false alarm burns tokens tracing dead ends — implementation-coupled tests are context waste disguised as quality.

Good test: reads like a spec. "Checkout with empty cart returns 400."
Bad test: breaks on internal function rename when behavior is unchanged.

## Anti-pattern: Horizontal Slicing

Never write all tests, then all code:

```
WRONG:  RED: test1 test2 test3 → GREEN: impl1 impl2 impl3
RIGHT:  test1 → impl1  ·  test2 → impl2  ·  test3 → impl3
```

Horizontal slices produce tests that check imagined behavior, not actual behavior. Outrunning your headlights → wrong test structure before understanding the implementation.

## Workflow

### 1. Plan

Before any code:
- [ ] Confirm interface changes with user
- [ ] List behaviors to test — not implementation steps
- [ ] Identify deep module opportunities: small interface, large implementation
- [ ] Get user approval on the list

Ask: "What should the public interface look like? Which behaviors matter most?"

Can't test everything. Confirm critical path with user.

### 2. Tracer bullet

Write ONE test for ONE behavior:
```
RED:   write test → watch it fail
GREEN: minimal code to pass → watch it pass
```
Proves test infrastructure works end-to-end before adding more tests.

### 3. Incremental loop

For each remaining behavior:
```
RED → GREEN
```

Rules:
- One test at a time
- Only enough code to pass current test
- Never anticipate future tests
- Test through public interfaces only — never private methods or internals

### 4. Refactor

After all tests pass:
- [ ] Extract duplication
- [ ] Deepen modules — move complexity behind simpler interfaces
- [ ] Run tests after each refactor step

Never refactor while RED. Get to GREEN first.

## Checklist per cycle

```
[ ] Test names a behavior, not an implementation detail
[ ] Test uses public interface only
[ ] Test survives a complete internal refactor
[ ] Code written is minimal for this test only
[ ] No speculative code added for future tests
```

## Red Flags

- Writing multiple tests before any impl → horizontal slicing. One test at a time.
- Test fails on internal rename when behavior is unchanged → wrong seam. Test via public interface only.
- Mocking internal collaborators → impl coupling. Mock only at system boundaries (external services, I/O, clocks).
- Refactoring while RED → wrong order. GREEN first, always.
- Speculative code "for the next test" → delete it. Code only what the current failing test demands.
- Test reads as implementation verification ("calls X with args Y") → wrong. Assert on observable output or state change.
