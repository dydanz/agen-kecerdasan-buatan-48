---
name: note-capture
description: >
  Capture and store facts, decisions, and context into the knowledge brain.
  Use when the operator explicitly asks to remember, note, or save something.
triggers:
  - remember
  - note that
  - store this
  - save this
  - don't forget
  - keep in mind
  - add to brain
  - make a note
---

## Note Capture Process

When the operator asks you to remember something:

1. **Identify the entity type** — classify as one of:
   - `person` — a team member, contact, or individual
   - `project` — an active or past project with status and tech stack
   - `decision` — an architectural, product, or business decision with rationale
   - `product` — a product you build or operate
   - `policy` — a standing rule or constraint

2. **Extract a structured entity:**
   - `title`: one sentence, factual, specific
   - `body`: 2–3 sentences with full context and rationale — enough that the fact is useful without the surrounding conversation
   - `tags`: 1–3 relevant domain tags (e.g., `["infra", "github.com/dydanz/akb48"]`)
   - `scope`: `org` (default for all operator notes)

3. **Call `gbrain_put`** with the structured entity JSON.

4. **Confirm** with a single concise response: "Stored. [one-line summary of what was saved]."

**Example:**
Operator: "Remember that we chose PostgreSQL + pgvector over Pinecone because we own the data."
→ Entity: `type=decision`, `title="Chose PostgreSQL + pgvector over Pinecone"`, `body="Selected for data ownership and operational simplicity. GBrain handles all indexing. Hybrid search: vector + keyword + graph."`, `tags=["infra", "database"]`
→ Response: "Stored. Decision: PostgreSQL + pgvector for brain storage (data ownership, GBrain integration)."
