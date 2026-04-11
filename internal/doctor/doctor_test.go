// internal/doctor/doctor_test.go
package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/daksh7011/immich-backup/internal/doctor"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startPostgres(t *testing.T) (containerName string, cleanup func()) {
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
	rawName, _ := pg.Name(ctx)
	return strings.TrimPrefix(rawName, "/"), func() { _ = pg.Terminate(ctx) }
}

func validCfg(t *testing.T, containerName string) *config.Config {
	t.Helper()
	return &config.Config{
		Immich: config.ImmichConfig{
			UploadLocation:    "/mnt/immich",
			PostgresContainer: containerName,
			PostgresUser:      "postgres",
			PostgresDB:        "immich",
		},
		Backup: config.BackupConfig{
			RcloneRemote:      "local:/tmp",
			Schedule:          "0 3 * * *",
			DBBackupFrequency: "0 */6 * * *",
			Retention:         config.RetentionConfig{Daily: 7, Weekly: 4},
			Transfers:         48,
			Checkers:          128,
			BufferSize:        "64M",
		},
		Daemon: config.DaemonConfig{LogPath: "/tmp/daemon.log"},
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

func TestCheck_AllPass(t *testing.T) {
	pgName, cleanup := startPostgres(t)
	t.Cleanup(cleanup)

	client, err := docker.NewClient()
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer client.Close()

	confPath := filepath.Join(t.TempDir(), "rclone.conf")
	writeFile(t, confPath, "[local]\ntype = local\n")

	cfg := validCfg(t, pgName)
	results := doctor.Check(client, cfg, confPath)

	for _, r := range results {
		if !r.OK {
			t.Errorf("check %q failed: %s (remedy: %s)", r.Name, r.Message, r.Remedy)
		}
	}
}

func TestCheck_PostgresDown(t *testing.T) {
	client, err := docker.NewClient()
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer client.Close()

	confPath := filepath.Join(t.TempDir(), "rclone.conf")
	writeFile(t, confPath, "[local]\ntype = local\n")

	cfg := &config.Config{
		Immich: config.ImmichConfig{
			PostgresContainer: "this-container-does-not-exist-xyz",
			UploadLocation:    "/mnt/immich",
			PostgresUser:      "postgres",
			PostgresDB:        "immich",
		},
		Backup: config.BackupConfig{
			RcloneRemote:      "local:/tmp",
			Schedule:          "0 3 * * *",
			DBBackupFrequency: "0 */6 * * *",
			Retention:         config.RetentionConfig{Daily: 7, Weekly: 4},
			Transfers:         48,
			Checkers:          128,
			BufferSize:        "64M",
		},
		Daemon: config.DaemonConfig{LogPath: "/tmp/daemon.log"},
	}

	results := doctor.Check(client, cfg, confPath)
	var pgResult *doctor.CheckResult
	for i := range results {
		if results[i].Name == "Postgres Container" {
			pgResult = &results[i]
		}
	}
	if pgResult == nil {
		t.Fatal("expected a 'Postgres Container' check result")
	}
	if pgResult.OK {
		t.Error("expected Postgres Container check to fail")
	}
	if pgResult.Remedy == "" {
		t.Error("expected a non-empty Remedy for failed check")
	}
}
