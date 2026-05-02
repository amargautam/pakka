# CONTEXT.md Format

## Structure

```md
# {Context Name}

{One or two sentences: what this context is and why it exists.}

## Language

**Term**:
One sentence. What it IS, not what it does.
_Avoid_: synonym1, synonym2

**OtherTerm**:
One sentence definition.
_Avoid_: alternate-name

## Relationships

- A **Term** has exactly one **OtherTerm**
- An **OtherTerm** belongs to exactly one **ThirdTerm**

## Flagged ambiguities

- "account" used to mean both **Customer** and **User** — resolved: these are distinct concepts.
```

## Rules

- Opinionated: pick one name per concept, list others as "Avoid."
- Flag conflicts explicitly in "Flagged ambiguities" with a clear resolution.
- One sentence per definition. What it IS.
- Show relationships with cardinality where obvious.
- Domain-specific terms only. General programming concepts (retries, timeouts, error types) don't belong.
- Create the file lazily — only when the first term is resolved.
- For multi-context repos, create a `CONTEXT-MAP.md` at the repo root listing each context, where it lives, and how contexts relate.
