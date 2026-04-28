# Workflow: Bug Triage & Fix

The process for identifying, understanding, and fixing bugs — from first report to verified fix in production.

## Severity Levels

| Severity | Definition | Response Time | Fix Target |
|----------|-----------|---------------|-----------|
| SEV1 - Critical | Data loss, security breach, service completely down | 30 minutes | Hotfix < 4h |
| SEV2 - High | Core feature broken, large % of users affected | 2 hours | Fix < 24h |
| SEV3 - Medium | Feature partially broken, workaround exists | 24 hours | Fix < 7 days |
| SEV4 - Low | Minor issue, cosmetic, edge case | 72 hours | Next sprint |

## Phase 1: Triage

When a bug is reported:

**Gather:**
1. Exact steps to reproduce
2. Expected behavior vs actual behavior
3. Environment (prod / staging / dev / local)
4. Frequency: always / sometimes / once
5. User impact: how many users affected?
6. First occurrence: when did this start?

**Check:**
- Is this a known issue? (search existing tickets)
- Was there a recent deployment? (check git log)
- Is there a spike in error metrics? (check Grafana)
- Is it isolated or widespread?

**Assign severity** using the table above.

## Phase 2: Investigation

```
1. Reproduce the bug locally or in staging
   - If you can't reproduce it, you can't reliably fix it
   - Use logs: find the request ID from the error report, trace through logs

2. Identify the root cause
   - Find the specific code path that produces the wrong behavior
   - Use: git log, git blame, grep, read related tests
   - Do NOT assume the cause — confirm with evidence

3. Write a failing test FIRST (TDD for bug fixes)
   - The test must fail with the current code
   - The test must pass after the fix
   - The test prevents regression

4. Fix the root cause (not the symptom)
   - If the fix feels like a workaround, keep digging
   - Consider: could this bug affect other code paths too?
```

## Phase 3: Fix Implementation

**Branch naming:**
```bash
git checkout -b fix/[ticket-id]-[brief-description]
# Example: fix/BUG-456-order-status-not-updating
```

**Fix checklist:**
- [ ] Failing test written first
- [ ] Root cause identified and fixed (not symptom)
- [ ] Fix doesn't break existing tests (`go test -race ./...`)
- [ ] Fix doesn't introduce new linting issues
- [ ] Test added specifically for this bug scenario
- [ ] Consider: are there other places with the same bug pattern?

**For SEV1/SEV2 — Hotfix Process:**
```bash
# Branch from main (which is what's in production)
git checkout -b hotfix/[ticket-id]-[description] main

# Implement fix + test
# Get fast code review (1 reviewer minimum)
# Deploy directly to production via expedited GitOps PR
# Monitor for 30 minutes
# Then backport to any active release branches
```

## Phase 4: Verification

Before marking fixed:

```
Automated:
  [ ] Failing test now passes
  [ ] All existing tests still pass
  [ ] No new linting errors

Manual:
  [ ] Bug cannot be reproduced using original repro steps
  [ ] Adjacent scenarios still work correctly
  [ ] Tested in staging environment

Production verification (after deploy):
  [ ] Error rate returned to baseline in Grafana
  [ ] No new errors introduced
  [ ] Specific error type no longer appearing in Loki logs
```

## Phase 5: Post-Mortem (SEV1/SEV2)

For SEV1 and SEV2 bugs, write a brief post-mortem within 5 business days:

```markdown
## Post-Mortem: [Bug Title]
Date: YYYY-MM-DD
Severity: SEV1 / SEV2
Duration: [time from detection to resolution]

### What Happened
[Timeline of events]

### Root Cause
[Technical root cause — be specific, not vague]

### Contributing Factors
[What allowed this bug to reach production]
- [ ] Missing test coverage?
- [ ] Inadequate code review?
- [ ] Missing monitoring/alerting?
- [ ] Deploy process gap?

### Resolution
[What was done to fix it]

### Action Items (with owners and due dates)
- [ ] [Preventive action] — Owner: [Name] — Due: YYYY-MM-DD
- [ ] [Monitoring improvement] — Owner: [Name] — Due: YYYY-MM-DD
- [ ] [Process improvement] — Owner: [Name] — Due: YYYY-MM-DD
```

Save post-mortems to `.claude/thoughts/incidents/YYYY-MM-DD-[brief-title].md`

## Important Guidelines

- Never fix a bug without first writing a test that fails — the test is proof the bug existed
- Never push a hotfix without at least one code review (even async is acceptable for SEV1)
- Root cause analysis must be actual root cause — "the code was wrong" is not an answer
- Post-mortems are blameless — focus on systems and processes, not individuals
- Document in `.claude/learnings.md` any non-obvious lessons from the bug investigation
