// internal/rcloneconf/rcloneconf.go
package rcloneconf

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// EnsureConfigured checks that path contains at least one configured rclone remote.
// If the config is missing or has no remotes it:
//  1. Informs the user.
//  2. Launches `rclone config --config <path>` interactively (inherits the terminal).
//  3. After rclone config exits, verifies ≥1 remote now exists.
//  4. Returns an error if still unconfigured (constitution Principle III).
func EnsureConfigured(path string) error {
	remotes, err := listRemotes(path)
	if err != nil || len(remotes) == 0 {
		fmt.Fprintf(os.Stdout,
			"No rclone remote configured at %s. Launching rclone config...\n", path)
		if launchErr := launchConfig(path); launchErr != nil {
			return fmt.Errorf("rclone config exited with error: %w", launchErr)
		}
		remotes, err = listRemotes(path)
		if err != nil || len(remotes) == 0 {
			return fmt.Errorf(
				"no rclone remote configured — run `rclone config --config %s` to add one",
				path,
			)
		}
	}
	return nil
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
