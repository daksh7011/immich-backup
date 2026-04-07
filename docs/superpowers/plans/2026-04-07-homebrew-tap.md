# Homebrew Tap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish immich-backup binaries to GitHub Releases and a Homebrew tap via GoReleaser, triggered by tag pushes.

**Architecture:** Release-drafter continues drafting changelogs on master merges. Publishing a draft release creates a tag, which triggers a new `release.yml` workflow that runs GoReleaser — it cross-compiles for 4 targets, attaches archives to the release, and pushes a formula to `daksh7011/homebrew-tap`.

**Tech Stack:** GoReleaser v2, goreleaser/goreleaser-action@v6, actions/setup-go@v5, actions/checkout@v6

---

## File Map

| Action | Path |
|--------|------|
| Create | `daksh7011/homebrew-tap` GitHub repo (via gh CLI) |
| Create | `.goreleaser.yaml` |
| Create | `.github/workflows/release.yml` |

---

### Task 1: Create homebrew-tap GitHub repo

**Files:** New GitHub repo `daksh7011/homebrew-tap`

- [ ] **Step 1: Create the public tap repo**

```bash
gh repo create daksh7011/homebrew-tap \
  --public \
  --description "Homebrew tap for daksh7011's tools"
```

Expected output:
```
✓ Created repository daksh7011/homebrew-tap on GitHub
```

- [ ] **Step 2: Verify it exists**

```bash
gh repo view daksh7011/homebrew-tap --json name,url
```

Expected: JSON with `"name": "homebrew-tap"` and the repo URL.

---

### Task 2: Create .goreleaser.yaml

**Files:**
- Create: `.goreleaser.yaml`

- [ ] **Step 1: Create `.goreleaser.yaml` in the repo root**

```yaml
version: 2

project_name: immich-backup

before:
  hooks:
    - go mod tidy

builds:
  - env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w
    main: ./main.go

archives:
  - formats:
      - tar.gz
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: checksums.txt

brews:
  - name: immich-backup
    repository:
      owner: daksh7011
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    homepage: "https://github.com/daksh7011/immich-backup"
    description: "Go CLI tool for backing up Immich (self-hosted photo server) using rclone."
    license: MIT
    install: |
      bin.install "immich-backup"
    test: |
      system "#{bin}/immich-backup", "--help"
```

- [ ] **Step 2: Validate the config locally (requires goreleaser installed)**

```bash
goreleaser check
```

Expected: `• config is valid` — or skip if goreleaser is not installed locally; CI will catch errors.

- [ ] **Step 3: Commit**

```bash
git add .goreleaser.yaml
git commit -m "ci: add goreleaser config for cross-platform builds and homebrew tap"
```

---

### Task 3: Create release workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create `.github/workflows/release.yml`**

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    permissions:
      contents: write
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Install rclone
        run: curl https://rclone.org/install.sh | sudo bash

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v6
        with:
          distribution: goreleaser
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
```

Note: `fetch-depth: 0` is required — GoReleaser needs full git history to generate the changelog.

- [ ] **Step 2: Commit and push**

```bash
git add .github/workflows/release.yml
git commit -m "ci: add tag-triggered release workflow with goreleaser"
git push git@github.com:daksh7011/immich-backup.git feat/go-skeleton
```

---

### Task 4: Add HOMEBREW_TAP_TOKEN secret (manual step)

This step cannot be automated — it requires a GitHub PAT.

- [ ] **Step 1: Create a GitHub Personal Access Token**

Go to: GitHub → Settings → Developer settings → Personal access tokens → Fine-grained tokens → Generate new token

Settings:
- Name: `HOMEBREW_TAP_TOKEN`
- Repository access: Only select repositories → `daksh7011/homebrew-tap`
- Permissions → Repository permissions → Contents: **Read and write**

- [ ] **Step 2: Add the token as a secret in immich-backup**

```bash
gh secret set HOMEBREW_TAP_TOKEN --repo daksh7011/immich-backup
```

Paste the PAT when prompted.

- [ ] **Step 3: Verify the secret exists**

```bash
gh secret list --repo daksh7011/immich-backup
```

Expected: `HOMEBREW_TAP_TOKEN` appears in the list.
