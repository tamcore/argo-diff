# Implementation TODO

Remaining work to complete the argo-diff implementation as per [IMPLEMENTATION.md](IMPLEMENTATION.md).

## Phase 1: Core Feature Improvements ✅

### 1.1 Multi-part Comment Support ✅
**File:** `pkg/github/client.go`

- [x] Split large comments at application boundaries (not mid-diff)
- [x] Add multi-part headers: `## ArgoCD Diff Preview (part N of M)`
- [x] Include workflow identifier in all parts: `<!-- argocd-diff-workflow: <name> -->`
- [x] Update `DeleteOldComments` to find comments by workflow name

### 1.2 Enhanced Diff Output ✅
**File:** `pkg/diff/engine.go`

- [x] Add ArgoCD UI link per application: `[View in ArgoCD](https://server/applications/namespace/app)`
- [x] Include app status and health with emojis (✅ Synced, ❌ OutOfSync, 💚 Healthy, 🔄 Progressing)
- [x] Add generation timestamp to output
- [x] Show summary: "**N** of **M** applications have changes"

### 1.3 Application Definition Matching ✅
**File:** `pkg/matcher/matcher.go`

- [x] Match changes to `applications/<app_name>.yaml`
- [x] Match nested definitions: `applications/*/<app_name>.yaml`
- [x] Return app metadata (status, health, last sync revision, namespace)

### 1.4 Multi-source Manifest Support ✅
**File:** `pkg/argocd/client.go`

- [x] Add `GetMultiSourceManifests(ctx, appName, revisions, sourcePositions)` method
- [x] Handle `--revisions` and `--source-positions` for multi-source apps
- [x] Update `processJob` to detect and handle multi-source applications

---

## Phase 2: Observability ✅

### 2.1 Prometheus Metrics ✅
**File:** `pkg/metrics/metrics.go` (new)

- [x] `argo_diff_jobs_total{repository, status}` - Counter for processed jobs
- [x] `argo_diff_jobs_in_queue` - Gauge for current queue depth
- [x] `argo_diff_processing_duration_seconds{repository}` - Histogram for job duration
- [x] `argo_diff_argocd_api_calls_total{operation, status}` - Counter for ArgoCD API calls
- [x] `argo_diff_github_api_calls_total{operation, status}` - Counter for GitHub API calls

### 2.2 Structured Logging ✅
**File:** `cmd/server/main.go`

- [x] Replace `log.Printf` with structured logger (slog or zerolog)
- [x] Add request IDs for tracing
- [x] Log job metadata consistently

---

## Phase 3: Deployment Infrastructure

### 3.1 Helm Chart
**Directory:** `charts/argo-diff/`

```
charts/argo-diff/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── _helpers.tpl
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── service-metrics.yaml
│   ├── ingress.yaml
│   ├── serviceaccount.yaml
│   ├── servicemonitor.yaml
│   └── poddisruptionbudget.yaml
```

- [ ] Create Chart.yaml with metadata
- [ ] Create values.yaml with sensible defaults
- [ ] Deployment with health/readiness probes, resource limits
- [ ] Services for app (8080) and metrics (9090)
- [ ] Optional Ingress with TLS support
- [ ] ServiceMonitor for Prometheus Operator
- [ ] PodDisruptionBudget (minAvailable: 1)

### 3.2 GoReleaser Configuration
**File:** `.goreleaser.yaml`

- [ ] Binary builds for linux/amd64, linux/arm64
- [ ] Container images via `ko` to `ghcr.io/tamcore/argo-diff`
- [ ] Debian packages (.deb)
- [ ] Changelog generation

### 3.3 GitHub Actions Workflows
**Directory:** `.github/workflows/`

- [ ] `ci.yaml` - Run tests, lint, vet on PRs
- [ ] `release.yaml` - GoReleaser on version tags

---

## Phase 4: Documentation & Examples

### 4.1 Workflow Examples
**Directory:** `docs/examples/`

- [ ] `github-workflow.yaml` - Complete GitHub Actions workflow
- [ ] `docker-compose.yaml` - Local development setup
- [ ] `systemd.service` - Systemd unit file for .deb installs

### 4.2 Kubernetes Examples
**Directory:** `examples/kubernetes/`

- [ ] `deployment.yaml` - Basic deployment manifest
- [ ] `secret.yaml` - Example secret structure
- [ ] `ingress.yaml` - Ingress example

---

## Phase 5: Optional Enhancements

### 5.1 Worker Pool Refactor
**File:** `pkg/worker/pool.go` (new)

- [ ] Extract worker pool to dedicated package
- [ ] Add pool status reporting for `/ready` endpoint
- [ ] Implement graceful drain with timeout

### 5.2 Security Hardening

- [ ] Add rate limiting per repository
- [ ] Token sanitization in logs
- [ ] Input validation hardening

---

## Implementation Order

1. **Phase 1.2** - Enhanced diff output (quick win, improves UX)
2. **Phase 2.1** - Prometheus metrics (needed for production)
3. **Phase 3.1** - Helm chart (deployment blocker)
4. **Phase 3.2** - GoReleaser (release blocker)
5. **Phase 3.3** - CI workflows (automation)
6. **Phase 1.1** - Multi-part comments (edge case)
7. **Phase 1.3** - App definition matching (nice to have)
8. **Phase 1.4** - Multi-source support (advanced)
9. **Phase 4** - Documentation
10. **Phase 5** - Optional enhancements

---

## Testing Requirements

Each phase should include:
- [ ] Unit tests for new functionality
- [ ] Update existing tests if behavior changes
- [ ] Manual verification in dev environment

