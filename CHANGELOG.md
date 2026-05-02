# Changelog

All notable changes to pakka. Format follows [Keep a Changelog](https://keepachangelog.com).

## [v0.2.0] — 2026-05-02

### Added

- `/pakka:plan` — design hub: routes spec / challenge / probe / slice from context, writes to `docs/specs/`, never auto-chains to build
- `/pakka:build` — implementation hub: routes tdd / debug / map / audit from context, spec approval gate required, verification gate (exit codes) before done
- `/pakka:setup` — one-time init + guard hook; no arg → init, `guard` → guard hook
- Hook pre-handling: `/pakka:compress <level>` and `/pakka:help` handled by UserPromptSubmit hook — ~70% latency reduction, no LLM round-trip for config writes
- Ambient disciplines injected at session start: verification (exit code required before any "done" claim) + skill-check (route to plan/build/review when signal detected)
- Semantic compression auto-enable by level: `ultra` = on by default (user can opt out), `super-ultra` = enforced
- Mid-session level switch: full filtered ruleset emitted in `additionalContext` — takes effect immediately without session restart

### Changed

- Default compression level: `super-ultra` (was `ultra`)
- Command count: 14 → 7 — old commands (spec, challenge, probe, slice, tdd, debug, map, init, guard) redirect to new hubs via alias
- `main.go` 1625 → 67 lines: extracted into 16 `*_cmd.go` files + `helpers.go` + `command.go` interface
- `hookevent.go`: `Parse()` removed — callers use `parseStrict` / `parseLenient` in helpers.go

### Fixed

- `resolveOutputLevel` fallback: `'ultra'` → `'super-ultra'` to match new default

## [v0.1.4] — 2026-05-02

### Fixed

- Added `"version"` field to `plugin.json` — without it, all versions (v0.1.0–v0.1.3) resolved to the same plugin cache directory and updates never applied for existing users
- `/pakka:compress <level>` fix now actually active — `commands/compress.md` was correctly patched in v0.1.3 but never loaded due to cache invalidation bug above

### Upgrade

Existing users must reinstall manually to pick up this fix:
```
/plugin install pakka@pakka-marketplace
/reload-plugins
```

## [v0.1.3] — 2026-05-02

### Fixed

- `/pakka:compress <level>` fix applied to correct file (`commands/compress.md`) — v0.1.2 patched `skills/pakka-compress/SKILL.md` but Claude Code loads `commands/compress.md` for command invocations

## [v0.1.2] — 2026-05-02

### Fixed

- `/pakka:compress <level>` now writes to `~/.config/pakka/config.json` (`defaultLevel`) and `~/.claude/.pakka-level` flag file — persists across plugin reinstalls and takes effect immediately in current session
- Skip `--orchestrator-run` binary invocation when `semantic: false` — eliminates latency on every level switch

## [v0.1.1] — 2026-05-02

### Fixed

- Infinite loop in 11 commands caused by `allowed-tools: Skill` delegation — commands now read SKILL.md directly
- `compress` command: validate level arg before Bash invocation, remove shell injection vector, safe restore (no auto-delete of backups)
- Restore operation now requires explicit user confirmation before overwriting files

### Changed

- Renamed `/pakka:review-architecture` → `/pakka:audit-code-arch`
- `reviewer` and `security` agents upgraded to `opus`
- `statusline` decoupled from `orchestrator` — stale count passed by caller (main.go)

## [v0.1.0] — 2026-05-02

### Added

**10 engineering skills** — auto-invoked by trigger phrase, callable as `/pakka:<skill>`:

| Skill | Invokes when you say |
|---|---|
| `/pakka:spec` | "build X", "implement X", "add feature" |
| `/pakka:debug` | "debug", "fix this bug", "broken", "failing" |
| `/pakka:tdd` | "write tests", "TDD", "test first" |
| `/pakka:audit-code-arch` | "architecture", "coupling", "hard to test" |
| `/pakka:challenge` | "challenge this", "stress test my plan" |
| `/pakka:probe` | "probe me", "question my design" |
| `/pakka:map` | "how does X work", "explain this module" |
| `/pakka:triage` | "triage", "look at issue #N" |
| `/pakka:slice` | "break into tickets", "create issues" |
| `/pakka:guard` | "protect git", "block force push" |

**Skill auto-invocation** — `rules/skill-invoke.md` injected at session start. Claude invokes the right skill automatically without a slash command.

**4-vector output compression** — JS hooks inject per-level ruleset at session start and reinforce every turn. Levels: `lite`, `strict`, `ultra` (default), `super-ultra`. Switch with `/pakka:compress <level>`.

**`claude -p` subprocess as primary semantic-rewrite engine.** Zero-config for Claude Code users — pakka reuses existing `claude` auth on `PATH`.

### Changed

- **Status line:** `pakka [ultra]` — active compression level always visible.
- **Default output compression level: `ultra`** — pakka's brand thesis is fewer tokens.

### Fixed

- Stack-config command exec: metacharacters rejected; no `sh -c` path.
- Semantic-compression sandbox: `claude` subprocess runs with `--permission-mode default`.
- Audit hash full-width: `InputHash` now full SHA-256.
- Path traversal guard: regex catches 2+ hop `../`.
- Commit-gate trailer dedupe: repeated invocations no longer stack duplicate trailers.
