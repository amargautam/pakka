---
description: Run reviewer + security in parallel on staged diff, filter by confidence, print grouped verdicts.
allowed-tools: Agent, Bash, Read
argument-hint: "[--base=<ref>] [--install-hook]"
---

## Instructions

### Handle `--install-hook`

If the user's argument contains `--install-hook`, run:
```
${CLAUDE_PLUGIN_ROOT}/bin/run install-git-hook
```
Print the result and stop. Do not run a review.

Note: Optional. Installs a prepare-commit-msg git hook so human-authored commits (typed in a terminal) also get the trailer. Claude Code commits are auto-signed via the plugin; no hook install needed.

### Run review

1. **Get the diff.** If `--base=<ref>` is provided, use `git diff <ref>...HEAD`.
   Otherwise, use `git diff --cached`. If the diff is empty, say so and exit.

2. **Compute the changed-line set.** Get the same range with `--unified=0`
   (e.g. `git diff --cached --unified=0`) and parse hunk headers to build the
   set of `(file, line)` pairs that are added or modified in the post-image.
   This is the **review scope**. Any finding whose `(file, line)` is not in
   this set will be dropped before the verdict — pre-existing code is not in
   scope.

3. **Launch both agents in parallel** using the Agent tool:
   - Agent `reviewer` — pass **only the diff** as context. Do not attach whole-file contents. Do not invite the agent to `Read` unrelated files.
   - Agent `security` — pass the same diff under the same constraint.

4. **Collect findings.** Each agent returns JSON lines. Parse all lines from both agents into a single list.

5. **Write full log (pre-filter).** Write **every** parsed finding —
   pre-confidence-filter, pre-scope-filter — to
   `.pakka/reviews/<short-sha-or-timestamp>.jsonl`. Create the directory if
   needed. The audit trail keeps the unfiltered set for debugging false
   positives.

6. **Filter by confidence.** Drop any finding where `confidence < 80` (or the value of `pakka.review.confidenceThreshold` from settings). Drop any finding missing a `line` field.

7. **Filter by scope.** Drop any finding whose `(file, line)` is not in the
   changed-line set computed in step 2. Findings on context lines, deleted
   lines, or unstaged files must not survive this filter.

8. **Group by file.** Sort surviving findings by file path, then by line number.

9. **Print verdicts.** For each surviving finding, print one line:
   ```
   [severity] file:line — rationale (confidence%)
     fix: proposed fix
   ```

10. **Determine pass/fail.**
    - If any surviving finding has `severity=error` → **FAIL**. Print `VERDICT: FAIL — N error(s) above threshold`. Exit with code 2.
    - Otherwise → **PASS**. Print `VERDICT: PASS`. Write the current Unix timestamp to `.pakka/reviews/last-pass-ts`.

### Red Flags

- Running review on an empty diff → warn and exit, don't fabricate findings.
- Reporting agent parse errors as review failures → log the error but don't block the commit.
- Showing findings below the confidence threshold → never. Filter first, display second.
- Lowering the threshold to catch more issues without recalibrating → unsafe. Threshold exists for a reason.
- Reporting findings on lines the diff did not touch → never. Pre-existing
  code is out of scope. The scope filter (step 7) is mandatory; do not skip
  it even if the agents claim a finding is "important context."
- Letting agents read whole files for context → no. Diff-only input keeps findings anchored to the change. The line-set filter is the safety net regardless.
