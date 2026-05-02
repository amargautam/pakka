# ADR Format

ADRs live in `docs/adr/`. Sequential numbering: `0001-slug.md`, `0002-slug.md`.

Create `docs/adr/` lazily — only when the first ADR is needed.

## Template

```md
# {Short title}

{1–3 sentences: context, decision, reason.}
```

An ADR can be a single paragraph. Record *that* a decision was made and *why* — not a filled-out form.

## Optional sections

Include only when they add genuine value. Most ADRs won't need them.

- **Status** (`proposed | accepted | deprecated | superseded by ADR-NNNN`)
- **Considered Options** — when rejected alternatives matter to a future reader
- **Consequences** — when non-obvious downstream effects need recording

## Qualify before writing

All three must be true:

1. **Hard to reverse** — cost of changing your mind later is meaningful
2. **Surprising without context** — future reader would wonder "why did they do it this way?"
3. **Real trade-off** — genuine alternatives existed, one was chosen for specific reasons

Easy to reverse → just reverse it later. Not surprising → nobody wonders why. No alternatives → nothing to record.

Scan `docs/adr/` for the highest number and increment by one.
