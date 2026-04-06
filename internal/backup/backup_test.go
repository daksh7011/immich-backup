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

	// Create dummy media files in source
	for _, name := range []string{"photo1.jpg", "photo2.jpg", "video.mp4"} {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte("dummy-content"), 0644); err != nil {
			t.Fatalf("create dummy file: %v", err)
		}
	}

	// Write a temp rclone.conf with a local: remote
	confDir := t.TempDir()
	confPath := filepath.Join(confDir, "rclone.conf")
	confContent := fmt.Sprintf("[testdst]\ntype = local\nnounc = true\n")
	if err := os.WriteFile(confPath, []byte(confContent), 0600); err != nil {
		t.Fatalf("write rclone config: %v", err)
	}

	r := backup.New(newDockerClient(t), confPath)
	remote := "testdst:" + dstDir
	if err := r.RunMedia(remote, srcDir); err != nil {
		t.Fatalf("RunMedia: %v", err)
	}

	for _, name := range []string{"photo1.jpg", "photo2.jpg", "video.mp4"} {
		dst := filepath.Join(dstDir, name)
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			t.Errorf("expected %s to be synced to destination", name)
		}
	}
}
