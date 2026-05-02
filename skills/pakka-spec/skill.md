---
name: spec
description: Write the spec first. Synthesizes problem statement, user stories, module decisions, and test strategy from conversation context — then publishes to the issue tracker. Code without a spec is context waste from line one. Use when formalizing a feature or handing off work to an agent.
allowed-tools: Bash, Read, Glob, Grep, Agent
argument-hint: "[optional: additional context or constraints]"
user-invocable: false
---

## Thesis

Code without a spec is context waste from the first line. A spec forces "what are we actually building?" before tokens are spent on the wrong thing. Synthesize from what's already in the conversation — don't re-interview the user.

## Process

### 1. Explore the codebase

If not already done, read the current state. Use project's domain vocabulary throughout (`CONTEXT.md`). Respect ADRs in the area.

### 2. Identify modules

Sketch major modules to build or modify. Actively look for deep module opportunities — large behavior behind small, testable interfaces.

Show list to user. Confirm:
- Matches expectations?
- Which modules need tests written?

### 3. Write and publish

Write using template below. Publish to issue tracker with `needs-triage` label.

```markdown
## Problem Statement

The problem the user faces, from their perspective.

## Solution

The solution, from the user's perspective.

## User Stories

Numbered list. Be extensive — cover all aspects.

1. As a [actor], I want [feature], so that [benefit].

## Implementation Decisions

- Modules to build or modify
- Interface changes
- Architectural decisions
- Schema changes
- API contracts

Do NOT include file paths or code snippets — they go stale.

## Testing Decisions

- What makes a good test in this area (behavior, not implementation)
- Which modules will be tested
- Prior art: similar test patterns already in the codebase

## Out of Scope

What this spec does NOT cover.

## Notes

Any further context.
```

## Red Flags

- Interviewing the user before writing → wrong. Synthesize first; user reviews, doesn't author.
- File paths or line numbers in the spec → they go stale within weeks. Describe interfaces and behavior.
- Vague user stories ("user can use the feature") → wrong. Every story needs actor, feature, and benefit.
- Mixing implementation details into Problem Statement → wrong. Keep user perspective in that section.
- Publishing without domain vocabulary → wrong. Use CONTEXT.md terms throughout.
