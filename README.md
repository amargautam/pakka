# Pakka

Claude Code harness: fewer tokens, fewer bugs, audit-ready.

## Install

```
/plugin marketplace add amargautam/pakka-marketplace
/plugin install pakka@pakka-marketplace
```

Dev install (local path):

```
/plugin install ./path/to/pakka
```

## Claims

v0 will publish three numbers on this README, each reproducible via `make bench`:

1. Tokens per merged PR vs raw Claude Code.
2. Bug-class catch rate on 10 seeded-bug PRs (target: pakka ≥ 8/10 vs raw Claude Code ≤ 3/10).
3. Self-verification: `make bench` reproduces both end-to-end.

No claim without a benchmark. No benchmark without a commit hash.

## Status

v0 in progress. See [`DESIGN.md`](./DESIGN.md) for scope, file specs, and build order.

## Development

Pakka is built using Pakka. See `DESIGN.md` §10 (Build order) and [`CLAUDE.md`](./CLAUDE.md).

## License

Apache-2.0. See [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE).
