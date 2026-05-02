---
description: Quality gate hub — verify, review, receive feedback, or finish branch. Infers mode from context.
allowed-tools: Agent, Bash, Read
argument-hint: "[--base=<ref>] [--install-hook] [--receive] [--finish]"
---

## Instructions

### Hook pre-handling

Check `additionalContext` for `PAKKA HOOK HANDLED`. If present, output verbatim and stop.

---

### 1. Infer mode from context

| Signal | Mode |
|--------|------|
| No signal, "done?", "ship?", "PR?", "ready to merge?" | **verify → review** (verification first, then code review) |
| "they said", "feedback says", "reviewer commented", "they want me to" | **receive** — handle incoming review feedback |
| "merge?", "land this?", "finish branch?", "close this out?" | **finish** — structured branch landing |
| `--install-hook` in args | install prepare-commit-msg hook, stop |

---

### Mode: verify → review

**Verification gate (mandatory, runs before review):**

1. Run the relevant test/lint/build command for the current project
2. Show actual exit code and output
3. If exit code ≠ 0: stop — do not review broken code. Fix first.
4. Only if exit code = 0: proceed to code review

**Code review (runs after verification passes):**

1. **Get the diff.** If `--base=<ref>` provided: `git diff <ref>...HEAD`. Otherwise: `git diff --cached`. Empty diff → say so and exit.

2. **Compute changed-line set.** Run `git diff --cached --unified=0` (or with base ref), parse hunk headers to build `(file, line)` pairs added or modified. This is the review scope. Findings outside scope are dropped.

3. **Launch both agents in parallel:**
   - Agent `reviewer` — diff only as context. No whole-file reads.
   - Agent `security` — same diff, same constraint.

4. **Collect findings.** Parse JSON lines from both agents into one list.

5. **Write full log (pre-filter).** Write every parsed finding to `.pakka/reviews/<short-sha-or-timestamp>.jsonl`. Create dir if needed.

6. **Filter by confidence.** Drop findings where `confidence < 80` (or `pakka.review.confidenceThreshold`). Drop findings missing `line` field.

7. **Filter by scope.** Drop findings whose `(file, line)` is not in the changed-line set.

8. **Group and print.** Sort by file + line. Print:
   ```
   [severity] file:line — rationale (confidence%)
     fix: proposed fix
   ```

9. **Verdict.**
   - Any `severity=error` → `VERDICT: FAIL — N error(s) above threshold`. Exit 2.
   - Otherwise → `VERDICT: PASS`. Write timestamp to `.pakka/reviews/last-pass-ts`.

---

### Mode: receive

Handle incoming code review feedback with technical rigor.

1. Read all feedback before responding to any of it
2. For each item: assess technically — is the finding correct?
3. If correct: acknowledge and fix. No performative agreement — just fix it.
4. If incorrect or contradicts an established decision: push back with evidence. Cite the relevant code, test, or `memory/DECISIONS.md` entry.
5. If ambiguous: ask one clarifying question. Do not implement until clear.

Do not agree with feedback that is wrong just to be agreeable. Technical accuracy over social smoothness.

---

### Mode: finish

Structured branch landing. Four options:

1. **Merge locally** — merge into main/master now. Requires clean diff + passing tests.
2. **Push + PR** — push branch, open pull request. Use `gh pr create`.
3. **Keep branch** — not ready to land. Document why in a comment.
4. **Discard** — abandon work on this branch. Confirm with user before any destructive action.

Present all four options. User picks. Do not default.

Before any merge or push: run verification gate (same as verify→review mode). Never land broken code.

---

### Handle `--install-hook`

Run `${CLAUDE_PLUGIN_ROOT}/bin/run install-git-hook`. Print result. Stop.

---

## Red Flags

- Skipping verification before review → wrong. Always verify first.
- Claiming "done" or "passing" without running the tests → wrong. Exit code is the evidence.
- Reviewing an empty diff → warn and exit, don't fabricate findings.
- Showing findings below confidence threshold → never.
- Findings on lines the diff did not touch → never. Scope filter is mandatory.
- Agreeing with incoming feedback that is technically wrong → wrong. Push back with evidence.
- Auto-merging or auto-pushing without user's explicit choice → wrong. Always present the four options.
