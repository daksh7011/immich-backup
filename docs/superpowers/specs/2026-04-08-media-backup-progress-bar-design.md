# Media Backup Progress Bar — Design Spec

**Date:** 2026-04-08
**Status:** Approved

## Summary

Enhance the `backup` command's TUI to show a live progress bar during media sync, including transfer speed, ETA, and file count. Uses rclone's JSON log output (`--use-json-log --stats 1s`) combined with a pre-scan (`rclone size --json`) to display accurate percentage from the start. File-level I/O errors (e.g. unreadable source files) are captured, displayed in the TUI, and skipped via `--ignore-errors` so the rest of the backup continues.

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

Four new message types added alongside existing `ProgressMsg`, `ErrorMsg`, `DoneMsg`:

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

// RcloneErrorMsg is sent when rclone reports a file-level error (level:"error" in JSON log).
// These are non-fatal: the backup continues via --ignore-errors.
type RcloneErrorMsg struct {
    Text string // full error message from rclone
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

The full log line struct also carries `Level` for error detection:

```go
type rcloneLogLine struct {
    Level string       `json:"level"`
    Msg   string       `json:"msg"`
    Stats *rcloneStats `json:"stats"`
}
```

Parsing rules (applied per line):
1. If `stats != nil` → emit `MediaProgressMsg`
2. Else if `level == "error"` → emit `RcloneErrorMsg{Text: msg}`
3. Otherwise → skip

A private `parseRcloneLine(line []byte) (any, bool)` function handles unmarshalling and returns the appropriate message type or `(nil, false)` to skip.

The stderr pipe from `rclone sync` is read line-by-line via `bufio.Scanner` in a goroutine. `--ignore-errors` is added to the rclone sync flags so file-level errors don't abort the run. rclone will exit non-zero if any files failed; `cmd.Wait()` is treated as a warning (not a fatal `ErrorMsg`) when `rcloneErrors` were already collected — a clean exit (no errors collected) sends `DoneMsg` normally.

---

## TUI (`internal/tui/backup_model.go`)

### States

| State | Trigger | Display |
|-------|---------|---------|
| Scan | `ScanMsg` received | Pulse/indeterminate `bubbles/progress` bar + `"Scanning library..."` |
| Sync | First `MediaProgressMsg` | Determinate bar at `transferredBytes / totalBytes` + stats row |
| Done | `DoneMsg` | Final bar at 100% + completion message (see below) |

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

### Error display and completion messages

File-level errors from `RcloneErrorMsg` accumulate in `rcloneErrors []string` and render below the progress bar in `errStyle` (red) as they arrive:

```
  [████████████████████████] 100%
  52.4 MB/s  │  done  │  2,950 / 2,950 files

  ✗ 2025/10-Dec-2025/20251210_163454.mp4: Failed to copy: input/output error
  ✗ 2025/11-Nov-2025/20251105_091200.jpg: Failed to copy: input/output error

  ✓ Backup complete with 2 file error(s).
```

Clean run (no errors): `✓ Backup complete!` in `okStyle`.
Partial run (errors collected): `✓ Backup complete with N file error(s).` in `warnStyle`.

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
    progress      progress.Model    // bubbles progress bar
    scanning      bool
    totalBytes    int64
    totalFiles    int64
    mediaProg     *MediaProgressMsg // nil until first tick
    rcloneErrors  []string          // accumulated file-level errors
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
