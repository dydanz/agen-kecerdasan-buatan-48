# /setup-infra

Guides infrastructure setup — Terraform module design, Kubernetes manifests, CI/CD pipeline configuration, and observability stack deployment. Orchestrates infrastructure domain agents.

## When Invoked

If no target is specified:

```
What infrastructure would you like to set up or review?

1. AWS + Terraform (new environment or module)
2. Kubernetes cluster setup (EKS, node groups, namespaces)
3. GitOps configuration (Flux or ArgoCD)
4. CI/CD pipeline (GitHub Actions)
5. Observability stack (Prometheus, Grafana, Loki, Tempo)
6. Security configuration (IAM, secrets, network policies)
7. Full stack setup (1-6 in order)

Specify what you need, and I'll coordinate the relevant infrastructure agents.
```

## Infrastructure Setup Process

### Environment Context Gathering

Before proceeding, collect:
```
To design your infrastructure, I need:

1. Cloud provider: AWS (primary focus)
2. AWS regions: [e.g., ap-southeast-1]
3. Environments needed: [dev / staging / prod]
4. AWS Organization: Existing or new?
5. Existing VPC/networking: Or greenfield?
6. Team size (affects IAM complexity)
7. Compliance requirements: [SOC2, HIPAA, PCI-DSS, none]
8. Estimated initial load: [req/s, data volume]
```

### Phase 1: Foundation (Networking + IAM)

Invoke `terraform-architect` agent:
- VPC design with CIDR allocation
- Subnet strategy (public / private-app / private-data)
- NAT Gateway configuration
- AWS accounts and IAM role structure

Invoke `security` agent:
- IAM roles with least privilege
- Permission boundaries
- Service control policies (for multi-account)

Output: Terraform module structure for `vpc/` and `iam/`

### Phase 2: Compute (EKS Cluster)

Invoke `kubernetes-architect` agent:
- Node group design
- Cluster version and addons
- Namespace strategy

Invoke `networking` agent:
- Ingress controller setup
- Network policies
- cert-manager configuration

Output: EKS Terraform module + baseline K8s manifests

### Phase 3: GitOps Setup

Invoke `gitops` agent:
- Flux or ArgoCD bootstrap
- GitOps repository structure
- Environment promotion strategy
- Image update automation

Output: `.flux/` or Argo Application manifests

### Phase 4: CI/CD Pipeline

Invoke `cicd` agent:
- GitHub Actions workflow templates
- Build, test, security scan stages
- Docker build + ECR push
- GitOps update step

Output: `.github/workflows/` templates

### Phase 5: Observability Stack

Invoke `observability-infra` agent:
- kube-prometheus-stack configuration
- Loki + Promtail deployment
- Tempo configuration
- Grafana dashboards as code
- Alertmanager routing (Slack + PagerDuty)

Output: Helm values files + Kubernetes manifests

### Review and Validation

After generating infrastructure code:

```
Infrastructure setup complete. Before applying:

✅ Review checklist:
  - [ ] Terraform: `terraform validate` passes in each environment
  - [ ] Kubernetes: `kubectl --dry-run=client -f ./` passes
  - [ ] Secrets: No sensitive values in any committed file
  - [ ] IAM: Reviewed for least-privilege
  - [ ] Costs: Estimated monthly cost reviewed

⚠️ Destructive operations require your explicit approval:
  - `terraform apply` — run manually after reviewing the plan
  - GitOps bootstrap — reconciliation begins immediately
  - ECR repository creation — cannot be renamed after creation
```

## Important Guidelines

- Never run `terraform apply` automatically — always show plan first, wait for approval
- Never run `kubectl apply` directly — use GitOps for all cluster state changes
- All secrets must go through External Secrets Operator — never in git
- IAM policies must be reviewed for least-privilege before applying
- Infrastructure changes in production require change management process
- Estimate monthly costs before applying (use `infracost` or AWS Pricing Calculator)
