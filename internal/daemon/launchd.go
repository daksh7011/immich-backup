// internal/daemon/launchd.go
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


const plistLabel = "com.immich-backup.agent"
const plistFilename = plistLabel + ".plist"

var plistTmpl = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>backup</string>
    </array>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key>
        <integer>{{.Hour}}</integer>
        <key>Minute</key>
        <integer>{{.Minute}}</integer>
    </dict>
    <key>StandardOutPath</key>
    <string>{{.LogPath}}</string>
    <key>StandardErrorPath</key>
    <string>{{.LogPath}}</string>
    <key>RunAtLoad</key>
    <false/>
</dict>
</plist>
`))

// GeneratePlist returns the launchd plist XML for the given binary path and config.
// Returns an error if the schedule uses step, range, or list expressions in the
// minute or hour fields — launchd requires plain integers in StartCalendarInterval.
// Exported for testing.
func GeneratePlist(binaryPath string, cfg *config.Config) (string, error) {
	parts := strings.Fields(cfg.Backup.Schedule)
	if len(parts) != 5 {
		return "", fmt.Errorf("schedule must have exactly 5 cron fields, got %d", len(parts))
	}
	minute, hour := parts[0], parts[1]
	if !isSimpleInt(minute) || !isSimpleInt(hour) {
		return "", fmt.Errorf(
			"launchd scheduling only supports simple hour/minute values (e.g. \"0 3 * * *\"); "+
				"step/range/list expressions like %q are not supported — use a specific time",
			cfg.Backup.Schedule,
		)
	}

	var buf strings.Builder
	if err := plistTmpl.Execute(&buf, map[string]string{
		"Label":      plistLabel,
		"BinaryPath": binaryPath,
		"Hour":       hour,
		"Minute":     minute,
		"LogPath":    cfg.Daemon.LogPath,
	}); err != nil {
		return "", fmt.Errorf("render plist template: %w", err)
	}
	return buf.String(), nil
}

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", plistFilename)
}

type launchdManager struct{}

func (m *launchdManager) Install(cfg *config.Config) error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	plist, err := GeneratePlist(bin, cfg)
	if err != nil {
		return fmt.Errorf("generate plist: %w", err)
	}
	path := plistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	return exec.Command("launchctl", "load", path).Run()
}

func (m *launchdManager) Uninstall() error {
	path := plistPath()
	_ = exec.Command("launchctl", "unload", path).Run()
	return os.Remove(path)
}

func (m *launchdManager) Start() error {
	return exec.Command("launchctl", "start", plistLabel).Run()
}

func (m *launchdManager) Stop() error {
	return exec.Command("launchctl", "stop", plistLabel).Run()
}

func (m *launchdManager) Restart() error {
	_ = m.Stop()
	return m.Start()
}

func (m *launchdManager) Status() (string, error) {
	out, err := exec.Command("launchctl", "list", plistLabel).Output()
	return string(out), err
}

func (m *launchdManager) Logs() (string, error) {
	return "", fmt.Errorf("use `immich-backup logs` to view logs")
}
