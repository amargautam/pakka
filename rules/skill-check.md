<EXTREMELY_IMPORTANT>
YOU MUST invoke the relevant pakka command before ANY response when the signal matches. This is not optional. This is not a suggestion. You cannot rationalize your way out of it.

IF TASK SIGNAL MATCHES, YOU DO NOT HAVE A CHOICE. INVOKE THE COMMAND FIRST.

## Trigger signals

### /pakka:plan — design, spec, probe, challenge, slice
TRIGGER when user message contains ANY of:
- design, spec, plan, approach, architecture, structure, "how should we", "what should we", "should we", "let's build", "we need to", "thinking about", "considering", "what about", "how about", "proposal", challenge, probe, decompose, slice, tickets

TRIGGER when the message is a question about direction, not execution.

### /pakka:build — TDD, debug, map, audit
TRIGGER when user message contains ANY of:
- implement, fix, debug, add, refactor, TDD, test, "not working", broken, error, "build this", "write the code", "make it work", "how does", "walk me through", "explain this", coupling, "hard to test"

### /pakka:review — verify, audit, finish
TRIGGER when user message contains ANY of:
- verify, check, audit, "is this right", finalize, review, done, "looks good", ship, "ready to", "approve", "sign off", receive, feedback

## Rules

1. If signal matches: invoke the command BEFORE writing any response. No exceptions.
2. If 1% chance a command applies: invoke it. Over-invoking is better than skipping.
3. If in doubt between plan/build: default to /pakka:plan.
4. Invoked command turns out to be wrong? Stop early — still better than skipping.
5. SUBAGENTS: skip this rule. You were dispatched for a specific task — execute it.
</EXTREMELY_IMPORTANT>
