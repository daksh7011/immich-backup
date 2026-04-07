# Homebrew Tap Design for immich-backup

**Date:** 2026-04-07

## Overview

Publish `immich-backup` as a Homebrew formula via GoReleaser, so users can install with:

```sh
brew tap daksh7011/tap
brew install immich-backup
```

## Release Flow

1. Merges to `master` → release-drafter accumulates changelog into a draft release (existing behaviour, unchanged)
2. When ready to ship → publish the draft on GitHub (specifying tag e.g. `v1.0.0`), which creates the tag
3. Tag push triggers `release.yml` → GoReleaser cross-compiles binaries, attaches them to the GitHub release, and pushes the updated Homebrew formula to `daksh7011/homebrew-tap`

## Components

### New repo: `daksh7011/homebrew-tap`

Created via `gh repo create`. GoReleaser writes `Formula/immich-backup.rb` on each release. No manual formula maintenance required.

### `.goreleaser.yaml` (immich-backup root)

- `CGO_ENABLED=0` — no CGo per project constraint
- Build targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`
- Archives: `.tar.gz` per platform
- Checksum file: `checksums.txt`
- Homebrew config: pushes formula to `daksh7011/homebrew-tap` using `HOMEBREW_TAP_TOKEN`

### `.github/workflows/release.yml` (new)

- Trigger: `push: tags: ['v*']`
- Steps: checkout → setup-go → goreleaser/goreleaser-action@v6
- Env: `GITHUB_TOKEN` (automatic) + `HOMEBREW_TAP_TOKEN` (repo secret, PAT with `repo` scope)

### `.github/workflows/master.yml` (unchanged)

Release-drafter continues to draft releases as PRs merge. GoReleaser handles the actual publish step.

## Manual Setup Required (one-time)

1. Create `HOMEBREW_TAP_TOKEN` secret in `daksh7011/immich-backup` repo settings — a GitHub PAT with `repo` scope so GoReleaser can push to `daksh7011/homebrew-tap`

## Files Changed

| Action | Path |
|--------|------|
| Create | `daksh7011/homebrew-tap` (new GitHub repo) |
| Create | `.goreleaser.yaml` |
| Create | `.github/workflows/release.yml` |
| No change | `.github/workflows/master.yml` |
