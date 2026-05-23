---
name: High risk (TRD required)
about: Breaking change, data migration, architecture change, source-of-truth change
labels: 'class:high,adr-required'
---

## Problem and context
[2–4 sentences. Include impact and scope.]

## Proposed change
[Detailed description of what will change]

## Alternatives considered
- [Option A] — [why rejected]

## Invariants
- [explicit invariant that must hold]
- PRD / source of truth: [reference to `akb48-prd/` document or named subsystem]

## Acceptance criteria
- [ ] [specific, measurable]
- [ ] Full test suite passes (`go test ./...`)
- [ ] Manual smoke test on local binary

## Test plan
- New unit tests: [what they cover]
- Manual test steps: [list]

## Rollback strategy
- How: [concrete steps, not "we'll revert"]
- Estimated rollback time: [estimate]

## Risk class justification
[Why High and not Medium]

## Linked ADRs
[ADR-NNN: title, or "None — will draft post-merge"]

## Change class
high

---
<!-- TRD must be complete before work begins. Self-approve with /approve-spec -->
<!-- adr-required label: draft ADR within 48h of merge (bot enforces) -->
<!-- 2 manual review passes recommended even for solo work -->
