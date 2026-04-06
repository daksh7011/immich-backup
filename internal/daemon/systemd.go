// internal/daemon/systemd.go
package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/daksh7011/immich-backup/internal/config"
)

const unitName = "immich-backup.service"
const timerName = "immich-backup.timer"

var unitTmpl = template.Must(template.New("unit").Parse(`[Unit]
Description=immich-backup media and database backup service
After=network.target

[Service]
Type=oneshot
ExecStart={{.BinaryPath}} backup
StandardOutput=append:{{.LogPath}}
StandardError=append:{{.LogPath}}
`))

var timerTmpl = template.Must(template.New("timer").Parse(`[Unit]
Description=immich-backup scheduled backup
Requires=immich-backup.service

[Timer]
OnCalendar={{.OnCalendar}}
Persistent=true

[Install]
WantedBy=timers.target
`))

// GenerateSystemdUnit returns the systemd service unit file content.
// Exported for testing.
func GenerateSystemdUnit(binaryPath string, cfg *config.Config) string {
	var buf strings.Builder
	_ = unitTmpl.Execute(&buf, map[string]string{
		"BinaryPath": binaryPath,
		"LogPath":    cfg.Daemon.LogPath,
	})
	return buf.String()
}

// GenerateSystemdTimer returns the systemd timer unit file content derived
// from the cron schedule in cfg. Returns an error if the schedule uses step
// expressions (e.g. */6) which cannot be directly expressed as a single
// OnCalendar entry.
// Exported for testing.
func GenerateSystemdTimer(schedule string) (string, error) {
	onCal, err := cronToOnCalendar(schedule)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	if err := timerTmpl.Execute(&buf, map[string]string{"OnCalendar": onCal}); err != nil {
		return "", fmt.Errorf("render timer template: %w", err)
	}
	return buf.String(), nil
}

// cronToOnCalendar converts a simple "MINUTE HOUR * * *" cron expression to a
// systemd OnCalendar value (e.g. "*-*-* 03:00:00"). Returns an error for
// step, range, or list expressions in the minute or hour fields.
func cronToOnCalendar(schedule string) (string, error) {
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return "", fmt.Errorf("schedule must have exactly 5 cron fields, got %d", len(parts))
	}
	minute, hour := parts[0], parts[1]
	if !isSimpleInt(minute) || !isSimpleInt(hour) {
		return "", fmt.Errorf(
			"daemon scheduling only supports simple hour/minute values (e.g. \"0 3 * * *\"); "+
				"step/range/list expressions like %q are not supported — use a specific time",
			schedule,
		)
	}
	m, _ := strconv.Atoi(minute)
	h, _ := strconv.Atoi(hour)
	return fmt.Sprintf("*-*-* %02d:%02d:00", h, m), nil
}

func unitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", unitName)
}

func timerPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", timerName)
}

type systemdManager struct{}

func (m *systemdManager) Install(cfg *config.Config) error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	unit := GenerateSystemdUnit(bin, cfg)
	timerContent, err := GenerateSystemdTimer(cfg.Backup.Schedule)
	if err != nil {
		return fmt.Errorf("generate timer: %w", err)
	}
	uPath := unitPath()
	tPath := timerPath()
	if err := os.MkdirAll(filepath.Dir(uPath), 0755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}
	if err := os.WriteFile(uPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	if err := os.WriteFile(tPath, []byte(timerContent), 0644); err != nil {
		return fmt.Errorf("write timer file: %w", err)
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	if err := exec.Command("systemctl", "--user", "enable", timerName).Run(); err != nil {
		return fmt.Errorf("enable timer: %w", err)
	}
	return exec.Command("systemctl", "--user", "start", timerName).Run()
}

func (m *systemdManager) Uninstall() error {
	_ = exec.Command("systemctl", "--user", "stop", timerName).Run()
	_ = exec.Command("systemctl", "--user", "disable", timerName).Run()
	_ = os.Remove(timerPath())
	_ = os.Remove(unitPath())
	return exec.Command("systemctl", "--user", "daemon-reload").Run()
}

func (m *systemdManager) Start() error {
	return exec.Command("systemctl", "--user", "start", timerName).Run()
}

func (m *systemdManager) Stop() error {
	return exec.Command("systemctl", "--user", "stop", timerName).Run()
}

func (m *systemdManager) Restart() error {
	return exec.Command("systemctl", "--user", "restart", timerName).Run()
}

func (m *systemdManager) Status() (string, error) {
	out, err := exec.Command("systemctl", "--user", "status", timerName).Output()
	return string(out), err
}

func (m *systemdManager) Logs() (string, error) {
	out, err := exec.Command("journalctl", "--user", "-u", unitName, "-n", "100").Output()
	return string(out), err
}
