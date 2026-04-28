# TRD: [System / Feature Name]

**Status:** Draft | In Review | Approved | Shipped
**Author:** [Name]
**Tech Lead:** [Name]
**Date:** YYYY-MM-DD
**Last Updated:** YYYY-MM-DD
**Related PRD:** [Link or N/A]

---

## 1. Overview

### Purpose
[1-2 sentences. What does this system/feature do and why does it exist?]

### Problem Statement
[What technical or product problem does this solve? Be specific — include pain points, scale, or operational burden.]

### Goals & Success Criteria
| Goal | Metric | Target |
|------|--------|--------|
| [e.g., Reduce latency] | [e.g., p99 response time] | [e.g., < 200ms] |
| [e.g., Increase reliability] | [e.g., error rate] | [e.g., < 0.1%] |

---

## 2. Scope

### In Scope
- [Capability or component explicitly covered]
- [Capability or component explicitly covered]

### Out of Scope
- [What this does NOT cover — prevents scope creep]
- [Future work explicitly deferred]

---

## 3. System Context

### High-Level Architecture
[Embed or link a diagram. Describe where this system sits in the broader landscape.]

```
[ASCII diagram or reference to attached diagram]
```

### Key Dependencies
| Dependency | Type | Purpose | Owner |
|------------|------|---------|-------|
| [Service / API / Library] | Internal / External | [What it provides] | [Team] |
| | | | |

---

## 4. Functional Requirements

### Core Behaviors
- **[REQ-01]** [The system shall…]
- **[REQ-02]** [The system shall…]
- **[REQ-03]** [The system shall…]

### Use Cases / User Flows
#### UC-01: [Name]
1. [Step]
2. [Step]
3. [Expected outcome]

#### UC-02: [Name]
1. [Step]
2. [Step]
3. [Expected outcome]

### Edge Cases
- [What happens when input is malformed / empty / oversized?]
- [What happens under partial failure of a dependency?]
- [Concurrency or race condition scenarios]
- [Retry / idempotency behavior]

---

## 5. Non-Functional Requirements

### Performance
| Metric | Requirement |
|--------|------------|
| Latency (p50) | [e.g., < 50ms] |
| Latency (p99) | [e.g., < 200ms] |
| Throughput | [e.g., 1,000 RPS sustained] |

### Scalability
[Expected growth rate. How does the system scale — horizontal, vertical, partitioned? What are the limits?]

### Reliability
| Metric | Target |
|--------|--------|
| Availability (SLA) | [e.g., 99.9%] |
| Error budget | [e.g., 43min/month] |
| RTO | [e.g., < 15 min] |
| RPO | [e.g., < 5 min] |

### Security
- [Authentication / authorization model]
- [Data classification (PII, sensitive, public)]
- [Encryption at rest / in transit requirements]
- [Threat vectors to mitigate]

### Compliance
- [Regulatory requirements: GDPR, SOC2, HIPAA, etc.]
- [Audit logging requirements]

---

## 6. Technical Design

### Proposed Architecture / Components
[Describe the major components, how they interact, and the rationale for the design.]

```
[Component diagram or pseudo-architecture]
```

### Data Model / Schema Changes
```sql
-- Example: new table or altered columns
CREATE TABLE example (
  id          UUID PRIMARY KEY,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### API Design
#### `[METHOD] /path/to/endpoint`
**Request:**
```json
{
  "field": "type — description"
}
```
**Response:**
```json
{
  "field": "type — description"
}
```
**Error codes:** `400` [reason], `401` [reason], `500` [reason]

### Key Technical Decisions & Tradeoffs
| Decision | Options Considered | Choice | Rationale |
|----------|--------------------|--------|-----------|
| [e.g., Storage engine] | [Option A vs B] | [Chosen] | [Why] |
| [e.g., Sync vs async] | [Option A vs B] | [Chosen] | [Why] |

---

## 7. Data & Storage

### Data Flow
```
[Source] → [Transform / Processing] → [Sink / Storage]
```

### Storage Choices
| Data Type | Store | Justification |
|-----------|-------|---------------|
| [e.g., Events] | [e.g., Kafka → ClickHouse] | [e.g., Append-only, analytical queries] |
| [e.g., State] | [e.g., PostgreSQL] | [e.g., ACID, relational] |
| [e.g., Cache] | [e.g., Redis] | [e.g., Low-latency reads] |

### Data Lifecycle
| Data Type | Retention | Deletion Policy |
|-----------|-----------|-----------------|
| [Type] | [e.g., 90 days] | [e.g., Hard delete on expiry] |
| [Type] | [e.g., 7 years] | [e.g., Archive to cold storage] |

---

## 8. Deployment & Operations

### Environments
| Environment | Purpose | Notes |
|-------------|---------|-------|
| dev | Local development | [Any special config] |
| staging | Pre-prod validation | [Traffic source / data] |
| prod | Live traffic | [Guardrails] |

### CI/CD Considerations
- [Build steps, test gates, lint/type checks required]
- [Any manual approval gates]
- [Secrets / environment variable management]

### Rollout Strategy
- [ ] Feature flag gating
- [ ] Canary deployment (X% of traffic)
- [ ] Blue/green switchover
- [ ] Full rollout criteria: [what must be true before 100%]

### Monitoring & Alerting
| Signal | Tool | Alert Condition | On-call Action |
|--------|------|----------------|----------------|
| [e.g., Error rate] | [e.g., Prometheus] | [e.g., > 1% for 5m] | [Runbook link] |
| [e.g., Queue depth] | [e.g., Grafana] | [e.g., > 10k msgs] | [Runbook link] |

---

## 9. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| [e.g., Dependency unavailable] | Medium | High | [Circuit breaker, fallback] |
| [e.g., Schema migration fails] | Low | High | [Blue/green + rollback script] |
| [e.g., Traffic spike] | Medium | Medium | [Rate limiting, autoscaling] |

---

## 10. Open Questions

| # | Question | Owner | Due | Status |
|---|----------|-------|-----|--------|
| 1 | [Unresolved decision or assumption] | [Name] | YYYY-MM-DD | Open |
| 2 | | | | |

---

## 11. Success Metrics

### Launch Criteria (Go / No-Go)
- [ ] [e.g., p99 latency < 200ms under load test]
- [ ] [e.g., Zero data-loss events in staging soak]
- [ ] [e.g., Rollback tested and confirmed < 5 min]

### Post-Launch KPIs
| KPI | Baseline | Target | Measurement Window |
|-----|----------|--------|-------------------|
| [e.g., Error rate] | [Current] | [Goal] | [e.g., 30 days post-launch] |
| [e.g., p99 latency] | [Current] | [Goal] | [e.g., 30 days post-launch] |
| [e.g., Adoption / usage] | [N/A] | [Goal] | [e.g., 60 days post-launch] |

---

## Appendix

### Glossary
| Term | Definition |
|------|------------|
| [Term] | [Definition] |

### References
- [Link to PRD]
- [Link to architecture diagram]
- [Link to related ADRs]
- [Relevant runbooks or past incidents]
