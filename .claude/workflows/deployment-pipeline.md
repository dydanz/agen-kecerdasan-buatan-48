# Workflow: Deployment Pipeline

The end-to-end deployment process from code merge to production, using GitOps as the delivery mechanism.

## Pipeline Overview

```
Code Merge to Main
    │
    ▼
[CI Pipeline — GitHub Actions]
    ├── Lint (Go + TS)
    ├── Unit Tests
    ├── Integration Tests
    ├── Security Scan (Trivy, truffleHog)
    ├── Build Docker Image
    ├── Scan Image (Trivy)
    └── Push to ECR
         │
         ▼
[Auto: Update Dev GitOps]
    │ Flux reconciles within 5-10min
    ▼
[Dev Environment]
    ├── Health check
    ├── Smoke test
    └── Monitor 15min
         │ (Manually triggered)
         ▼
[Staging Promotion PR]
    ├── Code review required
    ├── E2E tests in staging
    └── Stakeholder sign-off
         │ (Manually triggered with approval)
         ▼
[Production Deployment]
    ├── Deploy during window: weekdays 10am-3pm
    ├── Monitor 30min
    └── Rollback if needed
```

## Stage 1: CI Pipeline (Automated)

Triggered on every push to `main`.

```
Duration target: < 10 minutes
```

**Go Backend CI:**
```bash
golangci-lint run ./...          # Lint
go test -race -short ./...       # Unit tests
go test -race -run Integration   # Integration tests (with service containers)
go build -o /dev/null ./...      # Build check
```

**Frontend CI:**
```bash
pnpm typecheck                   # Type check
pnpm lint                        # ESLint
pnpm test:unit --run             # Unit tests
pnpm build                       # Build check
```

**Security:**
```bash
trivy fs . --severity CRITICAL,HIGH --exit-code 1
trufflehog git --since-commit HEAD~1
```

**Image Build:**
```bash
docker build --platform linux/amd64 -t $ECR_URI:$TAG .
trivy image $ECR_URI:$TAG --severity CRITICAL --exit-code 1
docker push $ECR_URI:$TAG
```

## Stage 2: Dev Deployment (Automated)

After CI succeeds on main, the CD step automatically:
1. Updates the image tag in the dev GitOps overlay
2. Commits and pushes to the GitOps repository
3. Flux reconciles within the next sync interval (≤ 5 minutes)

**Verify dev deployment:**
```bash
# Check Flux reconciliation status
flux get kustomization apps -n flux-system

# Check deployment rollout
kubectl rollout status deployment/api-server -n myapp-dev --timeout=5m

# Smoke test
curl -f https://api-dev.myapp.io/healthz/ready
```

**Monitor for 15 minutes:**
- Error rate in Grafana
- P99 latency
- Pod restart count

If any anomaly: investigate before promoting to staging.

## Stage 3: Staging Promotion

**When to promote:**
- Dev has been stable for 24 hours
- Feature is complete (all P0 acceptance criteria met in dev)
- QA sign-off obtained (for customer-facing features)

**Process:**
```bash
# GitOps promotion: update staging overlay with same image tag
# This is done via PR to the GitOps repository

# PR title: chore(deploy/staging): api-server → [TAG]
# Include: What changed, why, verification steps
```

After staging deployment:
1. Run E2E test suite against staging
2. Manual QA of new features against acceptance criteria
3. Performance validation (load test if SLO-affecting)
4. Stakeholder demo and sign-off if required

**Staging sign-off checklist:**
- [ ] E2E tests pass
- [ ] Acceptance criteria verified manually
- [ ] No performance regression (compare Grafana dashboards)
- [ ] Security scan: no new HIGH/CRITICAL findings
- [ ] Stakeholder sign-off (for major features)

## Stage 4: Production Deployment

**Deployment windows:**
- Weekdays only: 10:00 AM – 3:00 PM (local business hours)
- NO deployments: Fridays, public holidays, peak business periods
- Emergency hotfixes: any time, with on-call engineer monitoring

**Pre-deployment checklist:**
- [ ] Staging has been stable for 24 hours
- [ ] On-call engineer available to monitor and rollback
- [ ] Rollback plan confirmed: `git revert` + `flux reconcile`
- [ ] Monitoring dashboards open
- [ ] Stakeholders notified (for significant features)
- [ ] Database migrations: reviewed and rollback-safe

**Deploy process:**
```bash
# Create PR to prod GitOps overlay
# Requires: 2 reviewer approvals + tech lead sign-off

# After merge: Flux reconciles within 5-10 minutes
# Monitor during reconciliation
```

**Post-deployment monitoring (30 minutes):**
```
Watch in Grafana:
  - HTTP error rate: should not exceed 0.1%
  - P99 latency: should not increase > 20% from baseline
  - Pod restart count: should be 0
  - Active connections: normal range
  - Database connection pool: not exhausted

Watch in Loki:
  - ERROR level log count
  - New error patterns not seen before
```

## Rollback Procedure

**When to rollback:**
- Error rate > 1% (10x normal)
- P99 latency > 2x baseline
- Core feature broken for users
- Security issue discovered post-deploy

**Rollback steps:**
```bash
# 1. Revert the GitOps commit
git revert HEAD  # In GitOps repository
git push

# 2. Flux reconciles automatically — previous version redeploys
# Duration: ~5-10 minutes

# 3. Verify rollback succeeded
kubectl rollout status deployment/api-server -n myapp-prod
curl -f https://api.myapp.io/healthz/ready

# 4. Notify team of rollback and reason
```

**Database rollback:**
- If a migration was applied, run the down migration
- Only possible if migration was designed to be reversible
- This is why ALL migrations must have down scripts

## Deployment Constraints

- NEVER deploy on Fridays (no one to monitor over the weekend)
- NEVER deploy during peak traffic periods (check Grafana before scheduling)
- NEVER skip CI — no direct pushes to production image repository
- NEVER skip the dev → staging → prod promotion sequence for production features
- Hotfixes can skip staging for SEV1 only, with on-call engineer present
- All deployments must be GitOps — never `kubectl apply` directly
