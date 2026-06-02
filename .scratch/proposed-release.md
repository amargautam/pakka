<!-- destination: /Users/amar/Projects/pakka.dev/.claude/commands/release.md -->
<!-- prepared by docs/v090-drift-fixes (Workstream C). Replace the live file with this content. -->

# Release checklist — pakka vX.Y.Z

Argument: version tag (e.g. `v0.1.2`). Required.

Run every step in order. Do not skip any. Report each step's result before proceeding.

---

## 0. Docs drift audit (before anything else)

Read and cross-check:

1. `pakka/README.md` — all claims, counts, and command/agent names. Run against actual files:
   ```bash
   ls /Users/amar/Projects/pakka.dev/pakka/commands/ | wc -l   # README "N commands" claim
   ls /Users/amar/Projects/pakka.dev/pakka/agents/   | wc -l   # README agent list
   ```
   Flag: wrong count, renamed command still listed, capability claimed that isn't shipped.

2. `pakka-website/src/pages/` — same claims audit against README. Flag any numbers or status that diverged.

3. `memory/DECISIONS.md` — scan for "deferred to v0.X.Y" entries that are now past due.

Fix any drift **before** tagging. If fixes are non-trivial (code changes), stop and report to Larry.
Only proceed past this step when docs match reality.

### 0.1. Doc-sync audit (runnable)

Run this block. Investigate every line of output before proceeding. `<version>` = release tag being cut (e.g. `v0.9.0`).

```bash
VERSION=<version>                # set this, e.g. v0.9.0
ROOT=/Users/amar/Projects/pakka.dev

# (a) Version strings in monorepo root CLAUDE.md that don't match the release tag.
#     Historical mentions in the "Build order reference" section are expected;
#     flag only references that claim a `-dev` branch other than the in-flight one.
echo "--- CLAUDE.md root version refs ---"
grep -nE 'v0\.[0-9]+\.[0-9]+(-dev)?|v[0-9]+\.[0-9]+\.[0-9]+(-dev)?' "$ROOT/CLAUDE.md" \
  | grep -v "$VERSION" || echo "(no other version refs found)"

# (b) Command/agent counts on disk vs README claims.
CMD_COUNT=$(ls "$ROOT/pakka/commands/" | wc -l | tr -d ' ')
AGT_COUNT=$(ls "$ROOT/pakka/agents/"   | wc -l | tr -d ' ')
echo "--- counts ---"
echo "commands on disk: $CMD_COUNT"
echo "agents on disk:   $AGT_COUNT"
echo "--- README claims ---"
grep -nE '[0-9]+ commands?|[0-9]+ agents?|[0-9]+ skills?' "$ROOT/pakka/README.md" \
  || echo "(no numbered command/agent/skill claims)"
```

Drift on any line above → fix before tagging. README claim must match counts on disk. Active branch references in CLAUDE.md root must match current dev branch (which is `main` post-v0.3.0; `-dev` branches are no longer used).

---

## 1. Confirm code is committed and clean

```bash
cd /Users/amar/Projects/pakka.dev/pakka
git status        # must be clean
git log --oneline -3
```

If dirty: stop and ask Larry what to commit first.

---

## 1.2. Rebuild all platform binaries

```bash
cd /Users/amar/Projects/pakka.dev/pakka
make cross
```

Verify exit 0. Then confirm binaries updated:
```bash
ls -la bin/
```

Commit the rebuilt binaries:
```bash
git add bin/
git commit -m "chore: rebuild binaries for <version>

Co-Authored-By: pakka <279024857+pakka-bot@users.noreply.github.com>"
```

**Never skip this step.** Binaries in `bin/` are committed to the repo and served directly from the plugin cache. If not rebuilt before tagging, shipped binaries won't include the latest code changes. (Burned v0.5.0 — color changes in `internal/statusline/statusline.go` were missing from the released binary.)

---

## 1.5. Regenerate RECEIPTS.md

**1. Regenerate RECEIPTS.md:**
```bash
cd /Users/amar/Projects/pakka.dev/pakka
make self-report
```
`make self-report` uses `--repo-root=..` (monorepo root) — required to match the correct transcript dir.
Do NOT run the binary directly without `--repo-root=..`; it will silently use the wrong scope and produce a ~7× undercount.
Read the new `RECEIPTS.md` and extract the "Total estimated savings" figure (e.g. `~$43.47`).

**2. Update website savings figures (conditional):**

If `pakka-website/` displays RECEIPTS-derived savings figures (session count, bytes saved, total $, avg/session), update them. As of v0.9.0, no such files exist — `compress.astro` carries only per-1M-token calibration figures (`~$9.90/MTok` etc.), not session totals. Check before assuming this step is required:

```bash
grep -rnE '\$[0-9]+\.[0-9]+|[0-9]+ session|[0-9]{4,} bytes' \
  /Users/amar/Projects/pakka.dev/pakka-website/src/
```

If the grep returns RECEIPTS-derived figures, list the actual files + line numbers here and update them before the next step. If empty, skip to step 3.

**3. Commit RECEIPTS.md to pakka repo:**
```bash
cd /Users/amar/Projects/pakka.dev/pakka
git add RECEIPTS.md
git commit -m "chore: regenerate RECEIPTS.md for <version>

Co-Authored-By: pakka <279024857+pakka-bot@users.noreply.github.com>"
```

**4. Commit website update (only if step 2 found figures to update):**
```bash
cd /Users/amar/Projects/pakka.dev/pakka-website
git add <files-from-step-2>
git commit -m "docs(site): update savings figures for <version>

Co-Authored-By: pakka <279024857+pakka-bot@users.noreply.github.com>"
git push origin main
```

---

## 2. Bump version in plugin.json

In `pakka/.claude-plugin/plugin.json`, update the `"version"` field to `<version>` (strip the `v` prefix).

```bash
cd /Users/amar/Projects/pakka.dev/pakka
git add .claude-plugin/plugin.json
git commit -m "chore: bump plugin.json version to <version>

Co-Authored-By: pakka <279024857+pakka-bot@users.noreply.github.com>"
```

**Never skip this step.** Claude Code uses `plugin.json` version as the plugin cache dir name. Without a version bump, `/plugin install` reuses the stale cache dir and updates never apply. `/plugin marketplace update` must be run before `/plugin install` to pull the latest catalog ref — without it, install resolves to the old cached version regardless.

---

## 3. Tag and push pakka repo

```bash
git tag <version>
git push origin main
git push origin <version>
```

---

## 4. Update CHANGELOG.md

In `pakka/CHANGELOG.md`, prepend a new `## [<version>] — YYYY-MM-DD` section with:
- `### Fixed` / `### Added` / `### Changed` entries summarizing the diff since previous tag
- One bullet per meaningful change. No filler.

Then commit:

```bash
git add CHANGELOG.md
git commit -m "docs: CHANGELOG for <version>

Co-Authored-By: pakka <279024857+pakka-bot@users.noreply.github.com>"
git push origin main
```

---

## 5. Update marketplace

In `pakka-marketplace/.claude-plugin/marketplace.json`, update:
- `ref` → `<version>`
- `metadata.version` → `<version>` (strip the `v` prefix if field is semver-only)

```bash
cd /Users/amar/Projects/pakka.dev/pakka-marketplace
git add .claude-plugin/marketplace.json
git commit -m "chore: bump pakka ref to <version>

Co-Authored-By: pakka <279024857+pakka-bot@users.noreply.github.com>"
git push origin main
```

---

## 6. Create GitHub releases (both repos)

```bash
# pakka plugin repo
gh release create <version> \
  --repo amargautam/pakka \
  --title "pakka <version>" \
  --notes "<one-paragraph plain-text summary of what changed>" \
  --latest

# pakka-marketplace catalog repo
gh release create <version> \
  --repo amargautam/pakka-marketplace \
  --title "pakka-marketplace <version>" \
  --notes "Bumps pakka ref to <version>." \
  --latest
```

Copy both release URLs from output and report them.

---

## 7. Update memory/LOG.md

Append a new entry at the top of `memory/LOG.md`:

```
## <YYYY-MM-DD> · <version>

**Shipped:** <brief summary of what changed>
**Next:** <next planned work>
```

---

## 8. Final verification

```bash
gh release list --repo amargautam/pakka | head -5
gh release list --repo amargautam/pakka-marketplace | head -5
```

Report: tag pushed, changelog updated, marketplace bumped, release live, log updated.

---

## Red flags

- `plugin.json` version not bumped — cache key unchanged, existing users never get update (burned v0.1.0–v0.1.3)
- Tag already exists on remote — stop, ask Larry before overwriting
- Marketplace `ref` not updated — users will install wrong version
- GitHub release missing on `amargautam/pakka` — tag alone is not a release; users can't see it in GitHub UI
- GitHub release missing on `amargautam/pakka-marketplace` — both repos need releases; marketplace page shows v0.1.1 if skipped
- CHANGELOG missing the new version — skip only if version is a hotfix with no user-visible change
- `memory/LOG.md` not updated — next session's hydration is degraded
- `RECEIPTS.md` not regenerated — savings figure goes stale with each release
- Website savings figure not updated (when such files exist) — users see wrong number
