# AKB48 Architecture Decision Records

Architecture decisions that affect the structure, runtime behaviour, or component boundaries of AKB48. Each ADR captures the context, decision, alternatives, and trade-offs at the time the decision was made.

**Template:** `0000-template.md`

---

## Index

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| — | *(none yet)* | — | — |

---

## When to write an ADR

Add `adr-required` label to any PR that:
- Changes a component interface or session contract
- Introduces or removes a dependency
- Changes the adapter pattern or runtime wiring
- Affects how sessions, tools, or identity files are loaded
- Establishes a convention that future tickets must follow

The ADR bot will open a follow-up issue if the ADR is not linked within 48h of the PR merging.

## Naming

`NNNN-short-kebab-title.md` — sequential, starting from `0001`.
