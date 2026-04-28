# ADR-[NUMBER]: [Decision Title]

**Date:** YYYY-MM-DD
**Status:** Proposed | Accepted | Deprecated | Superseded by ADR-[NUMBER]
**Deciders:** [Names of people involved in the decision]
**Tags:** [backend] [frontend] [infrastructure] [security] [data]

---

## Context

[Describe the situation that requires a decision. What forces are at play?
What constraints exist (technical, business, timeline, team capability)?
What is the consequence of NOT making a decision?]

## Decision Drivers

- [Force 1: e.g., "We need to handle 10,000 concurrent connections"]
- [Force 2: e.g., "Team has limited Kubernetes experience"]
- [Force 3: e.g., "Must be compliant with SOC2 Type II by Q3"]
- [Force 4: e.g., "Budget constraint: managed service cost < $500/month"]

---

## Options Considered

### Option A: [Name]

[Description of the approach]

**Advantages:**
- [Concrete advantage 1]
- [Concrete advantage 2]

**Disadvantages:**
- [Concrete disadvantage 1]
- [Concrete disadvantage 2]

**Best when:** [Conditions where this is the right choice]

---

### Option B: [Name]

[Description of the approach]

**Advantages:**
- [Concrete advantage 1]
- [Concrete advantage 2]

**Disadvantages:**
- [Concrete disadvantage 1]
- [Concrete disadvantage 2]

**Best when:** [Conditions where this is the right choice]

---

### Option C: [Name] *(if applicable)*

[Description]

---

## Decision

**We choose: Option [X] — [Name]**

[Clear statement of what was decided, without hedging]

**Rationale:**
[2-4 sentences explaining WHY this option was chosen over the others.
Connect to the decision drivers above. Be specific.]

---

## Consequences

### Positive
- [What becomes easier or better as a result]
- [What technical debt is resolved]
- [What new capabilities are unlocked]

### Negative (Accepted Trade-offs)
- [What becomes harder or more complex]
- [What constraints this introduces on future decisions]
- [What we are NOT able to do with this choice]

### Risks & Mitigations
| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| [Risk 1] | High/Med/Low | [Mitigation approach] |
| [Risk 2] | | |

---

## Implementation Notes

[Any specific guidance for teams implementing this decision.
References to relevant code patterns, configuration, or documentation.]

---

## Review & Revisit

**Revisit this decision if:**
- [Condition 1 that would invalidate this choice, e.g., "traffic exceeds 100k req/s"]
- [Condition 2, e.g., "team grows beyond 20 engineers"]
- [Condition 3, e.g., "cost exceeds $X/month"]

**Related ADRs:**
- [ADR-NUMBER: Related decision]
