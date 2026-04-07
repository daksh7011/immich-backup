<!--
SYNC IMPACT REPORT
==================
Version change: 1.1.0 → 2.0.0 (MAJOR — NON-NEGOTIABLE Principle V redefined)

Modified principles:
  - V. "Rclone Configuration Is Read-Only" →
       "Isolated Local Rclone Configuration"
    Old: tool MUST NEVER create/modify/delete rclone remotes or config file.
    New: tool owns ~/.immich-backup/rclone.conf; delegates config UX to
         `rclone config`; NEVER touches the user's global rclone config.

Added sections: none

Removed sections: none

Template propagation status:
  - .specify/templates/plan-template.md   ✅ No structural changes needed;
                                             "Constitution Check" must reference
                                             updated Principle V wording.
  - .specify/templates/spec-template.md   ✅ No changes required.
  - .specify/templates/tasks-template.md  ✅ No changes required.
  - docs/superpowers/specs/
      2026-04-06-immich-backup-skeleton-design.md
                                          ⚠ PENDING — references constitution
                                             v1.1.0 and states "Rclone
                                             configuration is never created or
                                             modified by this tool." Needs update
                                             to reflect local rclone.conf +
                                             pause-launch-resume pattern. Config
                                             schema should add rclone config path.

Follow-up TODOs:
  - Update skeleton design spec to v2.0.0 compliance:
      (1) Add ~/.immich-backup/rclone.conf to directory structure and Hard Constraints.
      (2) Add rclone_config_path field to Config struct (or document hardcoded path).
      (3) Update doctor check to verify local rclone.conf has ≥1 remote configured.
      (4) Document pause-launch-resume flow in Section 2 (data flow).
      (5) Update constitution compliance bullet to reference v2.0.0.
-->

# immich-backup Constitution

## Core Principles

### I. Pure Go (NON-NEGOTIABLE)

The project MUST be written in pure Go. CGo is strictly forbidden — zero exceptions.
The binary MUST compile with `CGO_ENABLED=0` and ship as a self-contained, statically-linked
executable suitable for Homebrew (macOS/Linux) and Chocolatey (Windows) distribution.

**Rationale**: CGo breaks cross-compilation, complicates static linking, and introduces
hidden C-runtime dependencies that make distribution brittle. A single binary with no
dynamic dependencies is the only acceptable artifact.

### II. Rclone Remote Abstraction (NON-NEGOTIABLE)

The tool MUST interact with rclone only through remote names configured in the tool's
local rclone config (see Principle V). It MUST NEVER pass provider-specific flags
(e.g., `--s3-access-key-id`, `--drive-client-id`) to rclone invocations. The rclone
remote name is the sole coupling point between this tool and any storage backend.

**Rationale**: Provider-specific flags create tight coupling to storage backends and
duplicate configuration the user already manages in rclone. Any change to provider
credentials or parameters must remain entirely outside this tool's invocation logic.

### III. Fail-Fast & Clear Observability (NON-NEGOTIABLE)

The tool MUST fail immediately — before starting any backup — if any required dependency
is unreachable:

- rclone binary not found or not executable
- Docker socket unreachable or permission denied
- Immich Postgres container not running or not responding
- Local rclone config (`~/.immich-backup/rclone.conf`) has no remotes configured

Every failure MUST emit a structured, human-readable log line that names the dependency,
describes the error, and suggests a remediation step. Silent failures and partial backups
are not acceptable.

**Rationale**: A backup that silently completes with missing data is worse than no backup
at all. Operators must be able to diagnose failures without inspecting internal state.

### IV. Safe Postgres Backup (NON-NEGOTIABLE)

The tool MUST NEVER copy, rsync, or snapshot the raw Postgres data directory.
Database dumps MUST be produced exclusively via `pg_dumpall` executed inside the
Immich Postgres container using `docker exec`. The resulting dump file is then handed
to rclone for upload.

**Rationale**: Copying a live Postgres data directory produces a corrupt or
inconsistent backup. `pg_dumpall` guarantees a consistent logical dump without
requiring the database to be stopped.

### V. Isolated Local Rclone Configuration (NON-NEGOTIABLE)

The tool MUST maintain its own isolated rclone config at `~/.immich-backup/rclone.conf`.
ALL rclone invocations MUST pass `--config ~/.immich-backup/rclone.conf` — the tool
MUST NEVER read from or write to the user's global rclone config (typically
`~/.config/rclone/rclone.conf`).

**Missing or empty config — pause-launch-resume**:

If `~/.immich-backup/rclone.conf` does not exist, or exists but contains no configured
remotes, the tool MUST:

1. Inform the user that no rclone remote is configured.
2. Pause the current CLI flow.
3. Launch `rclone config --config ~/.immich-backup/rclone.conf` as an interactive
   subprocess, inheriting the terminal.
4. Wait for `rclone config` to exit.
5. Verify that at least one remote now exists in the local config. If not, the tool
   MUST exit with a clear error (Principle III).
6. Resume the original flow.

**User-triggered reconfiguration**:

When the user requests rclone settings changes (e.g., via `immich-backup configure`),
the same pause-launch-resume pattern MUST be applied. The tool hands off to
`rclone config --config ~/.immich-backup/rclone.conf`, waits for it to complete,
then resumes.

**Rationale**: An isolated config prevents corruption of the user's existing rclone
remotes for unrelated workflows. Delegating configuration UX entirely to `rclone config`
leverages rclone's battle-tested interactive configurator without reimplementing
credential management logic. The pause-launch-resume pattern gives users a guided,
in-flow path to add a remote without abandoning the immich-backup session.

### VI. Rootless Daemon Management

The tool MUST support installation as a scheduled background service without requiring
elevated (root/Administrator) privileges:

- **macOS**: generate and manage a `launchd` user agent plist (`~/Library/LaunchAgents/`)
- **Linux**: generate and manage a `systemd` user service unit (`~/.config/systemd/user/`)

Root-level service installation (e.g., `/etc/systemd/system/`) is out of scope.

**Rationale**: Requiring root for a personal media backup tool creates an unnecessary
security surface and prevents installation in shared/restricted environments.

## Hard Constraints

- **Config file**: `~/.immich-backup/config.yaml`, parsed exclusively with
  `gopkg.in/yaml.v3` plain structs. No Viper, Cobra config binding, or embedded config
  languages.
- **Rclone config file**: `~/.immich-backup/rclone.conf` — an isolated, tool-owned
  rclone configuration. ALL rclone invocations MUST pass
  `--config ~/.immich-backup/rclone.conf`. The user's global rclone config MUST NOT be
  read or modified.
- **Distribution targets**: macOS (Homebrew), Windows (Chocolatey), Linux (Homebrew
  or manual binary install). All platforms MUST be served by the same binary build matrix
  (`GOOS`/`GOARCH` cross-compilation).
- **Dependency on Docker**: Postgres access MUST go through `docker exec` against the
  running Immich stack container. Direct TCP access to Postgres is not supported.
- **Immich-specific, storage-agnostic**: Feature scope is limited to the Immich media
  server. Support for other self-hosted services MUST NOT be added without a constitution
  amendment.

## Development Workflow

- All tests MUST run without CGo (`CGO_ENABLED=0 go test ./...`).
- All tests run against real infrastructure (testcontainers-go + real rclone binary).
  No mocks, no fakes, no build tags. Docker and rclone are hard runtime prerequisites
  for this project, so tests require them too. A plain `go test ./...` runs everything.
- PRs touching backup or restore logic MUST include a Constitution Check confirming
  compliance with Principles III, IV, and V before merge.
- PRs touching setup, configure, or rclone invocation MUST verify the
  pause-launch-resume flow (Principle V) and that `--config ~/.immich-backup/rclone.conf`
  is passed to every rclone call.
- Complexity violations (e.g., adding a fourth external dependency type) MUST be
  documented in the plan's Complexity Tracking table with justification.

## Governance

This constitution supersedes all other development practices and style guides within
the `immich-backup` repository. Any amendment requires:

1. A written rationale referencing which principle is affected and why the change
   is necessary.
2. A version bump following semantic versioning rules (see below).
3. Propagation review across `.specify/templates/` and any agent guidance files.

**Versioning policy**:

- **MAJOR** — removal or redefinition of a NON-NEGOTIABLE principle.
- **MINOR** — new principle added, or existing principle materially expanded.
- **PATCH** — clarifications, wording fixes, non-semantic refinements.

All PRs and code reviews MUST verify compliance with the NON-NEGOTIABLE principles
(I–V). Principle VI compliance is verified only for daemon-management changes.

**Version**: 2.0.0 | **Ratified**: 2026-04-06 | **Last Amended**: 2026-04-06
