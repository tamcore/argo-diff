# argo-diff

Async Go service for generating ArgoCD application diffs on GitHub Pull Requests.

## Features

- **Async Processing**: HTTP webhook server with worker pool for parallel job processing
- **OIDC Authentication**: Secure token validation using GitHub's OIDC provider
- **Smart Matching**: Automatically identifies ArgoCD applications affected by PR changes
- **Diff Generation**: Generates detailed YAML diffs with markdown formatting
- **GitHub Integration**: Posts formatted diff reports as PR comments
- **Prometheus Metrics**: Built-in metrics endpoint for monitoring

## Architecture

```
GitHub Webhook → OIDC Validation → Job Queue → Worker Pool → ArgoCD + GitHub APIs
```

### Components

- **HTTP Server** ([cmd/server/main.go](cmd/server/main.go)): Webhook endpoint, health checks, metrics
- **Config** ([pkg/config/](pkg/config/)): Environment-based configuration with validation
- **Auth** ([pkg/auth/](pkg/auth/)): OIDC token validation and JWT extraction
- **Worker** ([pkg/worker/](pkg/worker/)): Job queue types and processing logic
- **ArgoCD Client** ([pkg/argocd/](pkg/argocd/)): gRPC-Web client for ArgoCD API
- **Matcher** ([pkg/matcher/](pkg/matcher/)): Matches changed files to affected applications
- **Diff Engine** ([pkg/diff/](pkg/diff/)): Generates YAML diffs with markdown output
- **GitHub Client** ([pkg/github/](pkg/github/)): GitHub API client for PR comments

## Configuration

All configuration is via environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | HTTP server port | `8080` |
| `METRICS_PORT` | Metrics server port | `9090` |
| `WORKER_COUNT` | Number of worker goroutines | `5` |
| `QUEUE_SIZE` | Job queue buffer size | `100` |
| `REPO_ALLOWLIST` | Comma-separated list of allowed repos | *(required)* |
| `GITHUB_OIDC_ISSUER` | GitHub OIDC issuer URL | `https://token.actions.githubusercontent.com` |
| `ARGOCD_SERVER` | ArgoCD server address | `argocd-server:80` |
| `ARGOCD_INSECURE` | Skip TLS verification | `true` |

## API

### POST /webhook

Accepts webhook payloads to generate diffs.

**Headers:**
- `Authorization: Bearer <github-oidc-token>`

**Request Body:**
```json
{
  "github_token": "ghp_...",
  "argocd_token": "argocd.token...",
  "repository": "owner/repo",
  "pr_number": 123,
  "base_ref": "abc123",
  "head_ref": "def456",
  "changed_files": ["apps/app1/deployment.yaml"],
  "workflow_name": "ArgoCD Diff",
  "dedupe_diffs": true,
  "argocd_url": "https://argocd.example.com",
  "ignore_argocd_tracking": true
}
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `github_token` | Yes | - | GitHub token for posting PR comments |
| `argocd_token` | Yes | - | ArgoCD API token |
| `repository` | Yes | - | Repository in `owner/repo` format |
| `pr_number` | Yes | - | Pull request number |
| `base_ref` | Yes | - | Base commit SHA |
| `head_ref` | Yes | - | Head commit SHA |
| `changed_files` | Yes | - | List of changed file paths |
| `workflow_name` | No | `"ArgoCD Diff"` | Workflow identifier for comment management |
| `dedupe_diffs` | No | `true` | Deduplicate identical diffs across apps (shows "Same diff as X") |
| `argocd_url` | No | - | ArgoCD UI URL for "View in ArgoCD" links (omitted if not set) |
| `ignore_argocd_tracking` | No | `false` | Ignore `argocd.argoproj.io/*` labels and annotations in diffs (useful for deduplication) |

**Response:**
```json
{
  "status": "accepted",
  "message": "Job queued for owner/repo PR #123"
}
```

### GET /health

Health check endpoint.

### GET /ready

Readiness check endpoint.

### GET /metrics

Prometheus metrics endpoint (served on `METRICS_PORT`).

## Development

### Prerequisites

- Go 1.25+
- golangci-lint
- Access to an ArgoCD instance
- GitHub repository with OIDC configured

### Build

```bash
go build -o bin/argo-diff ./cmd/server
```

### Run

```bash
export REPO_ALLOWLIST="owner/repo1,owner/repo2"
./bin/argo-diff
```

### Test

```bash
go test ./...
```

### Code Quality

Before committing, always run:

```bash
go fmt ./...
go vet ./...
golangci-lint run ./...
go test ./...
```

See [AGENTS.md](AGENTS.md) for semantic commit guidelines.

## Deployment

### Helm Chart

```bash
helm install argo-diff ./charts/argo-diff \
  --set allowedRepos="myorg/*" \
  --set argocd.server="argocd-server:80"
```

### Kubernetes

See [examples/kubernetes/](examples/kubernetes/) for deployment manifests.

## GitHub Workflow Integration

Example workflow to trigger argo-diff:

```yaml
name: ArgoCD Diff
on:
  pull_request:
    paths:
      - 'applications/**'

permissions:
  id-token: write
  pull-requests: write

jobs:
  diff:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Get changed files
        id: changed
        run: |
          files=$(git diff --name-only ${{ github.event.pull_request.base.sha }} ${{ github.sha }} | jq -R -s -c 'split("\n")[:-1]')
          echo "files=$files" >> $GITHUB_OUTPUT

      - name: Get OIDC token
        id: oidc
        run: |
          token=$(curl -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=argo-diff" | jq -r .value)
          echo "::add-mask::$token"
          echo "token=$token" >> $GITHUB_OUTPUT

      - name: Trigger diff
        run: |
          curl -X POST https://argo-diff.example.com/webhook?sync=true \
            -H "Authorization: Bearer ${{ steps.oidc.outputs.token }}" \
            -H "Content-Type: application/json" \
            -d '{
              "github_token": "${{ secrets.GITHUB_TOKEN }}",
              "argocd_token": "${{ secrets.ARGOCD_TOKEN }}",
              "repository": "${{ github.repository }}",
              "pr_number": ${{ github.event.pull_request.number }},
              "base_ref": "${{ github.event.pull_request.base.sha }}",
              "head_ref": "${{ github.sha }}",
              "changed_files": ${{ steps.changed.outputs.files }},
              "workflow_name": "ArgoCD Diff"
            }'
```

## License

MIT
