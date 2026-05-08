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

2. **Compute changed-line set.** Run `git diff --cached --unified=0` (or with base ref), parse hunk headers to build `(file, line)` pairs added or modified. This is the review scope. Findings outside scope are dropped (except `spec-divergence` — see step 7).

2a. **Discover spec.** If `--spec <file>` was passed, use it directly as SPEC_FILE. Otherwise:
   - If `--base=<ref>` provided: `CHANGED=$(git diff <ref>...HEAD --name-only | paste -sd, -)` else `CHANGED=$(git diff --cached --name-only | paste -sd, -)`
   - Run: `SPEC_FILE=$(${CLAUDE_PLUGIN_ROOT}/bin/run spec-find --branch "$(git branch --show-current)" --changed "$CHANGED")`
   - If `docs/specs/` does not exist → SPEC_FILE is empty, no advisory.
   - If `docs/specs/` exists and SPEC_FILE is empty → set ADVISORY=true.
   - If SPEC_FILE is non-empty → read file contents into SPEC_CONTENT.

2b. **Spec drift check.** If SPEC_FILE is non-empty:
   - Run: `MERGE_BASE=$(git merge-base origin/main HEAD 2>/dev/null || git merge-base main HEAD 2>/dev/null)`
   - If MERGE_BASE is non-empty: run `SPEC_DRIFT_LOG=$(git log "${MERGE_BASE}..HEAD" --oneline -- "${SPEC_FILE}" 2>/dev/null)`
   - If SPEC_DRIFT_LOG is non-empty:
     - `SPEC_DRIFT_COUNT=$(echo "$SPEC_DRIFT_LOG" | wc -l | tr -d ' ')`
     - `SPEC_DRIFT_DIFF=$(git diff "${MERGE_BASE}..HEAD" -- "${SPEC_FILE}" 2>/dev/null | head -100)`
     - Create a spec-drift finding (injected directly, not from an agent):
       ```json
       {"kind":"spec-drift","severity":"warning","file":"<SPEC_FILE>","line":1,"confidence":100,"rationale":"Spec modified in <SPEC_DRIFT_COUNT> commit(s) during build — review before approving","fix":"Review spec diff above before approving","diff":"<SPEC_DRIFT_DIFF truncated to 100 lines>"}
       ```
     - Add this finding to the findings list (before step 4 collect).
   - If MERGE_BASE is empty or SPEC_DRIFT_LOG is empty: skip silently.

3. **Launch all three agents in parallel.** Pass diff as context. If SPEC_CONTENT is set, append it as a `## Spec context` block in each agent's prompt.
   - Agent `reviewer` — no whole-file reads.
   - Agent `security` — no whole-file reads.
   - Agent `architect` — may Read files the diff touches for coupling context only.

4. **Collect findings.** Parse JSON lines from all three agents into one list.

5. **Write full log (pre-filter).** Write every parsed finding to `.pakka/reviews/<short-sha-or-timestamp>.jsonl`. Create dir if needed.

6. **Filter by confidence.** Drop findings where `confidence < 80` (or `pakka.review.confidenceThreshold`). Drop findings missing `line` field. Exception: `kind=spec-divergence` and `kind=spec-drift` findings are never dropped by confidence filter.

7. **Filter by scope.** Drop findings whose `(file, line)` is not in the changed-line set. Exception: `kind=spec-divergence` and `kind=spec-drift` findings are exempt from scope filtering.

8. **Group and print.** Sort by file + line. Print:
   ```
   [severity] file:line — rationale (confidence%)
     fix: proposed fix
   ```

   For `kind=spec-drift` findings: print the diff field as an indented block after the fix line.
   ```
   [warning] <spec-file>:1 — Spec modified in N commit(s) during build — review before approving (confidence: 100%)
     fix: Review spec diff above before approving
     diff:
     <SPEC_DRIFT_DIFF>
   ```

9. **Verdict.**
   - If ADVISORY=true → append: `note: no matching spec found in docs/specs/ — review ran without spec context. Run /pakka:plan to write one.`
   - Any `severity=error` → `VERDICT: FAIL — N error(s) above threshold`. Exit 2.
   - Otherwise → `VERDICT: PASS`. Write unix epoch timestamp to `.pakka/reviews/last-pass-ts` via `date +%s` (e.g. `echo $(date +%s) > .pakka/reviews/last-pass-ts`). Path is relative to the repo root being reviewed, not the session CWD.

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
