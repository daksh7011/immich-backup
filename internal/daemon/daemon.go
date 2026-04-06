// internal/daemon/daemon.go
package daemon

import (
	"fmt"
	"runtime"

	"github.com/daksh7011/immich-backup/internal/config"
)

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
