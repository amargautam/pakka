# Upgrade visibility — statusline shows newer cached plugin version
Date: 2026-07-24
Status: draft

## Problem
Plugin upgrades are silent: users run old cached versions while newer ones sit installed in the local plugin cache (observed live: session enforcing with 0.12.0 while 0.15.1 is cached beside it — five releases of fixes unused). No dial-home allowed, so pakka cannot check remotely; but the local cache directory already knows a newer version exists and nothing surfaces it.

## User stories
- As a pakka user, I want the status line to show when a newer installed version exists so that I reload/reinstall instead of silently running stale enforcement.

## Module decisions
- Signal source: sibling version directories of the running plugin root (CLAUDE_PLUGIN_ROOT parent) — pure readdir, ZERO network. Nothing else.
- Compare semver numerically; prerelease/malformed dir names ignored.
- Render: compact `↑<version>` segment appended to existing status line; absent when current or when signal unavailable (no plugin root, no cache dir).
- Runs in the statusline hot path → must stay in pakka-hot without new deps; readdir result cached by parent-dir mtime (existing cache pattern).
- Kill-switch parity via existing PAKKA_DISABLED (statusline already covered).

## Acceptance criteria
1. Running version 0.12.0 with sibling dirs {0.11.0, 0.12.1, 0.15.1} → segment `↑0.15.1`. Running 0.15.1 with no higher sibling → no segment.
2. Comparison numeric per component: 0.9.0 < 0.15.1; dirs not matching semver are ignored without error.
3. No plugin root resolvable (env absent) or cache dir unreadable → no segment, no error, exit unchanged.
4. Zero new imports in pakka-hot forbidden-deps test (still no sqlite/net-http/semantic/orchestrator/recall).
5. Parent-dir mtime unchanged across N renders → one readdir (probe counter); mtime bump → re-scan.
6. Status line keeps tokens AND percent display unchanged; new segment purely additive.
7. go test ./... exit 0; make test-js exit 0.

## Out of scope
- Any network version check.
- Auto-upgrade or prompting beyond the passive segment.
- Marketplace catalog parsing.

## Open questions
