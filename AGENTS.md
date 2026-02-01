# Agent Instructions for argo-diff

This document outlines development guidelines for AI agents and contributors working on this project.

## Commit Guidelines

Use **semantic commits** following the [Conventional Commits](https://www.conventionalcommits.org/) specification:

### Commit Types

| Type       | Description                                          |
|------------|------------------------------------------------------|
| `feat`     | A new feature                                        |
| `fix`      | A bug fix                                            |
| `docs`     | Documentation only changes                           |
| `style`    | Code style changes (formatting, no code change)      |
| `refactor` | Code change that neither fixes a bug nor adds feature|
| `perf`     | Performance improvement                              |
| `test`     | Adding or updating tests                             |
| `build`    | Changes to build system or dependencies              |
| `ci`       | CI configuration changes                             |
| `chore`    | Other changes that don't modify src or test files    |

### Commit Format

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

**Examples:**
```
TBD
```

### Scope (Optional)

Use package names as scopes: (TBD)

## Code Quality Checks

**Always run these commands before committing:**

### 1. Format Code
```bash
go fmt ./...
```

### 2. Run Vet
```bash
go vet ./...
```

### 3. Run Linter
```bash
golangci-lint run ./...
```

If golangci-lint is not installed:
```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### 4. Run Tests
```bash
go test ./...
```

### 5. Verify GoReleaser Config (when modified)
```bash
goreleaser check
```

Run this whenever `.goreleaser.yml` or `.goreleaser.yaml` is modified.

## Pre-Commit Checklist

- [ ] Code formatted with `go fmt ./...`
- [ ] No issues from `go vet ./...`
- [ ] No issues from `golangci-lint run ./...`
- [ ] All tests pass with `go test ./...`
- [ ] GoReleaser config validated with `goreleaser check` (if modified)
- [ ] Commit message follows semantic commit format
- [ ] Changes are logically grouped into small, focused commits

## Development Workflow

1. Make changes in small, logical increments
2. Run all quality checks before each commit
3. Write descriptive commit messages explaining *what* and *why*
4. Keep commits atomic - one logical change per commit
5. Add tests for new functionality

## GitHub Actions Workflow Guidelines

**Always pin actions to their latest available minor version** (e.g., `actions/checkout@v4`) rather than using major version tags or SHA references. This ensures:
- Security updates are automatically received
- Breaking changes are avoided (minor versions are backward compatible)
- Workflows remain maintainable and up-to-date

**Example:**
```yaml
- uses: actions/checkout@v4  # ✓ Correct
- uses: actions/checkout@v3  # ✗ Outdated
- uses: actions/checkout@main  # ✗ Unpinned
```
