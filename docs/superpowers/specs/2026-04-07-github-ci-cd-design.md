# GitHub CI/CD Design for immich-backup

**Date:** 2026-04-07
**Branch model:** `feat/*` → `develop` → `master`

## Overview

Three GitHub Actions workflows mirroring the structure of `daksh7011/ca-automation`, adapted for Go and the `develop`/`master` branch model. No JIRA ticket validation.

---

## Workflows

### `ci.yml` — Pull Request CI

**Trigger:** `pull_request` on types `[opened, synchronize, reopened, edited, labeled]`

**Concurrency:** cancel-in-progress per PR number.

**Jobs:**

1. **`build-ci`** (runs first)
   - `actions/checkout@v6`
   - `actions/setup-go@v5` (version from `go.mod`)
   - `actions/labeler@v6.0.1` (auto-labels based on `.github/labeler.yml`)
   - `go build ./...`
   - `go test -v -race ./...`

2. **`label-check`** (needs: `build-ci`)
   - Runs `.github/scripts/check-pr-labels.sh` — requires one of: `dependency`, `feature`, `fix`, `maintenance`, `release`

3. **`release-label-check`** (needs: `build-ci`, only if `base_ref == 'master'`)
   - Runs `.github/scripts/check-release-labels.sh` — requires one of: `major`, `minor`, `patch`

---

### `develop.yml` — Develop Branch Build

**Trigger:** `push` to `develop`

**Jobs:**

1. **`build-develop`**
   - `actions/checkout@v6`
   - `actions/setup-go@v5`
   - `go build ./...`
   - `go test -v -race ./...`

---

### `master.yml` — Master Branch Release

**Trigger:** `push` to `master`

**Jobs:**

1. **`build-master`** (permissions: `contents: write`, `pull-requests: write`)
   - `actions/checkout@v6`
   - `release-drafter/release-drafter@v7` — drafts/updates release
   - Sets `VERSION` env from drafter's `tag_name` output

---

## Supporting Files

### `.github/labeler.yml`

Auto-labels PRs by branch name and changed files:

| Label | Trigger |
|-------|---------|
| `dependency` | Branch matches `^renovate`, or changes to `go.mod` / `go.sum` |
| `ci` | Changes to `.github/**/*` |
| `feature` | Branch matches `^feature` or contains `feature` |
| `fix` | Branch matches `^fix` or contains `fix` |
| `maintenance` | Branch matches `^maintenance`, `maintenance`, `^chore`, or `chore` |
| `release` | PR base branch is `master` |

### `.github/release-drafter.yml`

- Tag template: `v$RESOLVED_VERSION`
- Categories: Features (`feature`), Bug Fixes (`bug`/`fix`), Dependencies (`dependencies`), Maintenance (`maintenance`/`CI`)
- Version resolver: `major`/`minor`/`patch` labels; default `patch`

### `.github/pull_request_template.md`

Standard checklist: description, type of change, testing notes, code/docs/style checklist.

### `.github/scripts/check-pr-labels.sh`

Validates PR has one of: `dependency`, `feature`, `fix`, `maintenance`, `release`. Exits 1 with error message if none found.

### `.github/scripts/check-release-labels.sh`

Validates PR has one of: `major`, `minor`, `patch`. Only runs for PRs targeting `master`.

---

## What is NOT included

- JIRA ticket validation (`validate-pr-title.sh`) — not needed
- Postgres service container — tests use testcontainers (Docker available on GitHub runners)
- Python/uv toolchain — replaced by Go toolchain

---

## Files to Create

```
.github/
  workflows/
    ci.yml
    develop.yml
    master.yml
  scripts/
    check-pr-labels.sh
    check-release-labels.sh
  labeler.yml
  release-drafter.yml
  pull_request_template.md
```
