# Spinner & Progress Feedback Design

**Date:** 2026-04-08  
**Branch:** feature/spinner-progress-feedback  
**Scope:** backup, doctor, daemon subcommands

---

## Problem

Several commands have blocking waits with no visual feedback:
- `backup`: pg_dumpall runs silently; rclone size runs silently; rclone sync internal file-check phase is invisible
- `doctor`: all 5 checks run synchronously before the TUI starts
- `daemon` subcommands: the systemd/launchd operation runs before the TUI starts

Goal: every wait shows exactly what is happening — spinner for indefinite operations, progress bar with numbers where data is available.

---

## Architecture

### 1. Shared step primitives — `internal/tui/steps.go` (new file)

```go
type stepState int
const (
    stepPending stepState = iota
    stepRunning
    stepDone
    stepError
)

type step struct {
    label  string
    state  stepState
    detail string // sub-line: speed, ETA, error text, "up to date", etc.
}
```

`renderSteps(steps []step, sp spinner.Model) string` renders each step:
- `⠿ label…`   — spinner, running
- `✓ label`    — green bold, done
- `✗ label — detail` — red bold, error
- `· label`    — dim, pending

Used by all three TUI models so the visual language is uniform.

---

### 2. Backup command

#### New message types in `internal/backup/backup.go`

```go
type PhaseMsg struct{ Phase BackupPhase }

type BackupPhase int
const (
    PhaseDBDump    BackupPhase = iota
    PhaseDBUpload
    PhaseMediaScan
    PhaseMediaCheck
    PhaseMediaSync
)
```

`backup.Run()` sends `PhaseMsg` at each phase transition (replaces the existing `ProgressMsg` text lines for phase labeling).

#### Phase → indicator mapping

| Phase | Step label | Trigger ON | Trigger OFF | Indicator |
|---|---|---|---|---|
| `PhaseDBDump` | "Dumping database" | `PhaseMsg{PhaseDBDump}` | `PhaseMsg{PhaseDBUpload}` | indefinite spinner |
| `PhaseDBUpload` | "Uploading database dump" | `PhaseMsg{PhaseDBUpload}` | `PhaseMsg{PhaseMediaScan}` | progress bar % + speed + ETA |
| `PhaseMediaScan` | "Scanning library" | `PhaseMsg{PhaseMediaScan}` | `ScanMsg` received | indefinite spinner |
| `PhaseMediaCheck` | "Checking for changes" | `ScanMsg` | first `MediaProgressMsg` with `FilesTotal > 0` | indefinite spinner |
| `PhaseMediaSync` | "Syncing media" | `FilesTotal > 0` | `DoneMsg` | progress bar % + speed + ETA + file count |

**Edge case:** if `FilesTotal` remains 0 until `DoneMsg` (nothing to sync), "Checking for changes" completes with detail `"up to date"`.

#### `BackupModel` internal state (replaces `lines []string`)

```go
type BackupModel struct {
    ch     <-chan any
    cancel context.CancelFunc
    done   bool
    lastErr error

    // named phase steps (explicit fields, not a generic slice)
    dbDumpStep      step
    dbUploadStep    step
    mediaScanStep   step  // rclone size
    mediaCheckStep  step  // rclone sync traversal (FilesTotal == 0)
    mediaSyncStep   step  // rclone sync transferring (FilesTotal > 0)

    // progress data
    dbProgress    progress.Model
    mediaProgress progress.Model
    dbUploadProg  *backup.DBUploadProgressMsg
    mediaProg     *backup.MediaProgressMsg
    rcloneErrors  []string

    spinner spinner.Model
}
```

The `dbProgress` and `mediaProgress` bars replace the previous `progress`/`dbProgress` fields (renamed for clarity).

---

### 3. Doctor command

#### New streaming function in `internal/doctor/doctor.go`

```go
// CheckAsync runs all checks sequentially in the calling goroutine and sends
// CheckStartMsg before each check and CheckResult after.
// Close ch is the caller's responsibility after the goroutine returns.
func CheckAsync(ex docker.Executor, cfg *config.Config, rcloneConfPath string, ch chan<- any)

type CheckStartMsg struct{ Name string }
```

`CheckResult` is already defined; it is reused as the "done" message.

#### `cmd/doctor.go` flow

```go
ch := make(chan any, 10)
go func() {
    doctor.CheckAsync(ex, cfg, config.RcloneConfigPath(), ch)
    close(ch)
}()
model := tui.NewDoctorModel(ch)
tea.NewProgram(model).Run()
```

#### `DoctorModel` state

- Holds `[]step` (one per check, all start `stepPending`)
- `currentIdx int` — which check is running
- On `CheckStartMsg`: set `steps[currentIdx].state = stepRunning`, start spinner tick
- On `CheckResult`: set `steps[currentIdx]` to `stepDone` or `stepError`, increment `currentIdx`
- On channel close (all results received): mark done, show summary

---

### 4. Daemon subcommands

#### New message type in `internal/tui/daemon_model.go`

```go
type DaemonResultMsg struct {
    Msg string
    Err error
}
```

#### `cmd/daemon.go` flow (each subcommand)

```go
ch := make(chan any, 1)
go func() {
    err := fn(c)
    msg := ""
    if err == nil { msg = "Done." }
    ch <- tui.DaemonResultMsg{Msg: msg, Err: err}
    close(ch)
}()
label := "Installing service..."  // per-subcommand label
_, runErr := tea.NewProgram(tui.NewDaemonModel(ch, label)).Run()
```

#### `DaemonModel` state

- Starts with a single `step{label, stepRunning}` + spinner
- On `DaemonResultMsg`: transitions to `stepDone` (message) or `stepError` (error)
- Quit hints appear once done

---

## Files changed

| File | Change |
|---|---|
| `internal/tui/steps.go` | **new** — `step` type, `renderSteps` |
| `internal/backup/backup.go` | add `PhaseMsg`, `BackupPhase`; send phases in `Run()` |
| `internal/tui/backup_model.go` | replace `lines []string` with explicit step fields; handle `PhaseMsg`; reclassify scan/check phases |
| `internal/doctor/doctor.go` | add `CheckAsync`, `CheckStartMsg` |
| `internal/tui/doctor_model.go` | make async; step-driven |
| `cmd/doctor.go` | goroutine + channel |
| `internal/tui/daemon_model.go` | async; step-driven; `DaemonResultMsg` |
| `cmd/daemon.go` | goroutine + channel per subcommand |

## Out of scope

- `setup`, `configure`, `status`, `logs` — no async waits, no changes
- `backup_test.go`, `doctor_test.go` — existing unit tests updated as needed to compile; no new test cases added
