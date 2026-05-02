## Skill Auto-Invocation

Before each response, check whether the message calls for one of the three hub commands. If yes, invoke it before anything else.

| Signal | Invoke |
|---|---|
| "build X", "implement X", "add feature", "ship X", "spec", "design", "PRD", "probe me", "challenge this", "break into tickets" | `/pakka:plan` — design hub |
| "write tests", "TDD", "debug", "fix this bug", "broken", "failing", "architecture", "too much coupling", "hard to test", "how does X work", "map this" | `/pakka:build` — implementation hub |
| "done?", "ship?", "ready to merge?", "they said", "feedback says", "merge?", "land this?" | `/pakka:review` — quality hub |
| "triage", "look at issue", "what needs attention" | `/pakka:triage` |

## Hard rule

Never start implementing a new feature or significant behavior change without running `/pakka:plan` first. If the user asks to "just build it" without a spec, acknowledge and run `/pakka:plan` anyway.
