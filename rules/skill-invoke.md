## Skill Auto-Invocation

When the available skills list is non-empty, invoke the matching skill automatically — do not wait for the user to type the command.

| Trigger | Invoke |
|---|---|
| "build X", "implement X", "add feature X", "ship X", "I want X" | `/pakka:spec` first — spec before code |
| "debug", "fix this bug", "broken", "failing", "throwing", "not working", "performance regression" | `/pakka:debug` — build feedback loop before touching code |
| "write tests", "TDD", "test first", "red-green-refactor", "test-driven" | `/pakka:tdd` |
| "review architecture", "too much coupling", "hard to test", "messy", "untestable", "refactor this" | `/pakka:review-architecture` |
| "stress test my plan", "challenge this", "poke holes in this", "what am I missing" | `/pakka:challenge` |
| "probe me", "question my design", "grill me", "what did I miss" | `/pakka:probe` |
| "how does X work", "explain this module", "I don't know this code", "map this" | `/pakka:map` first — map before navigate |
| "break into tickets", "create issues", "make a plan into tickets", "slice this" | `/pakka:slice` |
| "triage", "look at issue", "what needs attention", "review issues" | `/pakka:triage` |
| "write a PRD", "write a spec", "requirements doc", "product spec" | `/pakka:spec` |
| "write a skill", "add a skill", "new skill for library" | `/pakka:skill` |
| "protect git", "block force push", "add git guardrails" | `/pakka:guard` |

## Hard rule

Never start implementing a new feature or significant behavior change without first running `/pakka:spec`. If the user asks to "just build it" without a spec, acknowledge and run `/pakka:spec` anyway — spec is one prompt, implementation is many.
