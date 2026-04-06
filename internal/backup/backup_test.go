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
	if err := r.RunDatabase(name, "postgres", "immich", destPath); err != nil {
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
