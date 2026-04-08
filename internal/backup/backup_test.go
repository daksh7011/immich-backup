// internal/backup/backup_test.go
package backup_test

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/backup"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// localRcloneConf writes a rclone.conf with a local remote pointing at dir.
func localRcloneConf(t *testing.T, remoteName string) (confPath, remoteSyntax string) {
	t.Helper()
	dstDir := t.TempDir()
	confPath = filepath.Join(t.TempDir(), "rclone.conf")
	content := fmt.Sprintf("[%s]\ntype = local\nnounc = true\n", remoteName)
	if err := os.WriteFile(confPath, []byte(content), 0600); err != nil {
		t.Fatalf("write rclone.conf: %v", err)
	}
	return confPath, remoteName + ":" + dstDir
}

func newDockerClient(t *testing.T) *docker.Client {
	t.Helper()
	c, err := docker.NewClient()
	if err != nil {
		t.Fatalf("docker.NewClient: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestRunDatabase_ProducesDump(t *testing.T) {
	ctx := context.Background()
	pg, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:17-alpine"),
		postgres.WithDatabase("immich"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })

	rawName, _ := pg.Name(ctx)
	name := strings.TrimPrefix(rawName, "/")

	// Write a minimal rclone.conf — RunDatabase doesn't use rclone but New() requires it.
	confPath := filepath.Join(t.TempDir(), "rclone.conf")
	_ = os.WriteFile(confPath, []byte("[local]\ntype = local\n"), 0600)

	destPath := filepath.Join(t.TempDir(), "dump.sql.gz")
	r := backup.New(newDockerClient(t), confPath)
	if err := r.RunDatabase(name, "postgres", destPath); err != nil {
		t.Fatalf("RunDatabase: %v", err)
	}

	f, err := os.Open(destPath)
	if err != nil {
		t.Fatalf("open dump: %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("not a valid gzip: %v", err)
	}
	content, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	if len(content) == 0 {
		t.Error("dump content is empty")
	}
	// Verify the output is valid SQL, not Docker stream framing headers.
	if !strings.Contains(string(content), "PostgreSQL database dump") {
		t.Errorf("dump does not contain expected SQL preamble; possible stdcopy decode failure. First 200 bytes: %q", string(content[:min(200, len(content))]))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func startPostgres(t *testing.T) (containerName string) {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:17-alpine"),
		postgres.WithDatabase("immich"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })
	rawName, _ := pg.Name(ctx)
	return strings.TrimPrefix(rawName, "/")
}

func collectChan(ch <-chan any) []any {
	var msgs []any
	for msg := range ch {
		msgs = append(msgs, msg)
	}
	return msgs
}

func TestRun_HappyPath(t *testing.T) {
	pgName := startPostgres(t)

	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "photo.jpg"), []byte("dummy"), 0644); err != nil {
		t.Fatalf("create dummy file: %v", err)
	}

	confPath, remote := localRcloneConf(t, "rundst")

	ch := make(chan any, 20)
	go backup.Run(confPath, pgName, "postgres", srcDir, remote, newDockerClient(t), ch)
	msgs := collectChan(ch)

	if len(msgs) == 0 {
		t.Fatal("no messages received on channel")
	}
	// Channel must be closed (range above would block otherwise — already verified)
	// Last message must be DoneMsg
	if _, ok := msgs[len(msgs)-1].(backup.DoneMsg); !ok {
		t.Errorf("expected DoneMsg as last message, got %T: %v", msgs[len(msgs)-1], msgs[len(msgs)-1])
	}
	for _, msg := range msgs {
		if em, ok := msg.(backup.ErrorMsg); ok {
			t.Errorf("unexpected ErrorMsg: %v", em.Err)
		}
	}
}

func TestRun_DatabaseFailure_StopsEarlyAndClosesChannel(t *testing.T) {
	confPath, remote := localRcloneConf(t, "faildst")

	ch := make(chan any, 20)
	// Non-existent container forces an immediate database failure.
	go backup.Run(confPath, "nonexistent-container-xyzxyz", "postgres", t.TempDir(), remote, newDockerClient(t), ch)
	msgs := collectChan(ch)

	hasError := false
	for _, msg := range msgs {
		if _, ok := msg.(backup.ErrorMsg); ok {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected at least one ErrorMsg when database container is missing")
	}
	if len(msgs) > 0 {
		if _, ok := msgs[len(msgs)-1].(backup.DoneMsg); ok {
			t.Error("must not send DoneMsg when database backup fails")
		}
	}
}

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
	confContent := "[testdst]\ntype = local\nnounc = true\n"
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
