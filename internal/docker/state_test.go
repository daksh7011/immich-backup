// internal/docker/state_test.go
package docker

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestIsRunning(t *testing.T) {
	tests := []struct {
		name  string
		state *container.State
		want  bool
	}{
		{"running", &container.State{Running: true}, true},
		{"stopped", &container.State{}, false},
		{"restarting", &container.State{Running: true, Restarting: true}, false},
		{"paused", &container.State{Running: true, Paused: true}, false},
		{"no state", nil, false},
	}
	for _, tc := range tests {
		if got := isRunning(tc.state); got != tc.want {
			t.Errorf("%s: isRunning = %v, want %v", tc.name, got, tc.want)
		}
	}
}
