// internal/daemon/daemon.go
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/daksh7011/immich-backup/internal/config"
)

// EnsureLogFile creates the daemon log file and its parent directories if
// missing, without truncating an existing log. systemd (append:) and launchd
// (StandardOutPath) open the log before running the job and do not create
// parent directories, so a missing dir makes every scheduled run fail before
// the binary starts (systemd 209/STDOUT, launchd exit 78).
func EnsureLogFile(path string) error {
	if path == "" {
		return fmt.Errorf("daemon log path is empty; set daemon.log_path in the config")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("daemon log path %q must be absolute", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("create log file: %w", err)
	}
	return f.Close()
}

// Manager controls the immich-backup background service.
type Manager interface {
	Install(cfg *config.Config) error
	Uninstall() error
	Start() error
	Stop() error
	Restart() error
	Status() (string, error)
	Logs() (string, error)
}

// isSimpleInt reports whether s is a non-negative decimal integer with no
// step (/), range (-), or list (,) syntax. Used to validate cron hour/minute
// fields before inserting them into launchd plist integers or systemd OnCalendar.
func isSimpleInt(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

// New returns the platform-appropriate Manager.
// Panics if the platform is not supported (Windows is out of scope).
func New() Manager {
	switch runtime.GOOS {
	case "darwin":
		return &launchdManager{}
	case "linux":
		return &systemdManager{}
	default:
		panic(fmt.Sprintf("unsupported platform: %s", runtime.GOOS))
	}
}
