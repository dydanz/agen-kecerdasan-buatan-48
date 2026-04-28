---
name: tech-stack
description: Detailed technology choices, versions, and conventions for each layer of the stack
type: reference
---

# Technology Stack Reference

## Frontend

### Core
| Technology | Version | Purpose |
|-----------|---------|---------|
| Next.js | 15.x | React framework (App Router) |
| React | 19.x | UI library |
| TypeScript | 5.x | Type safety |

### Styling
| Technology | Version | Purpose |
|-----------|---------|---------|
| Tailwind CSS | 3.x | Utility-first CSS |
| shadcn/ui | latest | Accessible component library |

### State & Data
| Technology | Version | Purpose |
|-----------|---------|---------|
| TanStack Query | 5.x | Server state, caching, data fetching |
| Zustand | 4.x | Global client state |
| React Hook Form | 7.x | Form state management |
| Zod | 3.x | Schema validation |

### Testing
| Technology | Version | Purpose |
|-----------|---------|---------|
| Vitest | latest | Unit & integration test runner |
| @testing-library/react | latest | Component testing |
| msw | 2.x | API mocking at network level |
| Playwright | latest | E2E browser testing |

## Backend (Go)

### Core
| Technology | Version | Purpose |
|-----------|---------|---------|
| Go | 1.23+ | Application language |
| chi | 5.x | HTTP router (lightweight, idiomatic) |
| pgx | 5.x | PostgreSQL driver |
| sqlc | 2.x | Type-safe SQL query generation |
| golang-migrate | 4.x | Database migrations |

### Observability
| Technology | Version | Purpose |
|-----------|---------|---------|
| slog | stdlib | Structured logging |
| OpenTelemetry | latest | Distributed tracing |
| prometheus/client_golang | 1.x | Metrics exposition |

### Testing
| Technology | Version | Purpose |
|-----------|---------|---------|
| testify | 1.x | Assertions and test suites |
| gomock | 0.x | Interface mocking |
| testcontainers-go | 0.x | Real DB in integration tests |

### Code Quality
| Technology | Version | Purpose |
|-----------|---------|---------|
| golangci-lint | 1.62+ | Linting (multi-linter) |
| govulncheck | latest | Vulnerability scanning |

## Infrastructure

### Cloud
| Technology | Version | Purpose |
|-----------|---------|---------|
| AWS | — | Primary cloud provider |
| Terraform | 1.9+ | Infrastructure as Code |
| AWS Provider | 5.x | Terraform AWS resources |

### Kubernetes
| Technology | Version | Purpose |
|-----------|---------|---------|
| EKS | 1.31 | Managed Kubernetes |
| Flux | 2.x | GitOps reconciliation |
| NGINX Ingress | 1.x | Ingress controller |
| cert-manager | 1.x | TLS certificate automation |
| External Secrets Operator | 0.x | Secret sync from AWS |
| External DNS | 0.x | DNS sync from Ingress |

### Observability Stack
| Technology | Version | Purpose |
|-----------|---------|---------|
| Prometheus | 2.x | Metrics collection & alerting |
| Grafana | 10.x | Dashboards & visualization |
| Loki | 3.x | Log aggregation |
| Tempo | 2.x | Distributed tracing storage |
| Alertmanager | 0.x | Alert routing |

## Conventions

### Go
- Package naming: `lowercase`, no underscores except `_test`
- Error handling: always wrap with context using `fmt.Errorf("context: %w", err)`
- Testing: table-driven tests with `t.Run()` subtests
- Imports: stdlib first, then external, then internal (separated by blank lines)

### TypeScript/React
- Component naming: `PascalCase`
- Hook naming: `camelCase` with `use` prefix
- File naming: `kebab-case.tsx` for components, `camelCase.ts` for utilities
- Exports: named exports preferred over default exports (better refactor support)

### Git
- Branch naming: `feature/[ticket]-description`, `fix/[ticket]-description`, `chore/description`
- Commit format: `type(scope): description` (conventional commits)
  - Types: feat, fix, chore, docs, style, refactor, test, perf
- PR size: ideally < 400 lines changed — split larger changes

### Database
- Migration naming: `YYYYMMDDHHMMSS_description.sql`
- All migrations must be reversible (include down migration)
- Column naming: `snake_case`
- Boolean columns: `is_`, `has_`, `can_` prefix
- Timestamps: `created_at`, `updated_at` on all tables (TIMESTAMPTZ)
