package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/daksh7011/immich-backup/internal/daemon"
)

func TestDaemonLogPath(t *testing.T) {
	const cfgPath = "/home/u/.immich-backup/logs/daemon.log"
	if p, w := daemonLogPath(cfgPath, true, nil); p != cfgPath || w != "" {
		t.Errorf("not installed: %q %q", p, w)
	}
	same := &daemon.Definition{LogPath: cfgPath}
	if p, w := daemonLogPath(cfgPath, true, same); p != cfgPath || w != "" {
		t.Errorf("same path: %q %q", p, w)
	}
	other := &daemon.Definition{LogPath: "/var/tmp/old.log"}
	p, w := daemonLogPath(cfgPath, true, other)
	if p != "/var/tmp/old.log" || !strings.Contains(w, "daemon install") || !strings.Contains(w, cfgPath) {
		t.Errorf("differing path: %q %q", p, w)
	}
	if p, w := daemonLogPath(cfgPath, false, other); p != "/var/tmp/old.log" || w != "" {
		t.Errorf("config not loaded: %q %q", p, w)
	}
}

func TestPrintNoLogFile_SuggestsDaemonStatus(t *testing.T) {
	var buf bytes.Buffer
	printNoLogFile(&buf, "/x/daemon.log", true)
	if !strings.Contains(buf.String(), "/x/daemon.log") || !strings.Contains(buf.String(), "daemon status") {
		t.Errorf("output = %q", buf.String())
	}
	buf.Reset()
	printNoLogFile(&buf, "/x/daemon.log", false)
	if strings.Contains(buf.String(), "daemon status") {
		t.Errorf("no service installed: no service manager: should not suggest daemon status: %q", buf.String())
	}
}
