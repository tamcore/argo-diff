# Implementation Plan: argo-diff Go Service

## Overview

Transform the 734-line bash script into a Go HTTP service using ArgoCD's official Go library (v3) and pure Go implementations (zero exec calls). The service receives webhook payloads from GitHub Actions with all needed data, processes diffs asynchronously in-memory, and posts PR comments.

## Architecture

```
┌─────────────────────┐
│ GitHub Actions      │
│ Workflow            │
│ (with OIDC token)   │
└──────────┬──────────┘
           │ POST /webhook
           │ Authorization: Bearer <oidc_token>
           ▼
┌─────────────────────────────────────────────────┐
│ argo-diff Service                               │
│                                                 │
│  ┌─────────────┐   ┌──────────────┐           │
│  │ OIDC Auth   │   │ HTTP Server  │           │
│  │ Validator   │◄──┤ /webhook     │           │
│  └─────────────┘   │ /health      │           │
│                    │ /ready       │           │
│                    │ /metrics     │           │
│                    └──────┬───────┘           │
│                           │                    │
│                           ▼                    │
│                    ┌──────────────┐           │
│                    │ Job Queue    │           │
│                    │ (buffered)   │           │
│                    └──────┬───────┘           │
│                           │                    │
│                           ▼                    │
│                    ┌──────────────┐           │
│                    │ Worker Pool  │           │
│                    │ (goroutines) │           │
│                    └──────┬───────┘           │
│                           │                    │
│        ┌──────────────────┼──────────────┐   │
│        │                  │              │   │
│        ▼                  ▼              ▼   │
│  ┌──────────┐      ┌───────────┐  ┌────────┐│
│  │ Matcher  │─────►│ ArgoCD    │  │ GitHub ││
│  │          │      │ Client    │  │ Client ││
│  └──────────┘      └─────┬─────┘  └────────┘│
│                          │                   │
│                          ▼                   │
│                    ┌───────────┐            │
│                    │ Diff      │            │
│                    │ Engine    │            │
│                    └───────────┘            │
└─────────────────────────────────────────────┘
```

## Components

### 1. HTTP Server (`cmd/server/main.go`)

**Endpoints:**
- `POST /webhook` - Accept diff job requests (requires OIDC auth)
- `GET /health` - Health check (always returns 200 OK)
- `GET /ready` - Readiness check (checks worker pool status)
- `GET /metrics` - Prometheus metrics

**Configuration (Environment Variables):**
- `WORKER_COUNT` (default: 1) - Number of worker goroutines
- `QUEUE_SIZE` (default: 100) - Max buffered jobs
- `REPO_ALLOWLIST` (required) - Comma-separated list of allowed repos (supports wildcards: `owner/repo` or `org/*`)
- `PORT` (default: 8080) - HTTP server port
- `METRICS_PORT` (default: 9090) - Metrics server port

**Webhook Payload:**
```json
{
  "github_token": "ghp_xxx",
  "argocd_token": "xxx",
  "argocd_server": "argocd.example.com",
  "argocd_insecure": false,
  "repository": "owner/repo",
  "pr_number": 123,
  "base_ref": "master",
  "head_ref": "abc123def456",
  "changed_files": ["app1/deployment.yaml", "app2/service.yaml"],
  "workflow_name": "ArgoCD Diff"
}
```

**Response:**
- `202 Accepted` - Job queued successfully
- `400 Bad Request` - Invalid payload
- `401 Unauthorized` - Missing or invalid OIDC token
- `403 Forbidden` - Repository not in allowlist
- `503 Service Unavailable` - Queue full

### 2. OIDC Validation (`pkg/auth/oidc.go`)

**Purpose:** Validate GitHub Actions OIDC tokens to authenticate webhook requests

**Process:**
1. Extract JWT from `Authorization: Bearer <token>` header
2. Fetch GitHub OIDC JWKS from `https://token.actions.githubusercontent.com/.well-known/jwks`
3. Validate JWT signature using JWKS
4. Verify claims:
   - `iss`: `https://token.actions.githubusercontent.com`
   - `aud`: configurable (default: `https://github.com/<owner>`)
   - `exp`: not expired
   - `repository`: matches allowlist
5. Extract repository claim for authorization
6. Check against `REPO_ALLOWLIST` (exact match or wildcard)

**Wildcard Support:**
- `owner/repo` - Exact match
- `owner/*` - All repos in organization
- `*/*` - All repos (not recommended)

### 3. ArgoCD Client (`pkg/argocd/client.go`)

**Dependencies:** `github.com/argoproj/argo-cd/v3/pkg/apiclient`

**Functions:**
- `NewClient(server, token, insecure)` - Create gRPC-Web client
- `ListApplications(ctx)` - Get all ArgoCD applications
- `GetManifests(ctx, appName, revisions, sourcePositions)` - Fetch manifests with retry
  - Single-source: `--revision <sha>`
  - Multi-source: `--revisions <sha1> --source-positions 1 --revisions <sha2> --source-positions 2`
- Retry logic: 3 attempts with exponential backoff (5s → 10s)

**Key Considerations:**
- Handle both single-source and multi-source applications
- Use gRPC-Web for HTTP/HTTPS compatibility
- Support insecure mode for development

### 4. Application Matcher (`pkg/matcher/matcher.go`)

**Purpose:** Determine which ArgoCD applications are affected by changed files

**Algorithm:**
1. Filter apps by repository URL (case-insensitive comparison)
2. For each app, check if any changed file matches:
   - Path prefix: `changed_file` starts with `app.spec.source.path`
   - App definition: `applications/<app_name>.yaml`
   - Nested definition: `applications/*/<app_name>.yaml`
3. Return list of affected apps with metadata:
   - App name
   - Status (Synced/OutOfSync)
   - Health (Healthy/Degraded/Progressing/etc.)
   - Last sync revision
   - Namespace

**Multi-source Support:**
- Check all sources in `spec.sources[]` array
- Match if any source matches repository and path

### 5. Diff Engine (`pkg/diff/engine.go`)

**Dependencies:**
- `gopkg.in/yaml.v3` - YAML parsing
- `github.com/sergi/go-diff` - Unified diff generation

**Process:**
1. Fetch live manifests (current cluster state at last sync revision)
2. Fetch predicted manifests (what would be deployed with PR changes)
3. Split multi-doc YAML into individual resources:
   - Naming: `<namespace>_<name>_<kind>.yaml`
   - Use destination namespace if metadata.namespace not set
4. For each resource:
   - Skip if has `helm.sh/hook` annotation
   - Generate unified diff using `go-diff`
   - Filter redundant namespace changes
   - Extract metadata (apiVersion, kind, namespace, name)
5. Format as markdown:
   - Header with app status and health
   - ArgoCD UI link
   - Collapsible sections per resource
   - Status emojis (✅ Synced, ❌ OutOfSync, 💚 Healthy, 🔄 Progressing, etc.)
   - Workflow identifier comment: `<!-- argocd-diff-workflow: <name> -->`
   - Timestamp

**Markdown Structure:**
```markdown
## ArgoCD Diff Preview

**N** of **M** applications have changes

_Generated at 12:34PM UTC, 1 Feb 2026_

<!-- argocd-diff-workflow: ArgoCD Diff -->

---

### Application: `app-name`

**Status:** ✅ Synced | **Health:** 💚 Healthy

[View in ArgoCD](https://argocd.server/applications/argocd/app-name)

<details open>
<summary>===== apps/v1/Deployment namespace/name =====</summary>

\`\`\`diff
--- live.yaml
+++ predicted.yaml
@@ -10,7 +10,7 @@
     spec:
       containers:
       - name: app
-        image: old-image:1.0
+        image: new-image:2.0
\`\`\`
</details>
```

### 6. GitHub Client (`pkg/github/client.go`)

**Dependencies:**
- `github.com/google/go-github/v58/github`
- `golang.org/x/oauth2`

**Functions:**
- `NewClient(token)` - Create authenticated GitHub client
- `DeleteOldComments(ctx, owner, repo, prNumber, workflowName)` - Delete previous diff comments
  - Filter by: `github-actions[bot]` user and workflow identifier
  - Use pagination to get all comments
- `PostComment(ctx, owner, repo, prNumber, body)` - Post new comment
  - Auto-split if body > 60KB
  - Add workflow identifier to all parts
  - Format multi-part headers: "part N of M"
- `PostErrorComment(ctx, owner, repo, prNumber, err)` - Post error notification

**Comment Splitting:**
- Max size: 60,000 characters (conservative vs 65,536 limit)
- Split by application sections
- Each part gets header: `## ArgoCD Diff Preview (part N of M)`
- All parts include workflow identifier

### 7. Worker Pool (`pkg/worker/pool.go`)

**Purpose:** Process diff jobs asynchronously with concurrency control

**Structure:**
```go
type Job struct {
    Repository   string
    PRNumber     int
    BaseRef      string
    HeadRef      string
    ChangedFiles []string
    GitHubToken  string
    ArgoCD       ArgocdConfig
    WorkflowName string
}

type Pool struct {
    jobs    chan Job
    workers int
    metrics *Metrics
}
```

**Process Flow:**
1. Receive job from channel
2. Run matcher to find affected apps
3. For each app:
   - Fetch live manifests from ArgoCD
   - Fetch predicted manifests from ArgoCD
   - Generate diffs
4. Format markdown
5. Delete old GitHub comments
6. Post new GitHub comment(s)
7. On error: Post error comment to PR

**Metrics (Prometheus):**
- `jobs_total{repository="owner/repo", status="success|failure"}` - Total jobs processed
- `jobs_in_queue` - Current queue depth
- `processing_duration_seconds{repository="owner/repo"}` - Job processing time
- `argocd_api_calls_total{operation="list|manifests", status="success|failure"}` - ArgoCD API calls
- `github_api_calls_total{operation="list|create|delete", status="success|failure"}` - GitHub API calls

**Graceful Shutdown:**
- Listen for SIGTERM/SIGINT
- Stop accepting new jobs
- Drain queue (finish in-progress jobs)
- Close connections

### 8. Helm Chart (`charts/argo-diff/`)

**Files:**
```
charts/argo-diff/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── service-metrics.yaml
│   ├── ingress.yaml
│   ├── serviceaccount.yaml
│   ├── servicemonitor.yaml
│   └── poddisruptionbudget.yaml
```

**Key Configurations:**

**Deployment:**
- Image: `ghcr.io/tamcore/argo-diff:latest`
- Env vars: `WORKER_COUNT`, `QUEUE_SIZE`, `REPO_ALLOWLIST`
- Readiness probe: `GET /ready`
- Liveness probe: `GET /health`
- Resource limits: CPU 500m, Memory 512Mi (configurable)

**Services:**
- App service: Port 8080 (HTTP)
- Metrics service: Port 9090 (Prometheus)

**Ingress:**
- Path: `/webhook`
- TLS: Optional
- Annotations: Configurable

**ServiceMonitor:**
- Scrape interval: 30s
- Port: metrics (9090)

**PodDisruptionBudget:**
- MinAvailable: 1

### 9. GoReleaser Config (`.goreleaser.yaml`)

**Build Strategy:**
- Use `ko` for container image builds (no Dockerfile needed)
- Base image: Chainguard minimal (default when no base specified)
- Multi-arch: linux/amd64, linux/arm64

**Outputs:**
- Container images: `ghcr.io/tamcore/argo-diff:<version>`
- Debian packages: `argo-diff_<version>_amd64.deb`
- GitHub releases with changelog

**Key Configuration:**
```yaml
version: 2

builds:
  - id: argo-diff
    main: ./cmd/server
    binary: argo-diff
    env:
      - CGO_ENABLED=0
    goos:
      - linux
    goarch:
      - amd64
      - arm64

kos:
  - id: argo-diff
    repository: ghcr.io/tamcore/argo-diff
    bare: true
    preserve_import_paths: false
    platforms:
      - linux/amd64
      - linux/arm64

nfpms:
  - package_name: argo-diff
    file_name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Arch }}"
    vendor: tamcore
    homepage: https://github.com/tamcore/argo-diff
    maintainer: Philipp Born
    description: Async ArgoCD diff service for GitHub Actions
    license: MIT
    formats:
      - deb
    contents:
      - src: ./argo-diff
        dst: /usr/bin/argo-diff
```

### 10. Release Workflow (`.github/workflows/release.yaml`)

**Trigger:** Tags matching `v*` (e.g., `v1.0.0`)

**Steps:**
1. Checkout code
2. Setup Go
3. Setup ko
4. Login to ghcr.io
5. Run `goreleaser check`
6. Run `goreleaser release`

**Permissions:**
- `contents: write` - Create GitHub releases
- `packages: write` - Push to ghcr.io
- `id-token: write` - OIDC for signing (optional)

### 11. Workflow Examples (`docs/examples/`)

**GitHub Workflow Sample:**
```yaml
name: ArgoCD Diff
on:
  pull_request:
    branches: [master]

permissions:
  contents: read
  pull-requests: write
  id-token: write  # Required for OIDC

jobs:
  diff:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Get OIDC token
        id: oidc
        run: |
          OIDC_TOKEN=$(curl -s -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
            "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=https://github.com/${{ github.repository_owner }}" | \
            jq -r .value)
          echo "token=$OIDC_TOKEN" >> $GITHUB_OUTPUT

      - name: Get changed files
        id: changes
        run: |
          git fetch origin ${{ github.base_ref }}
          FILES=$(git diff --name-only origin/${{ github.base_ref }}...${{ github.sha }} | jq -R -s -c 'split("\n") | map(select(length > 0))')
          echo "files=$FILES" >> $GITHUB_OUTPUT

      - name: Trigger argo-diff
        run: |
          curl -X POST https://argo-diff.example.com/webhook \
            -H "Authorization: Bearer ${{ steps.oidc.outputs.token }}" \
            -H "Content-Type: application/json" \
            -d '{
              "github_token": "${{ secrets.GITHUB_TOKEN }}",
              "argocd_token": "${{ secrets.ARGOCD_TOKEN }}",
              "argocd_server": "argocd.example.com",
              "argocd_insecure": false,
              "repository": "${{ github.repository }}",
              "pr_number": ${{ github.event.pull_request.number }},
              "base_ref": "${{ github.base_ref }}",
              "head_ref": "${{ github.event.pull_request.head.sha }}",
              "changed_files": ${{ steps.changes.outputs.files }},
              "workflow_name": "ArgoCD Diff"
            }'
```

**Docker Run Example:**
```bash
docker run -d \
  -p 8080:8080 \
  -p 9090:9090 \
  -e WORKER_COUNT=3 \
  -e QUEUE_SIZE=100 \
  -e REPO_ALLOWLIST="myorg/*" \
  ghcr.io/tamcore/argo-diff:latest
```

**Systemd Service (for .deb installation):**
```ini
[Unit]
Description=ArgoCD Diff Service
After=network.target

[Service]
Type=simple
User=argo-diff
Environment="WORKER_COUNT=1"
Environment="QUEUE_SIZE=100"
Environment="REPO_ALLOWLIST=myorg/*"
ExecStart=/usr/bin/argo-diff
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## Implementation Order

1. ✅ Document implementation plan
2. Initialize Go dependencies (go.mod)
3. Create project structure
4. Implement OIDC validation (testable independently)
5. Implement ArgoCD client wrapper (testable with mock server)
6. Implement application matcher (unit testable)
7. Implement diff engine (unit testable)
8. Implement GitHub client (testable with mock server)
9. Implement worker pool (integration point)
10. Implement HTTP server (brings everything together)
11. Create Helm chart
12. Add goreleaser config
13. Create release workflow
14. Provide workflow examples

## Testing Strategy

- **Unit tests:** matcher, diff engine, OIDC validation
- **Integration tests:** Full workflow with mock ArgoCD/GitHub servers
- **Manual testing:** Deploy to dev cluster, trigger from real PR

## Dependencies Summary

```go
require (
    // ArgoCD
    github.com/argoproj/argo-cd/v3 v3.x.x
    
    // YAML
    gopkg.in/yaml.v3 v3.0.1
    
    // Git (if needed for local operations)
    github.com/go-git/go-git/v5 v5.11.0
    
    // Diff
    github.com/sergi/go-diff v1.3.1
    
    // GitHub
    github.com/google/go-github/v58 v58.0.0
    golang.org/x/oauth2 v0.15.0
    
    // OIDC/JWT
    github.com/golang-jwt/jwt/v5 v5.2.0
    github.com/lestrrat-go/jwx/v2 v2.0.21
    
    // Prometheus
    github.com/prometheus/client_golang v1.18.0
    
    // HTTP
    github.com/gorilla/mux v1.8.1 (or stdlib)
)
```

## Security Considerations

1. **OIDC Token Validation:** Always validate JWT signature and claims on every request
2. **Repository Allowlist:** Strictly enforce to prevent unauthorized diff requests
3. **Token Handling:** Never log tokens, clear from memory after use
4. **Rate Limiting:** Consider adding rate limiting per repository
5. **Input Validation:** Sanitize all inputs to prevent injection attacks

## Scalability Considerations

1. **Horizontal Scaling:** Service is stateless, can run multiple replicas
2. **Queue Management:** Bounded queue prevents memory exhaustion
3. **Worker Concurrency:** Tunable to match ArgoCD API capacity
4. **Metrics:** Monitor queue depth, processing time, error rates

## Future Enhancements

- Job persistence (Redis/database) for restarts
- Webhook retry mechanism
- Caching of ArgoCD app list (TTL-based)
- Support for GitLab, Bitbucket
- Web UI for job status
- Slack/Discord notifications
