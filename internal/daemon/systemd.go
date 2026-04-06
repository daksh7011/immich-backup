// internal/daemon/systemd.go
package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/daksh7011/immich-backup/internal/config"
)

const unitName = "immich-backup.service"

var unitTmpl = template.Must(template.New("unit").Parse(`[Unit]
Description=immich-backup media and database backup service
After=network.target

[Service]
Type=oneshot
ExecStart={{.BinaryPath}} backup
StandardOutput=append:{{.LogPath}}
StandardError=append:{{.LogPath}}

[Install]
WantedBy=default.target
`))

// GenerateSystemdUnit returns the systemd unit file content.
// Exported for testing.
func GenerateSystemdUnit(binaryPath string, cfg *config.Config) string {
	var buf strings.Builder
	_ = unitTmpl.Execute(&buf, map[string]string{
		"BinaryPath": binaryPath,
		"LogPath":    cfg.Daemon.LogPath,
	})
	return buf.String()
}

func unitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", unitName)
}

type systemdManager struct{}

func (m *systemdManager) Install(cfg *config.Config) error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	unit := GenerateSystemdUnit(bin, cfg)
	path := unitPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return exec.Command("systemctl", "--user", "enable", unitName).Run()
}

func (m *systemdManager) Uninstall() error {
	_ = exec.Command("systemctl", "--user", "disable", unitName).Run()
	return os.Remove(unitPath())
}

func (m *systemdManager) Start() error {
	return exec.Command("systemctl", "--user", "start", unitName).Run()
}

func (m *systemdManager) Stop() error {
	return exec.Command("systemctl", "--user", "stop", unitName).Run()
}

func (m *systemdManager) Restart() error {
	return exec.Command("systemctl", "--user", "restart", unitName).Run()
}

func (m *systemdManager) Status() (string, error) {
	out, err := exec.Command("systemctl", "--user", "status", unitName).Output()
	return string(out), err
}

func (m *systemdManager) Logs() (string, error) {
	out, err := exec.Command("journalctl", "--user", "-u", unitName, "-n", "100").Output()
	return string(out), err
}
