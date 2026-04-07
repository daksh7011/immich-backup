// cmd/rclone.go
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/daksh7011/immich-backup/internal/rcloneconf"
)

// promptRcloneConfig asks the user whether to launch rclone config interactively.
// It runs entirely outside the TUI to avoid terminal ownership conflicts.
func promptRcloneConfig(rcloneConfPath string) {
	fmt.Print("Configure rclone remote now? [y/N]: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
		return
	}
	if err := rcloneconf.LaunchConfig(rcloneConfPath); err != nil {
		fmt.Fprintln(os.Stderr, "rclone config:", err)
	}
}
