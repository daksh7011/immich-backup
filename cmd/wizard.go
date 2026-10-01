// cmd/wizard.go
package cmd

import (
	"fmt"
	"strings"

	"github.com/daksh7011/immich-backup/internal/config"
	"github.com/daksh7011/immich-backup/internal/daemon"
)

// loadWizardConfig loads the config for setup and configure without
// validating it, so the wizard can repair a config every other command
// rejects. Only a file that cannot be read or parsed is an error.
func loadWizardConfig(path string) (*config.Config, error) {
	cfg, err := config.LoadRaw(path)
	if err != nil {
		return nil, fmt.Errorf("load config: %w (fix or remove %s, then re-run)", err, path)
	}
	return cfg, nil
}

// saveWizardConfig validates cfg and writes it to path. The wizard checks
// every field it asks for; Validate also covers the ones it does not (such
// as daemon.log_path). An invalid config is not written, since every other
// command would then refuse to load it.
func saveWizardConfig(path string, cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configuration not saved: %w\nedit %s to fix the fields above, then re-run", err, path)
	}
	if err := config.Save(path, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// scheduleChangeNotice returns the line to print when the wizard changed the
// schedule and a background service is installed: the unit or plist holds
// its own copy of the schedule, so the change applies only after a reinstall.
// installed is only consulted when the schedule changed.
func scheduleChangeNotice(oldSchedule, newSchedule string, installed func() bool) string {
	if strings.Join(strings.Fields(oldSchedule), " ") == strings.Join(strings.Fields(newSchedule), " ") {
		return ""
	}
	if !installed() {
		return ""
	}
	return fmt.Sprintf("The schedule changed to %q, but the installed background service still uses the old one.\n"+
		"Run `immich-backup daemon install` to apply it.", newSchedule)
}

// serviceInstalled reports whether a systemd unit or launchd plist exists.
// A unit that exists but cannot be read counts as installed, so the user is
// still told to reinstall; unsupported platforms have no service.
func serviceInstalled() bool {
	svc, err := daemon.Detect()
	if err != nil {
		return false
	}
	def, err := svc.Definition()
	return err != nil || def != nil
}

// finishWizard saves the wizard's result and prints what to do next.
func finishWizard(path, oldSchedule string, cfg *config.Config, saved string) error {
	if err := saveWizardConfig(path, cfg); err != nil {
		return err
	}
	fmt.Println(saved)
	if msg := scheduleChangeNotice(oldSchedule, cfg.Backup.Schedule, serviceInstalled); msg != "" {
		fmt.Println(msg)
	}
	return nil
}
