// internal/docker/host_test.go
package docker_test

import (
	"testing"

	"github.com/daksh7011/immich-backup/internal/docker"
)

func TestHost_UsesDockerHostEnv(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///run/user/1000/docker.sock")
	if got := docker.Host(); got != "unix:///run/user/1000/docker.sock" {
		t.Errorf("Host() = %q, want DOCKER_HOST value", got)
	}
}

func TestHost_DefaultWhenUnset(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	if got := docker.Host(); got == "" {
		t.Error("Host() should return the default socket when DOCKER_HOST is unset")
	}
}
