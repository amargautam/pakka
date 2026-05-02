---
name: pakka-skill
description: Add a reusable discipline to the pakka library. A well-written skill compresses a whole engineering practice into a single invocation — eliminating the need to re-explain it every session. Use when encoding a repeatable workflow, process, or technique into a skill file.
allowed-tools: Read, Write, Edit, Glob, Grep
argument-hint: "[skill name and purpose]"
user-invocable: true
---

## Thesis

A skill is reusable context. A well-written skill eliminates the need to re-explain a discipline every session — compresses the instruction once, amortizes it across every future invocation.

## Process

### 1. Requirements

Ask:
- What task or domain does this skill cover?
- What specific phrases or contexts should trigger it?
- Does it need supporting reference files or utility scripts?
- Any reference material to bundle?

### 2. Draft

Create:
- `skill.md` — main instructions with frontmatter and Red Flags section
- Supporting reference files if content would exceed 100 lines
- Utility scripts if deterministic operations are needed

### 3. Review

Present the draft. Ask:
- Does this cover the use cases?
- Anything missing or unclear?

## File structure

```
skills/pakka-{name}/
├── skill.md          ← required
├── REFERENCE.md      ← if skill.md would exceed 100 lines
└── scripts/          ← if deterministic shell operations are needed
    └── helper.sh
```

## skill.md frontmatter

```yaml
---
name: pakka-{name}
description: {What it does. Use when [specific triggers].}
allowed-tools: {comma-separated tool list}
argument-hint: "[args]"
user-invocable: true|false
---
```

## Description rules

The description is what the runtime reads to decide whether to load this skill. Make it precise:
- First sentence: what capability it provides
- Second sentence: `Use when [exact phrases, keywords, or contexts]`
- Max 1024 characters

Good: `"Debug via deterministic feedback loop. Use when user says 'debug', 'fix this bug', or describes broken behavior."`
Bad: `"Helps with debugging."`

## Body rules

- **Red Flags section is mandatory.** No exceptions — pakka eval rejects skills without it.
- Lead with thesis: how does this skill reduce context waste or prevent bugs?
- Language-agnostic unless skill is explicitly stack-specific.
- Terse: tables, arrows, checklists over prose paragraphs.
- Split into reference files when skill.md would exceed 100 lines.

## Red Flags format

Each entry: `[thing that would go wrong] → [why it's wrong / correct behavior]`

## After writing

Run `/pakka:eval` on new skill before committing. Layer 1 checks frontmatter, banned words, Red Flags presence, and line length. Fix any failures before skill enters the library.

## Red Flags

- Missing Red Flags section → eval Layer 1 will reject. Mandatory.
- Description without "Use when..." triggers → runtime can't decide when to load this skill.
- skill.md over 100 lines without splitting → split into reference files.
- Language-specific tooling when skill could be general → generalize.
- Banned words in body (`guarantee`, `revolutionary`, `seamless`, `delightful`) → eval Layer 1 will reject.
- Time-sensitive content (specific versions, current dates) → goes stale. Avoid.
