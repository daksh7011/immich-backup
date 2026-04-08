# Media Backup Progress Bar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a live progress bar with speed, ETA, and file count during media sync in the backup TUI, and surface rclone file-level I/O errors gracefully instead of printing them raw to stderr.

**Architecture:** Pre-scan source with `rclone size --json`, then run `rclone sync --use-json-log --stats 1s --ignore-errors` and parse its JSON stderr line-by-line. Parsed progress and errors are sent to the existing Bubble Tea channel as new typed message types. The `BackupModel` TUI handles a scan phase (spinner) and sync phase (determinate `bubbles/progress` bar + stats row + error list).

**Tech Stack:** Go 1.26, Cobra, Bubble Tea v2 (`charm.land/bubbletea/v2`), Lip Gloss v2 (`charm.land/lipgloss/v2`), Bubbles v2 (`charm.land/bubbles/v2` — progress + spinner), rclone (shelled out).

---

## File Map

| File | Change |
|------|--------|
| `internal/backup/backup.go` | Add `ScanMsg`, `MediaProgressMsg`, `RcloneErrorMsg`; add private JSON structs; add `parseRcloneLine`; update `Runner` interface + `RunMedia` impl; update `Run` |
| `internal/backup/backup_test.go` | Add unit tests for `parseRcloneLine`; update `TestRunMedia_SyncsFiles`; update `TestRun_HappyPath` |
| `internal/tui/backup_model.go` | Add `progress.Model` + `spinner.Model` fields; handle new message types in `Update`; update `View` |
| `go.mod` / `go.sum` | Promote `charm.land/bubbles/v2` from indirect to direct |

---

## Task 1: Add new message types and JSON structs to backup.go

**Files:**
- Modify: `internal/backup/backup.go`

- [ ] **Step 1: Add four new exported message types and three private JSON structs**

Open `internal/backup/backup.go`. After the existing `DoneMsg` declaration, add:

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
	Speed            float64 // bytes/sec; 0 on first tick
	ETA              *int64  // nil = not yet known (rclone emits null on first tick)
	FilesDone        int64
	FilesTotal       int64
}

// RcloneErrorMsg is sent when rclone reports a file-level error.
// Non-fatal: backup continues because RunMedia uses --ignore-errors.
type RcloneErrorMsg struct {
	Text string
}
```

After those, add the private JSON structs (used only inside this package):

```go
type rcloneSizeResult struct {
	Count    int64 `json:"count"`
	Bytes    int64 `json:"bytes"`
	Sizeless int64 `json:"sizeless"`
}

type rcloneLogLine struct {
	Level string       `json:"level"`
	Msg   string       `json:"msg"`
	Stats *rcloneStats `json:"stats"`
}

type rcloneStats struct {
	Bytes          int64   `json:"bytes"`
	TotalBytes     int64   `json:"totalBytes"`
	Speed          float64 `json:"speed"`
	ETA            *int64  `json:"eta"`
	Transfers      int64   `json:"transfers"`
	TotalTransfers int64   `json:"totalTransfers"`
}
```

Add `"encoding/json"` to the import block (it isn't imported yet).

- [ ] **Step 2: Build to verify no compilation errors**

```bash
go build ./internal/backup/...
```

Expected: no output (clean build).

- [ ] **Step 3: Commit**

```bash
git add internal/backup/backup.go
git commit -m "feat: add ScanMsg, MediaProgressMsg, RcloneErrorMsg and JSON structs"
```

---

## Task 2: Implement and test parseRcloneLine

**Files:**
- Modify: `internal/backup/backup.go`
- Modify: `internal/backup/backup_test.go`

- [ ] **Step 1: Write the failing tests first**

Add to `internal/backup/backup_test.go` (keep the existing `package backup_test` declaration):

```go
func TestParseRcloneLine_StatsLine(t *testing.T) {
	eta := int64(4)
	line := []byte(`{"level":"info","msg":"Transferred","stats":{"bytes":52523008,"totalBytes":375390208,"speed":60811070.1,"eta":4,"transfers":2,"totalTransfers":4},"source":"slog/logger.go:256"}`)
	msg, ok := backup.ParseRcloneLine(line)
	if !ok {
		t.Fatal("expected ok=true for stats line")
	}
	p, isProgress := msg.(backup.MediaProgressMsg)
	if !isProgress {
		t.Fatalf("expected MediaProgressMsg, got %T", msg)
	}
	if p.TransferredBytes != 52523008 {
		t.Errorf("TransferredBytes: got %d, want 52523008", p.TransferredBytes)
	}
	if p.TotalBytes != 375390208 {
		t.Errorf("TotalBytes: got %d, want 375390208", p.TotalBytes)
	}
	if p.ETA == nil || *p.ETA != eta {
		t.Errorf("ETA: got %v, want %d", p.ETA, eta)
	}
	if p.FilesDone != 2 {
		t.Errorf("FilesDone: got %d, want 2", p.FilesDone)
	}
	if p.FilesTotal != 4 {
		t.Errorf("FilesTotal: got %d, want 4", p.FilesTotal)
	}
}

func TestParseRcloneLine_NullETA(t *testing.T) {
	line := []byte(`{"level":"info","msg":"Transferred","stats":{"bytes":52523008,"totalBytes":375390208,"speed":0,"eta":null,"transfers":2,"totalTransfers":4}}`)
	msg, ok := backup.ParseRcloneLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	p := msg.(backup.MediaProgressMsg)
	if p.ETA != nil {
		t.Errorf("expected nil ETA, got %v", p.ETA)
	}
}

func TestParseRcloneLine_ErrorLine(t *testing.T) {
	line := []byte(`{"level":"error","msg":"photo.jpg: Failed to copy: input/output error","source":"slog/logger.go:256"}`)
	msg, ok := backup.ParseRcloneLine(line)
	if !ok {
		t.Fatal("expected ok=true for error line")
	}
	e, isErr := msg.(backup.RcloneErrorMsg)
	if !isErr {
		t.Fatalf("expected RcloneErrorMsg, got %T", msg)
	}
	if e.Text != "photo.jpg: Failed to copy: input/output error" {
		t.Errorf("unexpected Text: %q", e.Text)
	}
}

func TestParseRcloneLine_NonStatsInfoLine(t *testing.T) {
	line := []byte(`{"level":"info","msg":"Copied (new)","size":5242880,"object":"file1.bin","source":"slog/logger.go:256"}`)
	_, ok := backup.ParseRcloneLine(line)
	if ok {
		t.Error("expected ok=false for non-stats info line")
	}
}

func TestParseRcloneLine_InvalidJSON(t *testing.T) {
	_, ok := backup.ParseRcloneLine([]byte("not json"))
	if ok {
		t.Error("expected ok=false for invalid JSON")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/backup/... -run TestParseRcloneLine -v
```

Expected: compile error — `backup.ParseRcloneLine` undefined.

- [ ] **Step 3: Implement ParseRcloneLine in backup.go**

Add to `internal/backup/backup.go` (after the JSON structs from Task 1):

```go
// ParseRcloneLine parses one JSON log line from rclone --use-json-log output.
// Returns (MediaProgressMsg, true) for stats lines, (RcloneErrorMsg, true) for
// error lines, and (nil, false) for all other lines (info, debug, etc.).
// Exported so it can be tested from the _test package.
func ParseRcloneLine(line []byte) (any, bool) {
	var entry rcloneLogLine
	if err := json.Unmarshal(line, &entry); err != nil {
		return nil, false
	}
	if entry.Stats != nil {
		return MediaProgressMsg{
			TransferredBytes: entry.Stats.Bytes,
			TotalBytes:       entry.Stats.TotalBytes,
			Speed:            entry.Stats.Speed,
			ETA:              entry.Stats.ETA,
			FilesDone:        entry.Stats.Transfers,
			FilesTotal:       entry.Stats.TotalTransfers,
		}, true
	}
	if entry.Level == "error" {
		return RcloneErrorMsg{Text: entry.Msg}, true
	}
	return nil, false
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/backup/... -run TestParseRcloneLine -v
```

Expected: all 5 `TestParseRcloneLine_*` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backup/backup.go internal/backup/backup_test.go
git commit -m "feat: implement and test ParseRcloneLine for rclone JSON log parsing"
```

---

## Task 3: Refactor RunMedia — add channel, pre-scan, and JSON streaming

**Files:**
- Modify: `internal/backup/backup.go`
- Modify: `internal/backup/backup_test.go`

- [ ] **Step 1: Update the Runner interface**

In `internal/backup/backup.go`, change the `Runner` interface:

```go
type Runner interface {
	RunMedia(remote, srcDir string, ch chan<- any) error
	RunDatabase(container, pgUser, destPath string) error
}
```

Add a private send helper after the `Run` function (or near the top of the file):

```go
// sendMsg sends msg to ch without blocking. Safe to call with a nil ch.
func sendMsg(ch chan<- any, msg any) {
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}
```

- [ ] **Step 2: Rewrite RunMedia**

Replace the existing `RunMedia` method with:

```go
// RunMedia pre-scans srcDir with rclone size, then syncs srcDir to remote
// using rclone sync with JSON logging. Progress and file-level errors are sent
// to ch. ch may be nil (progress is silently discarded).
func (r *BackupRunner) RunMedia(remote, srcDir string, ch chan<- any) error {
	// Phase 1: pre-scan to get total file count and bytes.
	sizeOut, err := exec.Command("rclone", "--config", r.rcloneConf, "size", srcDir, "--json").Output()
	if err != nil {
		return fmt.Errorf("rclone size: %w", err)
	}
	var sizeResult rcloneSizeResult
	if err := json.Unmarshal(sizeOut, &sizeResult); err != nil {
		return fmt.Errorf("parse rclone size output: %w", err)
	}
	sendMsg(ch, ScanMsg{TotalFiles: sizeResult.Count, TotalBytes: sizeResult.Bytes})

	// Phase 2: sync with JSON log streaming.
	args := []string{
		"--config", r.rcloneConf,
		"sync", srcDir, remote,
		"--use-json-log", "--stats", "1s", "--log-level", "INFO",
		"--ignore-errors",
	}
	cmd := exec.Command("rclone", args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("rclone stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rclone sync start: %w", err)
	}

	var fileErrors int
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		msg, ok := ParseRcloneLine(scanner.Bytes())
		if !ok {
			continue
		}
		if _, isErr := msg.(RcloneErrorMsg); isErr {
			fileErrors++
		}
		sendMsg(ch, msg)
	}

	if err := cmd.Wait(); err != nil && fileErrors == 0 {
		// Non-zero exit with no captured file errors = fatal rclone error.
		return fmt.Errorf("rclone sync: %w", err)
	}
	return nil
}
```

Add `"bufio"` to the import block.

- [ ] **Step 3: Update Run() to pass ch to RunMedia**

In the `Run` function, change the `RunMedia` call from:

```go
if err := r.RunMedia(rcloneRemote, uploadLocation); err != nil {
```

to:

```go
if err := r.RunMedia(rcloneRemote, uploadLocation, ch); err != nil {
```

- [ ] **Step 4: Build to verify it compiles**

```bash
go build ./internal/backup/...
```

Expected: no output (clean build).

- [ ] **Step 5: Update TestRunMedia_SyncsFiles to pass a channel and assert progress messages**

Replace `TestRunMedia_SyncsFiles` in `internal/backup/backup_test.go` with:

```go
func TestRunMedia_SyncsFiles(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	for _, name := range []string{"photo1.jpg", "photo2.jpg", "video.mp4"} {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte("dummy-content"), 0644); err != nil {
			t.Fatalf("create dummy file: %v", err)
		}
	}

	confDir := t.TempDir()
	confPath := filepath.Join(confDir, "rclone.conf")
	confContent := fmt.Sprintf("[testdst]\ntype = local\nnounc = true\n")
	if err := os.WriteFile(confPath, []byte(confContent), 0600); err != nil {
		t.Fatalf("write rclone config: %v", err)
	}

	ch := make(chan any, 50)
	r := backup.New(newDockerClient(t), confPath)
	remote := "testdst:" + dstDir
	if err := r.RunMedia(remote, srcDir, ch); err != nil {
		t.Fatalf("RunMedia: %v", err)
	}
	close(ch)

	// Collect messages.
	var msgs []any
	for msg := range ch {
		msgs = append(msgs, msg)
	}

	// Must receive a ScanMsg.
	hasScan := false
	for _, m := range msgs {
		if s, ok := m.(backup.ScanMsg); ok {
			hasScan = true
			if s.TotalFiles != 3 {
				t.Errorf("ScanMsg.TotalFiles: got %d, want 3", s.TotalFiles)
			}
		}
	}
	if !hasScan {
		t.Error("expected at least one ScanMsg")
	}

	// Files must be synced.
	for _, name := range []string{"photo1.jpg", "photo2.jpg", "video.mp4"} {
		dst := filepath.Join(dstDir, name)
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			t.Errorf("expected %s to be synced to destination", name)
		}
	}
}
```

- [ ] **Step 6: Run all backup tests**

```bash
go test ./internal/backup/... -v -count=1
```

Expected: all tests PASS (TestRunDatabase_ProducesDump, TestRun_HappyPath, TestRun_DatabaseFailure_StopsEarlyAndClosesChannel, TestRunMedia_SyncsFiles, TestParseRcloneLine_*).

Note: TestRunDatabase_ProducesDump and TestRun_* spin up real Docker containers and take ~30 seconds each.

- [ ] **Step 7: Commit**

```bash
git add internal/backup/backup.go internal/backup/backup_test.go
git commit -m "feat: refactor RunMedia with pre-scan, JSON log streaming, and --ignore-errors"
```

---

## Task 4: Update BackupModel TUI with progress bar and error display

**Files:**
- Modify: `internal/tui/backup_model.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Promote bubbles/v2 to a direct dependency**

```bash
cd /home/slothie/IdeaProjects/immich-backup && go get charm.land/bubbles/v2
```

Expected: `go.mod` changes `charm.land/bubbles/v2` from `// indirect` to a direct dependency. `go.sum` may update.

- [ ] **Step 2: Rewrite backup_model.go**

Replace the entire contents of `internal/tui/backup_model.go` with:

```go
// internal/tui/backup_model.go
package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	"github.com/daksh7011/immich-backup/internal/backup"
)

// BackupModel is the Bubble Tea model for live backup progress display.
type BackupModel struct {
	ch      <-chan any
	lines   []string
	done    bool
	lastErr error

	// progress tracking
	progress     progress.Model
	spinner      spinner.Model
	scanning     bool
	totalBytes   int64
	totalFiles   int64
	mediaProg    *backup.MediaProgressMsg
	rcloneErrors []string
}

// NewBackupModel creates a BackupModel that reads from ch.
func NewBackupModel(ch <-chan any) BackupModel {
	p := progress.New(
		progress.WithColors(
			lipgloss.Color("#CBA6F7"), // colorMauve — filled portion
			lipgloss.Color("#CBA6F7"), // single color (no gradient)
		),
		progress.WithoutPercentage(), // we render percentage ourselves in the stats row
	)
	p.SetWidth(48)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#CBA6F7"))

	return BackupModel{
		ch:      ch,
		progress: p,
		spinner:  s,
	}
}

// Err returns the last fatal error received from the backup runner.
func (m BackupModel) Err() error { return m.lastErr }

func (m BackupModel) Init() tea.Cmd {
	return WaitForChan(m.ch)
}

func (m BackupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {

	case backup.ScanMsg:
		m.scanning = true
		m.totalFiles = v.TotalFiles
		m.totalBytes = v.TotalBytes
		return m, tea.Batch(WaitForChan(m.ch), m.spinner.Tick)

	case backup.MediaProgressMsg:
		m.scanning = false
		m.mediaProg = &v
		return m, WaitForChan(m.ch)

	case backup.RcloneErrorMsg:
		m.rcloneErrors = append(m.rcloneErrors, v.Text)
		return m, WaitForChan(m.ch)

	case backup.ProgressMsg:
		m.lines = append(m.lines, v.Text)
		return m, WaitForChan(m.ch)

	case backup.ErrorMsg:
		m.lastErr = v.Err
		m.done = true
		return m, tea.Quit

	case backup.DoneMsg:
		m.done = true
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if v.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m BackupModel) View() tea.View {
	out := renderHeader("  Backup Progress  ")

	// Text log lines (database steps, etc.)
	arrow := progressStyle.Render("→")
	for _, l := range m.lines {
		out += " " + arrow + " " + dimStyle.Render(l) + "\n"
	}

	// Progress section (only shown once scanning or syncing has started)
	if m.scanning || m.mediaProg != nil || (m.done && m.totalBytes > 0) {
		out += "\n"
		out += m.renderProgressSection()
	}

	// File-level rclone errors
	if len(m.rcloneErrors) > 0 {
		out += "\n"
		for _, e := range m.rcloneErrors {
			out += " " + errStyle.Render("✗") + " " + errStyle.Render(e) + "\n"
		}
	}

	// Fatal error or completion
	if m.lastErr != nil {
		out += "\n " + errStyle.Render("✗") + " " + errStyle.Render(fmt.Sprintf("Error: %v", m.lastErr)) + "\n"
	} else if m.done {
		out += "\n"
		if len(m.rcloneErrors) > 0 {
			out += " " + warnStyle.Render(fmt.Sprintf("✓ Backup complete with %d file error(s).", len(m.rcloneErrors))) + "\n"
		} else {
			out += " " + okStyle.Render("✓ Backup complete!") + "\n"
		}
	}

	if !m.done {
		out += renderHints([]Hint{{"ctrl+c", "abort"}})
	} else {
		out += renderHints([]Hint{{"q / enter", "quit"}})
	}

	return tea.NewView(out)
}

func (m BackupModel) renderProgressSection() string {
	out := ""

	if m.scanning {
		// Indeterminate: spinner + label
		out += " " + m.spinner.View() + " " + dimStyle.Render("Scanning library...") + "\n"
		return out
	}

	if m.mediaProg == nil {
		return out
	}

	p := m.mediaProg

	// Determinate progress bar
	pct := 0.0
	if p.TotalBytes > 0 {
		pct = float64(p.TransferredBytes) / float64(p.TotalBytes)
		if pct > 1.0 {
			pct = 1.0
		}
	}
	if m.done {
		pct = 1.0
	}
	pctLabel := fmt.Sprintf(" %3.0f%%", pct*100)
	out += " " + m.progress.ViewAs(pct) + dimStyle.Render(pctLabel) + "\n"

	// Stats row: speed | ETA | files
	speed := formatSpeed(p.Speed)
	eta := formatETA(p.ETA)
	files := fmt.Sprintf("%s / %s files",
		formatCount(p.FilesDone),
		formatCount(p.FilesTotal),
	)
	sep := sepStyle.Render("  │  ")
	out += " " + dimStyle.Render(speed+sep+eta+sep+files) + "\n"

	return out
}

func formatSpeed(bytesPerSec float64) string {
	if bytesPerSec <= 0 {
		return "0 B/s"
	}
	switch {
	case bytesPerSec >= 1<<30:
		return fmt.Sprintf("%.1f GB/s", bytesPerSec/(1<<30))
	case bytesPerSec >= 1<<20:
		return fmt.Sprintf("%.1f MB/s", bytesPerSec/(1<<20))
	case bytesPerSec >= 1<<10:
		return fmt.Sprintf("%.1f KB/s", bytesPerSec/(1<<10))
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}

func formatETA(eta *int64) string {
	if eta == nil {
		return "calculating..."
	}
	d := time.Duration(*eta) * time.Second
	if d <= 0 {
		return "done"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds remaining", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds remaining", m, s)
	}
	return fmt.Sprintf("%ds remaining", s)
}

func formatCount(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	return fmt.Sprintf("%d,%03d,%03d", n/1_000_000, (n/1000)%1000, n%1000)
}
```

- [ ] **Step 3: Build the whole project**

```bash
go build ./...
```

Expected: no output (clean build).

- [ ] **Step 4: Smoke-test the TUI manually**

Run the backup command against a small local rclone remote (your local config). Verify:
- "Scanning library..." with spinner appears first
- Progress bar appears with % + speed + ETA + file count
- On completion, `✓ Backup complete!` appears

- [ ] **Step 5: Commit**

```bash
git add internal/tui/backup_model.go go.mod go.sum
git commit -m "feat: add live progress bar, spinner scan phase, and rclone error display to BackupModel"
```

---

## Self-Review

**Spec coverage:**
- [x] Pre-scan with `rclone size --json` → Task 3
- [x] `ScanMsg`, `MediaProgressMsg`, `RcloneErrorMsg` types → Task 1
- [x] `parseRcloneLine` with unit tests → Task 2
- [x] `--use-json-log --stats 1s --ignore-errors` flags → Task 3
- [x] Spinner during scan phase → Task 4
- [x] Determinate progress bar during sync → Task 4
- [x] Stats row: speed, ETA, file count → Task 4
- [x] ETA nil → "calculating..." → Task 4
- [x] rclone errors collected + shown in red → Task 4
- [x] Clean completion message vs partial completion message → Task 4
- [x] bubbles/v2 promoted to direct dep → Task 4

**Type consistency check:**
- `ParseRcloneLine` exported in Task 2 and called in Task 3 via `ParseRcloneLine(scanner.Bytes())` ✓
- `sendMsg(ch, ScanMsg{...})` — `ScanMsg` defined in Task 1, used in Task 3 ✓
- `backup.MediaProgressMsg` referenced in `backup_model.go` — defined in Task 1 ✓
- `m.spinner.Tick` used as `tea.Cmd` in `Update` — `Tick()` is `func() tea.Msg` satisfying `tea.Cmd` ✓
- `progress.WithColors(lipgloss.Color(...))` — lipgloss.Color implements color.Color in lipgloss v2 ✓
- `m.progress.ViewAs(pct)` — returns string, used in View ✓
