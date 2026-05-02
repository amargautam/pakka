# Contributing

Pakka is built by AI agents using pakka itself. Every merged change has passed the review gate.

## How to contribute

1. **Open an issue first.** Describe what you want to build or fix. Label it `bug` or `enhancement`.
2. **Wait for triage.** A maintainer (or the `/pakka:triage` skill) will classify it and write an agent brief if it's `ready-for-agent`.
3. **Fork and implement.** Follow the agent brief. All code goes through `/pakka:review` before merge.
4. **One vertical slice per PR.** Each PR should deliver a narrow but complete path through every layer — independently reviewable and mergeable.

## Standards

- Skills require a `Red Flags` section. Run `/pakka:eval` before submitting.
- No TypeScript-specific or language-specific code in general skills — keep them generalizable.
- Commit messages: `type(scope): description`. Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`.
- Every commit pakka reviews gets a `Reviewed-by-pakka` trailer. Don't remove it.

## Local setup

```bash
git clone https://github.com/amargautam/pakka
cd pakka
make build        # builds bin/pakka-core
make test         # runs all tests
```

No external dependencies beyond Go stdlib. See `go.mod` for the Go version.
