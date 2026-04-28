---
name: project-overview
description: High-level project context — what we're building, why, tech stack, and team structure. Update this as the project evolves.
type: reference
---

# Project Overview

> **Update this file** as the project takes shape. This is the first context loaded in every session.

## What We're Building

[Describe the application: purpose, target users, core value proposition]

## Tech Stack

### Frontend
- Framework: [e.g., Next.js 15, React 19]
- Language: TypeScript
- Styling: [e.g., Tailwind CSS, shadcn/ui]
- State: [e.g., TanStack Query + Zustand]
- Testing: Vitest + Playwright

### Backend
- Language: Go [version]
- Architecture: Clean Architecture (hexagonal)
- Database: [e.g., PostgreSQL 16]
- Cache: [e.g., Redis 7]
- Messaging: [e.g., Kafka, RabbitMQ, or N/A]

### Infrastructure
- Cloud: AWS
- Container orchestration: Kubernetes (EKS)
- IaC: Terraform
- GitOps: Flux
- CI/CD: GitHub Actions

## Environments

| Environment | URL | AWS Account | Notes |
|-------------|-----|-------------|-------|
| Dev         | https://dev.example.com | [account-id] | Auto-deploy on merge |
| Staging     | https://staging.example.com | [account-id] | Deploy via PR |
| Production  | https://example.com | [account-id] | Manual approval |

## Team Structure

| Role | Name | Domain |
|------|------|--------|
| Product Manager | [Name] | Requirements, PRD |
| Tech Lead | [Name] | Architecture, review |
| Backend | [Names] | Go services |
| Frontend | [Names] | React/Next.js |
| Platform | [Names] | Infrastructure, CI/CD |

## Key Decisions (ADRs)

| Decision | Choice | Date | Rationale |
|----------|--------|------|-----------|
| [e.g., State management] | [Zustand + TanStack Query] | YYYY-MM-DD | [Brief reason] |
| [e.g., Go web framework] | [chi / echo / gin] | YYYY-MM-DD | [Brief reason] |

## Repository Structure

```
[project-root]/
├── frontend/        # Next.js / React application
├── backend/         # Go services
├── infrastructure/  # Terraform + K8s manifests
├── k8s-gitops/      # GitOps repository (or subfolder)
└── .claude/         # Claude Code configuration
```

## Development Setup

```bash
# Prerequisites
# - Go [version]+
# - Node.js [version]+
# - Docker + Docker Compose
# - kubectl + helm

# Start local development
docker compose up -d    # Start backing services (DB, Redis, etc.)
make run-backend        # Start Go server
cd frontend && npm dev  # Start frontend dev server
```

## Key Links

- Figma / Design: [URL]
- Linear / Jira: [URL]
- Notion / Confluence: [URL]
- Grafana Dashboard: [URL]
- Staging environment: [URL]
