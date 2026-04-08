# Media Backup Progress Bar — Design Spec

**Date:** 2026-04-08
**Status:** Approved

## Summary

Enhance the `backup` command's TUI to show a live progress bar during media sync, including transfer speed, ETA, and file count. Uses rclone's JSON log output (`--use-json-log --stats 1s`) combined with a pre-scan (`rclone size --json`) to display accurate percentage from the start.

---

## Data Flow

```
backup.Run()
  │
  ├─ Phase 1: rclone size --json <srcDir>
  │    └─ sends ScanMsg{TotalFiles, TotalBytes} → channel → TUI shows pulse bar + "Scanning library..."
  │
  └─ Phase 2: rclone sync --use-json-log --stats 1s <srcDir> <remote>
       ├─ goroutine reads stderr line-by-line (bufio.Scanner)
       │    └─ parses JSON → sends MediaProgressMsg{...} → channel → TUI updates bar + stats
       └─ cmd.Wait() → sends ProgressMsg{"Media sync complete."} + DoneMsg{}
```

---

## New Message Types (`internal/backup/backup.go`)

Three new message types added alongside existing `ProgressMsg`, `ErrorMsg`, `DoneMsg`:

```go
// ScanMsg is sent after rclone size --json completes.
type ScanMsg struct {
    TotalFiles int64
    TotalBytes int64
}

// MediaProgressMsg is sent each stats tick during rclone sync.
type MediaProgressMsg struct {
    TransferredBytes int64
    TotalBytes       int64
    Speed            float64 // bytes/sec
    ETA              *int64  // nil = not yet known (rclone emits null on first tick)
    FilesDone        int64
    FilesTotal       int64
}
```

Existing message types are unchanged.

---

## JSON Parsing (`internal/backup/backup.go`)

### Pre-scan

```go
// rclone size --json output
type rcloneSizeResult struct {
    Count    int64 `json:"count"`
    Bytes    int64 `json:"bytes"`
    Sizeless int64 `json:"sizeless"`
}
```

Run: `rclone --config <conf> size <srcDir> --json`
Parse stdout, emit `ScanMsg`.

### Stats lines

```go
type rcloneLogLine struct {
    Stats *rcloneStats `json:"stats"`
}

type rcloneStats struct {
    Bytes          int64   `json:"bytes"`
    TotalBytes     int64   `json:"totalBytes"`
    Speed          float64 `json:"speed"`         // bytes/sec; 0 on first tick
    ETA            *int64  `json:"eta"`           // null on first tick
    Transfers      int64   `json:"transfers"`
    TotalTransfers int64   `json:"totalTransfers"`
}
```

Filter: skip lines where `stats` is nil (non-stats log lines like `"Copied (new)"`).
A private `parseRcloneStats(line []byte) (MediaProgressMsg, bool)` function handles unmarshalling and returns `false` for non-stats lines.

The stderr pipe from `rclone sync` is read line-by-line via `bufio.Scanner` in a goroutine. Parsed `MediaProgressMsg` values are sent to the channel. rclone exit errors are caught by `cmd.Wait()` and sent as `ErrorMsg`.

---

## TUI (`internal/tui/backup_model.go`)

### States

| State | Trigger | Display |
|-------|---------|---------|
| Scan | `ScanMsg` received | Pulse/indeterminate `bubbles/progress` bar + `"Scanning library..."` |
| Sync | First `MediaProgressMsg` | Determinate bar at `transferredBytes / totalBytes` + stats row |
| Done | `DoneMsg` | Final bar at 100% + `"✓ Backup complete!"` |

### Sync phase layout

```
  Backup Progress

  → Starting database backup...
  → Database dump uploaded. Starting media sync...

  [████████████░░░░░░░░░░░░] 52%
  52.4 MB/s  │  3m12s remaining  │  1,240 / 2,950 files

  ctrl+c abort
```

When ETA is nil: render `"calculating..."` instead of the time.

### Styling

- Progress bar filled color: `colorMauve` (`#CBA6F7`)
- Progress bar empty color: `colorSurface1` (`#45475A`)
- Stats row: `dimStyle` (`colorSubtext`)
- Separator `│`: `sepStyle` (`colorSurface1`)
- Speed, ETA, file count formatted in `backup_model.go` (not in `backup.go`)

### `BackupModel` fields added

```go
type BackupModel struct {
    // existing
    ch      <-chan any
    lines   []string
    done    bool
    lastErr error

    // new
    progress    progress.Model // bubbles progress bar
    scanning    bool
    totalBytes  int64
    totalFiles  int64
    mediaProg   *MediaProgressMsg // nil until first tick
}
```

---

## Dependencies

- `charm.land/bubbles/v2` — already an indirect dependency; promotes to direct
- No new external dependencies

---

## Out of Scope

- Progress bar for database dump (too fast, no useful granularity)
- Progress bar for database upload via `rclone copy` (single-file, minimal value)
- `--bwlimit` or throttling controls
- Per-file transfer list in TUI
