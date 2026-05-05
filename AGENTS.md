# TRACES Developer Guide

## Pre-Push Checks

Before pushing, run all validation checks:

```bash
task prepush
```

This runs:
1. `go fmt ./...` and `go mod tidy` (formatting)
2. `go vet ./...` (static analysis)
3. `go build -o traces-server .` (build check)
4. `go test -v ./...` (unit tests)

## Install Git Pre-Push Hook

To automatically run checks before every push:

```bash
task install-hooks
```

This installs a `.git/hooks/pre-push` script that runs `task prepush` and rejects the push if any check fails.

## Running Playwright E2E Tests

```bash
# Run all e2e tests (starts server automatically)
task test-e2e

# Run with more output
CI=true npx playwright test --workers=1 --reporter=line

# Run a specific test file
npx playwright test tests/admin.spec.ts
```

Note: E2E tests require `./traces-server` to be built first (`task build`).

## All Tasks

| Task | Description |
|------|-------------|
| `task build` | Build Go binary |
| `task test` | Run Go unit tests |
| `task test-e2e` | Run Playwright e2e tests |
| `task test-all` | Run all tests (unit + e2e) |
| `task lint` | Run `go vet` |
| `task tidy` | Format and tidy Go modules |
| `task prepush` | All checks before pushing |
| `task install-hooks` | Install git pre-push hook |
| `task dev` | Hot reload (requires `air`) |
| `task run` | Start server locally |
