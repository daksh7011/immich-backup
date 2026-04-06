<!--
SYNC IMPACT REPORT
==================
Version change: [PLACEHOLDER] → 1.0.0 (initial ratification — all content new)

Modified principles: N/A (first fill)

Added sections:
  - Core Principles (I–VI, all new)
  - Hard Constraints
  - Development Workflow
  - Governance

Removed sections: N/A

Template propagation status:
  - .specify/templates/plan-template.md   ✅ No structural changes needed; "Constitution Check"
                                             section gates must reference the six principles below.
  - .specify/templates/spec-template.md   ✅ No changes required; generic enough to remain as-is.
  - .specify/templates/tasks-template.md  ✅ No changes required; phase structure is generic.

Follow-up TODOs:
  - None; all fields resolved from user-supplied hard rules and today's date.
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

The tool MUST interact with rclone only through remote names that the user has already
configured in their own rclone installation. It MUST NEVER pass provider-specific flags
(e.g., `--s3-access-key-id`, `--drive-client-id`) to rclone invocations. The rclone
remote name is the sole coupling point between this tool and any storage backend.

**Rationale**: Provider-specific flags create tight coupling to storage backends and
duplicate configuration the user already manages in rclone. Any change to provider
credentials or parameters must remain entirely outside this tool's scope.

### III. Fail-Fast & Clear Observability (NON-NEGOTIABLE)

The tool MUST fail immediately — before starting any backup — if any required dependency
is unreachable:

- rclone binary not found or not executable
- Docker socket unreachable or permission denied
- Immich Postgres container not running or not responding

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

### V. Rclone Configuration Is Read-Only (NON-NEGOTIABLE)

The tool MUST NEVER create, modify, delete, or otherwise manage rclone remotes or
its configuration file. It MUST operate only against remotes that already exist in
the user's rclone configuration. If a referenced remote does not exist, the tool
MUST fail with a clear error (see Principle III).

**Rationale**: Modifying a shared tool's configuration on the user's behalf risks
corrupting credentials for unrelated workflows. Configuration ownership belongs
entirely to the user.

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

**Version**: 1.1.0 | **Ratified**: 2026-04-06 | **Last Amended**: 2026-04-06
