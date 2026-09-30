# Domain Docs

This repository uses a single-context domain-doc layout.

## Before exploring, read these

- `CONTEXT.md` at the repository root
- Relevant ADRs under `docs/adr/`

If these files do not exist, proceed silently. Domain-modeling skills create them lazily when terminology or architectural decisions are resolved.

## File structure

```
/
├── CONTEXT.md
├── docs/adr/
└── src/
```

## Use the glossary's vocabulary

Use terms defined in `CONTEXT.md` when naming domain concepts. Avoid synonyms the glossary explicitly rejects.

## Flag ADR conflicts

Explicitly surface output that contradicts an existing ADR rather than silently overriding it.
