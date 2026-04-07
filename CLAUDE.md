# immich-backup

Go CLI tool for backing up Immich (self-hosted photo server) using rclone.

## Stack
- CLI: Cobra
- TUI: Bubble Tea + Lip Gloss + Huh
- Config: gopkg.in/yaml.v3
- Backup: rclone (shelled out as subprocess)

## Non-negotiable constraints
- No CGo. Ever.
- No GraalVM, no JVM, no CGo-dependent libraries.
- Fail fast and loud if prerequisites are missing.

## Prerequisites the tool enforces at runtime
- rclone in PATH
- Docker socket accessible by current user
- Immich postgres container running

## What gets backed up
- Media: rclone sync (true mirror, size+mtime comparison)
- Database: pg_dumpall via docker exec, gzipped

## Daemon
- macOS: launchd plist in ~/Library/LaunchAgents/
- Linux: systemd user service in ~/.config/systemd/user/
- No sudo required

## Commands
setup, configure, backup, status, doctor, logs, daemon (install/uninstall/start/stop/restart/status/logs)

## Commit style
Do not add `Co-Authored-By` trailers to commits. Never add Claude or any AI tool as a collaborator in git commits.