// internal/docker/docker_test.go
package docker_test

import (
	"context"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/docker"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startAlpine(t *testing.T) (name string, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:      "alpine:latest",
			Cmd:        []string{"sleep", "60"},
			WaitingFor: wait.ForExec([]string{"echo", "ready"}),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start alpine: %v", err)
	}
	rawName, _ := c.Name(ctx)
	return strings.TrimPrefix(rawName, "/"), func() { _ = c.Terminate(ctx) }
}

func TestClient_Exec(t *testing.T) {
	name, cleanup := startAlpine(t)
	t.Cleanup(cleanup)

	client, err := docker.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	out, err := client.Exec(name, "echo", "hello-from-exec")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "hello-from-exec") {
		t.Errorf("expected 'hello-from-exec' in output, got: %q", got)
	}
	// Ensure no Docker stream multiplexing headers leaked into output.
	// A clean decode produces printable text only; binary header bytes would
	// appear as non-printable runes before the first word.
	if len(got) > 0 && got[0] != 'h' {
		t.Errorf("output starts with unexpected byte 0x%02x; possible stdcopy decode failure", got[0])
	}
}

func TestClient_IsContainerRunning_True(t *testing.T) {
	name, cleanup := startAlpine(t)
	t.Cleanup(cleanup)

	client, err := docker.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	running, err := client.IsContainerRunning(name)
	if err != nil {
		t.Fatalf("IsContainerRunning: %v", err)
	}
	if !running {
		t.Error("expected container to be running")
	}
}

func TestClient_IsContainerRunning_NotFound(t *testing.T) {
	client, err := docker.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	running, err := client.IsContainerRunning("definitely-does-not-exist-xyzxyz")
	if err != nil {
		t.Fatalf("unexpected error for missing container: %v", err)
	}
	if running {
		t.Error("expected false for non-existent container")
	}
}
