// internal/rcloneconf/rcloneconf.go
package rcloneconf

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// EnsureConfigured checks that path contains at least one configured rclone remote.
// It returns an error if the config is missing or has no remotes. It does NOT
// launch rclone config interactively — use LaunchConfig for that.
func EnsureConfigured(path string) error {
	remotes, err := listRemotes(path)
	if err != nil || len(remotes) == 0 {
		return fmt.Errorf(
			"no rclone remote configured — run `rclone config --config %s` to add one",
			path,
		)
	}
	return nil
}

// LaunchConfig runs `rclone config --config path` interactively, inheriting
// the terminal. It is intended to be called from the setup/configure wizard.
func LaunchConfig(path string) error {
	return launchConfig(path)
}

// ListRemotes returns the names of all remotes in the rclone config at path.
// Remote names are returned without the trailing colon that rclone appends.
func ListRemotes(path string) ([]string, error) {
	remotes, err := listRemotes(path)
	if err != nil {
		return nil, err
	}
	stripped := make([]string, len(remotes))
	for i, r := range remotes {
		stripped[i] = strings.TrimSuffix(r, ":")
	}
	return stripped, nil
}

// listRemotes runs `rclone listremotes --config path` and returns remote names.
func listRemotes(path string) ([]string, error) {
	out, err := exec.Command("rclone", "listremotes", "--config", path).Output()
	if err != nil {
		return nil, fmt.Errorf("rclone listremotes: %w", err)
	}
	var remotes []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			remotes = append(remotes, line)
		}
	}
	return remotes, nil
}

// launchConfig runs `rclone config` interactively, inheriting the terminal.
func launchConfig(path string) error {
	cmd := exec.Command("rclone", "config", "--config", path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
