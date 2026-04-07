# GitHub CI/CD Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GitHub Actions workflows, labeler, release-drafter, PR template, and helper scripts to immich-backup, mirroring the structure of daksh7011/ca-automation adapted for Go and master/develop branches.

**Architecture:** Three workflows (ci.yml for PRs, develop.yml for develop branch, master.yml for master branch) plus supporting config files and two bash scripts. No JIRA validation.

**Tech Stack:** GitHub Actions, Go 1.26, actions/setup-go@v5, actions/labeler@v6.0.1, release-drafter/release-drafter@v7

---

## File Map

| Action | Path |
|--------|------|
| Create | `.github/workflows/ci.yml` |
| Create | `.github/workflows/develop.yml` |
| Create | `.github/workflows/master.yml` |
| Create | `.github/scripts/check-pr-labels.sh` |
| Create | `.github/scripts/check-release-labels.sh` |
| Create | `.github/labeler.yml` |
| Create | `.github/release-drafter.yml` |
| Create | `.github/pull_request_template.md` |

---

### Task 1: Bash helper scripts

**Files:**
- Create: `.github/scripts/check-pr-labels.sh`
- Create: `.github/scripts/check-release-labels.sh`

- [ ] Create `.github/scripts/check-pr-labels.sh`:

```bash
#!/bin/bash
set -e

LABELS="$1"
REQUIRED_LABELS="dependency feature fix maintenance release"

for label in $REQUIRED_LABELS; do
  if echo "$LABELS" | grep -qw "$label"; then
    exit 0
  fi
done

echo "Error: PR must have one of the following labels: $REQUIRED_LABELS"
exit 1
```

- [ ] Create `.github/scripts/check-release-labels.sh`:

```bash
#!/bin/bash
set -e

LABELS="$1"
VERSION_LABELS="major minor patch"

for label in $VERSION_LABELS; do
  if echo "$LABELS" | grep -qw "$label"; then
    exit 0
  fi
done

echo "Error: PR targeting master must have one of the following labels: $VERSION_LABELS"
exit 1
```

- [ ] Make scripts executable: `chmod +x .github/scripts/*.sh`

- [ ] Commit:
```bash
git add .github/scripts/
git commit -m "ci: add PR label validation scripts"
```

---

### Task 2: Labeler config

**Files:**
- Create: `.github/labeler.yml`

- [ ] Create `.github/labeler.yml`:

```yaml
dependency:
  - head-branch: ['^renovate']
  - changed-files:
      - any-glob-to-any-file: '**/go.mod'
      - any-glob-to-any-file: '**/go.sum'

ci:
  - changed-files:
      - any-glob-to-any-file: '.github/**/*'

feature:
  - head-branch: ['^feature', 'feature']

fix:
  - head-branch: ['^fix', 'fix']

maintenance:
  - head-branch: ['^maintenance', 'maintenance', '^chore', 'chore']

release:
  - base-branch: 'master'
```

- [ ] Commit:
```bash
git add .github/labeler.yml
git commit -m "ci: add PR auto-labeler config"
```

---

### Task 3: Release drafter config

**Files:**
- Create: `.github/release-drafter.yml`

- [ ] Create `.github/release-drafter.yml`:

```yaml
name-template: 'v$RESOLVED_VERSION'
tag-template: 'v$RESOLVED_VERSION'
categories:
  - title: '🚀 Features'
    labels:
      - 'feature'
  - title: '🐛 Bug Fixes'
    labels:
      - 'bug'
      - 'fix'
  - title: '⬆️ Dependencies'
    labels:
      - 'dependencies'
      - 'dependency'
  - title: '🧰 Maintenance'
    labels:
      - 'maintenance'
      - 'ci'
change-template: '- $TITLE @$AUTHOR (#$NUMBER)'
change-title-escapes: '\<*_&@'
version-resolver:
  major:
    labels:
      - 'major'
  minor:
    labels:
      - 'minor'
  patch:
    labels:
      - 'patch'
  default: patch
template: |
  ## Changes

  $CHANGES
```

- [ ] Commit:
```bash
git add .github/release-drafter.yml
git commit -m "ci: add release-drafter config"
```

---

### Task 4: PR template

**Files:**
- Create: `.github/pull_request_template.md`

- [ ] Create `.github/pull_request_template.md`:

```markdown
# Description

Please include a summary of the changes and the related issue. Please also include relevant motivation and context. List any dependencies that are required for this change.

Fixes # (issue)

## Type of change

Please delete options that are not relevant.

- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] This change requires a documentation update

# How Has This Been Tested?

Please describe the tests that you ran to verify your changes. Provide instructions so we can reproduce. Please also list any relevant details for your test configuration.

# Checklist:

- [ ] My code follows the style guidelines of this project
- [ ] I have performed a self-review of my code
- [ ] I have commented my code, particularly in hard-to-understand areas
- [ ] I have made corresponding changes to the documentation
- [ ] My changes generate no new warnings
```

- [ ] Commit:
```bash
git add .github/pull_request_template.md
git commit -m "ci: add PR template"
```

---

### Task 5: PR CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] Create `.github/workflows/ci.yml`:

```yaml
name: Build (CI)

on:
  pull_request:
    types: [opened, synchronize, reopened, edited, labeled]

concurrency:
  group: ${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}
  cancel-in-progress: true

jobs:
  build-ci:
    permissions:
      contents: read
      pull-requests: write
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.GITHUB_TOKEN }}

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Labeler
        uses: actions/labeler@v5

      - name: Build
        run: go build ./...

      - name: Test
        run: go test -v -race ./...

  label-check:
    needs: build-ci
    permissions:
      contents: read
      pull-requests: read
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Check for required PR label
        run: bash .github/scripts/check-pr-labels.sh "${{ join(github.event.pull_request.labels.*.name, ' ') }}"

  release-label-check:
    needs: build-ci
    permissions:
      contents: read
      pull-requests: read
    runs-on: ubuntu-latest
    if: github.base_ref == 'master'
    steps:
      - uses: actions/checkout@v4
      - name: Check for release version label
        run: bash .github/scripts/check-release-labels.sh "${{ join(github.event.pull_request.labels.*.name, ' ') }}"
```

- [ ] Commit:
```bash
git add .github/workflows/ci.yml
git commit -m "ci: add PR CI workflow"
```

---

### Task 6: Develop branch workflow

**Files:**
- Create: `.github/workflows/develop.yml`

- [ ] Create `.github/workflows/develop.yml`:

```yaml
name: Build Develop

on:
  push:
    branches:
      - develop

jobs:
  build-develop:
    permissions:
      contents: read
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.GITHUB_TOKEN }}

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Build
        run: go build ./...

      - name: Test
        run: go test -v -race ./...
```

- [ ] Commit:
```bash
git add .github/workflows/develop.yml
git commit -m "ci: add develop branch build workflow"
```

---

### Task 7: Master branch release workflow

**Files:**
- Create: `.github/workflows/master.yml`

- [ ] Create `.github/workflows/master.yml`:

```yaml
name: Build Master & Publish Release

on:
  push:
    branches:
      - master

jobs:
  build-master:
    permissions:
      contents: write
      pull-requests: write
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.GITHUB_TOKEN }}

      - name: Release Drafter
        id: release_drafter
        uses: release-drafter/release-drafter@v6
        with:
          token: ${{ secrets.GITHUB_TOKEN }}

      - name: Set version to env
        run: echo "VERSION=${{ steps.release_drafter.outputs.tag_name }}" >> $GITHUB_ENV
```

- [ ] Commit:
```bash
git add .github/workflows/master.yml
git commit -m "ci: add master branch release workflow"
```
