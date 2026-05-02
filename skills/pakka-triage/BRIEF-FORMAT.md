# Agent Brief Format

An agent brief is posted as a comment when an issue moves to `ready-for-agent`. It is the authoritative spec an AFK agent works from. Issue body and discussion are context — the brief is the contract.

## Principles

**Durable, not precise.** The issue may sit for weeks. Codebase will change. Write so the brief stays useful as files are renamed or refactored.

- Describe interfaces, types, behavioral contracts — not file paths or line numbers
- Name specific types, function signatures, or config shapes the agent should look for
- Describe WHAT the system should do, not HOW to implement it

**Behavioral, not procedural.**
Good: "The order service should accept partial cancellation and emit a cancellation event."
Bad: "Open the order handler and add a switch statement."

**Complete acceptance criteria.** Every criterion independently verifiable by the agent without human confirmation.
Good: "Running `order cancel --partial <id>` with a valid order ID returns 200 and the event appears in the log."
Bad: "Cancellation should work correctly."

**Explicit scope.** State what is out of scope. Prevents gold-plating.

## Template

```markdown
## Agent Brief

**Category:** bug / enhancement
**Summary:** one-line description of what needs to happen

**Current behavior:**
What happens now. For bugs: the broken behavior. For enhancements: the status quo.

**Desired behavior:**
What should happen after the work is done. Be specific about edge cases and error conditions.

**Key interfaces:**
- `TypeOrFunctionName` — what needs to change and why
- Config shape — any new options needed

**Acceptance criteria:**
- [ ] Specific, verifiable criterion 1
- [ ] Specific, verifiable criterion 2

**Out of scope:**
What this brief does NOT cover.
```
