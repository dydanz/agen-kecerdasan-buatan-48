# Workflow: Idea to Production

The end-to-end journey from a raw idea to a feature live in production.

## Overview

```
Idea → Research → PRD → TRD → Implementation → Testing → Deploy → Monitor
  │        │        │      │         │              │         │         │
  │   product-  product-  tech-   plan_feature   run_tests  gitops   observability
  │   research  manager   lead-                            /cicd      -infra
  │                       architect                                               │
  └────────────────────────────────────────────────────────────────────────────────
         ⛔ No code written until TRD is Approved
```

## Phase 1: Discovery (Product)

**Goal:** Validate the problem is real and worth solving.

**Steps:**
1. Document the raw idea or user problem
2. Invoke `/generate-prd` to begin the research and requirements process
3. `product-research` agent: conduct user and market research
4. Align on problem statement (no solution yet)
5. Define success metrics with baselines

**Exit criteria:**
- [ ] Problem statement confirmed by stakeholders
- [ ] Target user segment defined with evidence
- [ ] Success metrics have baselines
- [ ] Decision: build vs buy vs defer

---

## Phase 2: Requirements (PRD)

**Goal:** Define exactly what to build, with clear acceptance criteria.

**Steps:**
1. `product-manager` agent: write PRD using research findings
2. Review all user stories and acceptance criteria
3. Resolve all open questions (no PRD with unknowns)
4. Get stakeholder sign-off on scope (P0 vs P1 vs P2)

**Artifacts:**
- `.claude/thoughts/prd/YYYY-MM-DD-[feature].md`

**Exit criteria:**
- [ ] PRD status: Approved
- [ ] All P0 requirements defined with acceptance criteria
- [ ] Out-of-scope section explicit
- [ ] Dependencies identified with owners and ETAs

---

## Phase 3: TRD — Technical Requirements Document

**Goal:** Produce a single approved TRD before any code is written. The TRD is the authoritative contract between product, engineering, and ops.

**Steps:**
1. Invoke `/design-architecture` with the approved PRD
2. `tech-lead-architect` agent: author TRD using `templates/trd-template.md`
3. Fill every section — leave no section blank; mark N/A only if explicitly not applicable
4. Review API contract with frontend and backend leads
5. Review data model with DBA or backend lead
6. Confirm all NFRs (performance, reliability, security) have measurable targets
7. Resolve all open questions — no TRD may be approved with open questions remaining
8. Get tech lead sign-off → set TRD status to **Approved**

**Artifacts:**
- `.claude/thoughts/trd/YYYY-MM-DD-[feature]-trd.md` (from `templates/trd-template.md`)
- ADRs for key technical decisions (use `templates/architecture-decision-record.md`)

**Exit criteria — hard gate, no implementation starts until all boxes are checked:**
- [ ] TRD status: **Approved**
- [ ] All 11 sections completed (no blanks except explicit N/A)
- [ ] API contract reviewed by both frontend and backend leads
- [ ] Data model and migration strategy defined (or N/A with reason)
- [ ] All NFR targets are measurable (not "fast" — use numbers)
- [ ] Technical risks documented with mitigations
- [ ] Open Questions section is empty (all resolved)
- [ ] Success metrics defined with baselines and targets

> **Rule:** If implementation uncovers a gap in the TRD, stop and update the TRD first. Get re-approval before continuing.

---

## Phase 4: Implementation

**Goal:** Build the feature according to the TDD.

**Steps:**
1. Invoke `/plan-feature` with the approved TRD
2. Create implementation tasks from the plan
3. Backend: implement following clean architecture
   - Domain layer first (no external deps)
   - Infrastructure adapters second
   - HTTP handlers last
4. Frontend: implement following feature-based structure
   - API hooks first
   - State management second
   - UI components last
5. Write tests alongside code (TDD preferred)

**Key agents:**
- `go-architect` — structural decisions
- `concurrency` — goroutine patterns
- `fault-tolerance` — retry/circuit breaker
- `observability` — logging/metrics/tracing
- `frontend-architect` — component structure
- `state-management` — data flow

**Exit criteria:**
- [ ] All P0 features implemented
- [ ] Unit tests written and passing
- [ ] Integration tests written and passing
- [ ] Code reviewed (`/review-code`)
- [ ] golangci-lint passes

---

## Phase 5: Testing

**Goal:** Verify the implementation matches the PRD acceptance criteria.

**Steps:**
1. Invoke `/run-tests` — all test layers
2. Create test plan using `templates/test-plan-template.md`
3. Run E2E tests against staging environment
4. Performance test if SLO is involved
5. Manual QA against acceptance criteria in PRD
6. Fix any failures before proceeding

**Exit criteria:**
- [ ] All automated tests pass in CI
- [ ] Coverage meets targets (70% overall, 85% domain)
- [ ] All P0 acceptance criteria verified manually in staging
- [ ] Performance SLOs met in staging load test

---

## Phase 6: Deployment

**Goal:** Ship to production safely.

**Steps:**
1. Merge to main → CI runs → builds and pushes image
2. GitOps automation updates dev environment
3. Verify dev deployment is healthy
4. Create PR to staging GitOps overlay
5. Verify staging deployment is healthy
6. Create PR to prod GitOps overlay (requires approval)
7. Deploy to production during deployment window
8. Monitor for 30 minutes post-deployment

**Exit criteria:**
- [ ] Dev deployment healthy for 24 hours
- [ ] Staging deployment healthy, E2E tests pass
- [ ] Production deployment healthy for 30 minutes
- [ ] No spike in error rate or latency
- [ ] Rollback plan confirmed (revert GitOps commit)

---

## Phase 7: Monitor & Learn

**Goal:** Confirm the feature achieves its success metrics.

**Steps:**
1. Monitor error rates and latency for 48 hours
2. Track leading indicators (adoption, activation) at Day 7
3. Track lagging indicators (retention, NPS) at Day 30
4. Document learnings in `.claude/learnings.md`

**Exit criteria:**
- [ ] Primary metric on track to hit target
- [ ] No regression in counter-metrics
- [ ] Learnings documented for future features
