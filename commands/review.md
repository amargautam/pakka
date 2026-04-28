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

1. **Get the diff.** If `--base=<ref>` is provided, use `git diff <ref>...HEAD`. Otherwise, use `git diff --cached`. If the diff is empty, say so and exit.

2. **Launch both agents in parallel** using the Agent tool:
   - Agent `reviewer` — pass the diff as context.
   - Agent `security` — pass the same diff.

3. **Collect findings.** Each agent returns JSON lines. Parse all lines from both agents into a single list.

4. **Filter by confidence.** Drop any finding where `confidence < 80` (or the value of `pakka.review.confidenceThreshold` from settings). Drop any finding missing a `line` field.

5. **Group by file.** Sort findings by file path, then by line number.

6. **Print verdicts.** For each finding above threshold, print one line:
   ```
   [severity] file:line — rationale (confidence%)
     fix: proposed fix
   ```

7. **Write full log.** Write all findings (pre-filter) to `.pakka/reviews/<short-sha-or-timestamp>.jsonl`. Create the directory if needed.

8. **Determine pass/fail.**
   - If any finding with `severity=error` passes the confidence threshold → **FAIL**. Print `VERDICT: FAIL — N error(s) above threshold`. Exit with code 2.
   - Otherwise → **PASS**. Print `VERDICT: PASS`. Write the current Unix timestamp to `.pakka/reviews/last-pass-ts`.

### Red Flags

- Running review on an empty diff → warn and exit, don't fabricate findings.
- Reporting agent parse errors as review failures → log the error but don't block the commit.
- Showing findings below the confidence threshold → never. Filter first, display second.
- Lowering the threshold to catch more issues without recalibrating → unsafe. Threshold exists for a reason.
