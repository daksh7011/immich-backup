// internal/rclonebin/rclonebin.go
package rclonebin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const binName = "rclone"

// Resolve returns the absolute path of the rclone binary. It tries PATH
// first, then well-known install dirs. The fallback matters for scheduled
// runs: launchd starts jobs with PATH=/usr/bin:/bin:/usr/sbin:/sbin, and an
// rclone from Homebrew, Linuxbrew or ~/.local/bin is not on that PATH.
func Resolve() (string, error) {
	return resolve(exec.LookPath, fallbackDirs())
}

// Path returns the resolved rclone path, or the bare name "rclone" when it
// cannot be resolved so exec reports the usual "not found" error.
func Path() string {
	p, err := Resolve()
	if err != nil {
		return binName
	}
	return p
}

// fallbackDirs lists install locations to search when rclone is not on PATH.
func fallbackDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"))
	}
	return append(dirs,
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/home/linuxbrew/.linuxbrew/bin",
		"/usr/bin",
		"/snap/bin",
	)
}

func resolve(lookPath func(string) (string, error), dirs []string) (string, error) {
	if p, err := lookPath(binName); err == nil {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", fmt.Errorf("resolve rclone path %q: %w", p, err)
		}
		return abs, nil
	}
	for _, d := range dirs {
		p := filepath.Join(d, binName)
		if isExecutable(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("rclone not found in PATH or in %s", strings.Join(dirs, ", "))
}

// isExecutable reports whether path is a regular file with an execute bit.
// Windows has no execute bits, so any regular file counts there.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0111 != 0
}
