# TRACES Developer Guide

## Conventional Commits

All commits **MUST** follow the [Conventional Commits](https://www.conventionalcommits.org/) format.
The release workflow uses `semantic-release` to automatically version and publish based on commit messages.

| Prefix | Release | Example |
|--------|---------|---------|
| `feat:` | minor | `feat: add dark mode toggle` |
| `fix:` | patch | `fix: correct date formatting for leap years` |
| `refactor:` | patch | `refactor: extract media icon helper` |
| `style:` | patch | `style: format Go imports` |
| `chore(deps):` | patch | `chore(deps): bump gin to v1.12` |
| `docs:` | none | `docs: update API examples` |
| `test:` | none | `test: add upload hash coverage` |
| `chore:` | none | `chore: clean up temp files` |

A commit message should look like:

```
feat: add year comparison chart

Implement side-by-side stat comparison for any two years,
with trend indicators and media breakdown.

Closes #42
```

## Pre-Push Checks

Before pushing, run all validation checks:

```bash
task prepush
```

This runs:
1. `go fmt ./...` and `go mod tidy` (formatting)
2. `go vet ./...` (static analysis)
3. `npx tsc` (TypeScript compilation)
4. `go build -o traces-server .` (build check)
5. `go test -v ./...` (unit tests)

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

Note: E2E tests require compiled TypeScript and the Go binary (`task build-ts` + `task build`).

## All Tasks

| Task | Description |
|------|-------------|
| `task build` | Build Go binary |
| `task build-ts` | Compile TypeScript source |
| `task typecheck` | TypeScript type checking (no emit) |
| `task test` | Run Go unit tests |
| `task test-e2e` | Run Playwright e2e tests |
| `task test-all` | Run all tests (unit + e2e) |
| `task lint` | Run `go vet` |
| `task tidy` | Format and tidy Go modules |
| `task prepush` | All checks before pushing |
| `task install-hooks` | Install git pre-push hook |
| `task dev` | Hot reload (requires `air`) |
| `task run` | Start server locally |
