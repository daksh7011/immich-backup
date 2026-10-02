// internal/doctor/config_check_test.go
package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/config"
)

func TestCheckConfigLoad_ReportsLoadError(t *testing.T) {
	loadErr := errors.New("config validation failed:\n  - daemon.log_path must be an absolute path (got \"logs/d.log\")")
	r := checkConfigLoad(&config.Config{}, loadErr)
	if r.OK {
		t.Fatal("expected Config check to fail")
	}
	if !strings.Contains(r.Message, "daemon.log_path must be an absolute path") {
		t.Errorf("message should carry the load error, got %q", r.Message)
	}
	if strings.Contains(r.Message, "is required") {
		t.Errorf("message should not validate the empty fallback config, got %q", r.Message)
	}
}

func TestCheckConfigLoad_NoErrorValidatesConfig(t *testing.T) {
	r := checkConfigLoad(&config.Config{}, nil)
	if r.OK {
		t.Fatal("expected empty config to fail validation")
	}
	if !strings.Contains(r.Message, "is required") {
		t.Errorf("expected validation errors, got %q", r.Message)
	}
}
