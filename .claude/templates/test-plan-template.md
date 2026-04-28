# Test Plan: [Feature Name]

**Feature:** [Feature or PRD reference]
**Author:** [Name]
**Date:** YYYY-MM-DD
**Status:** Draft | In Review | Approved

---

## Scope

### In Scope
- [What this test plan covers]
- [Which user flows, APIs, or components]

### Out of Scope
- [What is explicitly not covered — and why]

---

## Test Strategy

### Test Layer Distribution

| Layer | Count | Tools | Run in CI |
|-------|-------|-------|-----------|
| Unit tests | [Est. X] | Go testing + testify / Vitest | Yes (every PR) |
| Integration tests | [Est. Y] | Testcontainers / msw | Yes (every PR) |
| E2E tests | [Est. Z] | Playwright | Yes (main branch) |
| Performance tests | [Est. W] | k6 | Weekly / pre-release |

---

## Test Cases

### Unit Tests

#### [Component/Package Name]

| Test ID | Description | Input | Expected Output | Priority |
|---------|-------------|-------|----------------|----------|
| UT-001 | Happy path: [scenario] | [Input] | [Expected] | P0 |
| UT-002 | Error: [scenario] | [Input] | Error: [type] | P0 |
| UT-003 | Edge case: [scenario] | [Input] | [Expected] | P1 |

---

### Integration Tests

| Test ID | Description | Prerequisites | Expected Behavior | Priority |
|---------|-------------|---------------|-------------------|----------|
| IT-001 | [Test description] | [DB state / service state] | [Expected] | P0 |
| IT-002 | [Test description] | | | P0 |

---

### E2E Tests

| Test ID | User Journey | Steps | Expected Outcome | Priority |
|---------|-------------|-------|-----------------|----------|
| E2E-001 | [Core user flow] | 1. ... 2. ... 3. ... | [Final state] | P0 |
| E2E-002 | [Error flow] | 1. ... 2. ... | [Error message/behavior] | P1 |

---

### Edge Cases & Boundary Conditions

| Scenario | Test Type | Expected Behavior |
|----------|-----------|-------------------|
| Empty input | Unit | Returns validation error |
| Maximum input length | Unit | Truncates or errors with clear message |
| Concurrent requests | Integration | No data corruption, correct result |
| Network timeout | Unit/Integration | Retry per policy, surfaces error |
| [Other edge case] | | |

---

## Test Environment Requirements

### Infrastructure
- Database: [PostgreSQL 16 via Testcontainers]
- Cache: [Redis 7 via Testcontainers]
- External services: [msw handlers for third-party APIs]

### Test Data
- Seed data needed: [Yes / No — describe if yes]
- Data isolation: [Transaction rollback / Schema per test / Fresh DB per suite]

### Environment Variables
```bash
TEST_DATABASE_URL=postgres://...
TEST_REDIS_URL=redis://...
TEST_AUTH_TOKEN=...
```

---

## Acceptance Criteria for Testing

The feature is ready to ship when:

### Automated Gates (verified by CI)
- [ ] All P0 unit tests pass with `-race` flag
- [ ] All P0 integration tests pass
- [ ] All P0 E2E tests pass
- [ ] Code coverage: overall ≥ 70%, domain package ≥ 85%
- [ ] No data races detected by race detector
- [ ] No golangci-lint errors

### Manual Verification
- [ ] Happy path works end-to-end in staging environment
- [ ] Error messages are user-friendly (not raw error strings)
- [ ] Performance: [specific operation] < Xms in staging
- [ ] Accessibility: [key flows] pass automated axe audit

---

## Risks & Unknowns

| Risk | Impact | Mitigation |
|------|--------|-----------|
| [Test environment setup is complex] | Medium | Use docker-compose for local setup |
| [Third-party API is unreliable in staging] | High | Mock third-party in staging tests |
| [New feature has complex concurrency] | High | Add targeted -race tests |
