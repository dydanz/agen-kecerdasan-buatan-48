# PRD: [Feature Name]

**Status:** Draft | In Review | Approved | Shipped
**Owner:** [PM Name]
**Date:** YYYY-MM-DD
**Target Release:** [Version or Date]
**Last Updated:** YYYY-MM-DD

---

## Problem Statement

[2-3 sentences. What problem does this solve? For whom? What is the measurable impact if not solved?]

## Background & Context

[Research findings, user feedback, or business context that motivates this feature. Link to research doc if available.]

## Goals

### Primary Goal
[The ONE thing this feature must achieve — single, specific, measurable]

### Secondary Goals
- [Additional value delivered]
- [Constraint addressed]

### Non-Goals (Explicitly Out of Scope)
- [What this feature will NOT do — prevents scope creep]
- [Future work intentionally deferred]

---

## User Stories

### Story 1: [Core Happy Path]
**As a** [user type]
**I want to** [action / capability]
**So that** [benefit / measurable outcome]

**Acceptance Criteria:**
- [ ] Given [initial context], when [user action], then [system behavior / result]
- [ ] Given [initial context], when [user action], then [system behavior / result]
- [ ] Given [error condition], when [user action], then [user-visible error] and [system behavior]

**Out of Scope for this Story:**
- [Excluded behavior]

---

### Story 2: [Secondary Workflow]
**As a** [user type]
**I want to** [action]
**So that** [benefit]

**Acceptance Criteria:**
- [ ] Given ..., when ..., then ...

---

### Story 3: [Error / Edge Case]
**As a** [user type]
**I want to** [error handling / edge case]
**So that** [expected behavior in failure scenario]

**Acceptance Criteria:**
- [ ] Given ..., when ..., then ...

---

## Functional Requirements

### Must Have (P0) — Blocking launch
- **FR-001:** [Specific, testable behavior]
- **FR-002:** [Specific, testable behavior]
- **FR-003:** [Specific, testable behavior]

### Should Have (P1) — Important but not blocking
- **FR-010:** [Behavior]
- **FR-011:** [Behavior]

### Nice to Have (P2) — Defer if constrained
- **FR-020:** [Behavior — only if time permits]

---

## Non-Functional Requirements

| Category | Requirement | Measurement |
|----------|-------------|-------------|
| Performance | API response p99 < Xms | Prometheus histogram |
| Performance | Page TTI < Xs | Lighthouse / Core Web Vitals |
| Availability | X% uptime SLA | Uptime monitoring |
| Security | Auth required for all write operations | Security review |
| Accessibility | WCAG 2.1 AA compliance | Axe / manual audit |
| Data retention | [Data kept for X days/years] | Retention policy |

---

## Success Metrics

### Primary Metric
| Metric | Baseline | Target | Timeframe | Measurement |
|--------|----------|--------|-----------|-------------|
| [Core metric] | [Current value] | [Target value] | 30 days post-launch | [How measured] |

### Secondary Metrics
| Metric | Baseline | Target | Timeframe | Measurement |
|--------|----------|--------|-----------|-------------|
| [Metric 2] | | | | |
| [Metric 3] | | | | |

### Counter-Metrics (Watch for regressions)
| Metric | Current | Acceptable Range | Alert Threshold |
|--------|---------|------------------|----------------|
| Error rate | X% | < Y% | > Z% |
| Page load time | Xs | < Ys | > Zs |

---

## Dependencies

### Requires (must exist before launch)
- [ ] [Dependency 1] — Owner: [Team] — ETA: [Date]
- [ ] [Dependency 2] — Owner: [Team] — ETA: [Date]

### Blocks (cannot start until this ships)
- [Work item] — Team: [Team]

### External Dependencies
- [Third-party service, API, or contract]

---

## Open Questions

| Question | Owner | Due Date | Decision |
|----------|-------|----------|----------|
| [Unresolved question] | [Name] | YYYY-MM-DD | Pending |
| [Unresolved question] | [Name] | YYYY-MM-DD | Pending |

---

## Appendix

- Research: [Link to research document]
- Designs: [Link to Figma]
- Technical Design: [Link to TDD]
- Related PRDs: [Links]
- User Feedback: [Links to tickets, interviews, NPS comments]
