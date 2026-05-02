# Changelog

All notable changes to pakka. Format follows [Keep a Changelog](https://keepachangelog.com).

## [v0.1.0] — 2026-05-02

### Added

**10 engineering skills** — auto-invoked by trigger phrase, callable as `/pakka:<skill>`:

| Skill | Invokes when you say |
|---|---|
| `/pakka:spec` | "build X", "implement X", "add feature" |
| `/pakka:debug` | "debug", "fix this bug", "broken", "failing" |
| `/pakka:tdd` | "write tests", "TDD", "test first" |
| `/pakka:review-architecture` | "architecture", "coupling", "hard to test" |
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
