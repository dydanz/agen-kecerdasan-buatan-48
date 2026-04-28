# Workflow: Feature Development Lifecycle

The day-to-day development cycle for implementing a feature once the PRD and TDD are approved.

## Overview

```
TDD Approved → Branch → Implement → Test → Review → Merge → Deploy Dev
```

## Prerequisites

Before starting implementation — **no code written until all boxes are checked:**
- [ ] PRD is approved (`.claude/thoughts/prd/`)
- [ ] TRD is approved (`.claude/thoughts/trd/`) — generated from `templates/trd-template.md`
- [ ] TRD Open Questions section is empty (all resolved before approval)
- [ ] Feature is broken into implementation tasks (via `/plan-feature`)
- [ ] All NFR targets in TRD are measurable numbers (not qualitative descriptions)

## Step 1: Branch Setup

```bash
# Create feature branch from main
git checkout main && git pull
git checkout -b feature/[ticket-id]-[brief-description]

# Example:
git checkout -b feature/ENG-123-user-profile-editing
```

## Step 2: Backend Implementation (Go)

**Order matters — follow dependency direction:**

```
1. Domain layer (no external dependencies)
   - Entity definitions (domain/[entity]/entity.go)
   - Domain service interface (domain/[entity]/service.go)
   - Repository interface (domain/[entity]/repository.go)
   - Domain errors (domain/[entity]/errors.go)

2. Infrastructure adapters
   - DB repository implementation (infrastructure/persistence/[entity]_repo.go)
   - Migrations (infrastructure/persistence/migrations/)
   - External API clients (if needed)

3. Application layer
   - Use case handler (application/[usecase]/handler.go)
   - DTOs (application/[usecase]/dto.go)

4. HTTP layer (thin — just wire and serialize)
   - HTTP handler (infrastructure/http/handler/[entity]_handler.go)
   - Route registration

5. Tests (write alongside each layer)
   - Unit tests for domain service
   - Integration tests for repository
   - HTTP handler tests
```

**At each step:**
- Run `go build ./...` to catch compilation errors
- Run `go test -short ./...` to run fast tests
- Run `go vet ./...` to catch common mistakes

## Step 3: Frontend Implementation

**Order matters — start from data, end at UI:**

```
1. API hooks (features/[name]/api/)
   - Define types (matching backend API contract)
   - Implement TanStack Query hooks
   - Test with msw mock handlers

2. State management (if needed)
   - Zustand slice for UI state
   - Not needed for server state (TanStack Query handles it)

3. UI components (features/[name]/components/)
   - Start with the main component
   - Add loading state (skeleton)
   - Add error state (error boundary or inline error)
   - Add empty state

4. Page integration (app/routes/ or pages/)
   - Wire components to routing
   - Add page-level tests

5. E2E test
   - Happy path test
   - Key error path test
```

**At each step:**
- Run `pnpm typecheck` to catch type errors
- Run `pnpm test:unit --run` to run fast tests
- Preview in browser with real API or msw mocks

## Step 4: Self-Review Checklist

Before requesting a code review:

```
Backend:
  [ ] `go test -race ./...` passes
  [ ] `golangci-lint run ./...` passes (zero warnings)
  [ ] `go vet ./...` passes
  [ ] New code has unit tests
  [ ] New DB queries have integration tests
  [ ] Error paths are tested
  [ ] No sensitive data in logs
  [ ] Context is propagated through all calls
  [ ] Graceful shutdown not broken

Frontend:
  [ ] `pnpm typecheck` passes (zero errors)
  [ ] `pnpm test:unit --run` passes
  [ ] Mobile view looks correct (check at 375px)
  [ ] Loading state is implemented
  [ ] Error state is implemented
  [ ] Empty state is implemented
  [ ] Accessibility: keyboard navigation works

General:
  [ ] No TODO comments (convert to tickets)
  [ ] No debug logs or console.log left in
  [ ] Environment variables documented
  [ ] PR description explains the change clearly
```

## Step 5: Code Review

Invoke `/review-code` for a self-review, then request team review.

**PR description template:**
```markdown
## What
[What change was made]

## Why
[Why this change was needed — link to PRD/ticket]

## How
[Key technical decisions made]

## Testing
[How to test this manually]

## Checklist
- [ ] Tests added/updated
- [ ] No breaking changes (or migration documented)
- [ ] Docs updated (if applicable)
```

## Step 6: Merge and Observe

After merging to main:
1. CI runs automatically → wait for green
2. GitOps updates dev environment within 5-10 minutes
3. Check Grafana dashboard for dev environment
4. Verify feature works in dev environment
5. If any error spike: investigate immediately (don't wait for reports)

## Handling Blockers

If you hit a blocker:
1. Document the specific obstacle clearly
2. Check `.claude/learnings.md` for related past discoveries
3. Use `/analyze-codebase` to understand the affected area
4. Ask for help early — don't spend > 2 hours blocked without asking

If the TRD is wrong or incomplete (discovered during implementation):
1. Do NOT silently deviate — stop implementation immediately
2. Update the TRD first (`.claude/thoughts/trd/`)
3. Get tech lead re-approval on the changed sections
4. Update the implementation plan if scope changed, then continue
