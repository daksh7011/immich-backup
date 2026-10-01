// internal/daemon/env.go
package daemon

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/daksh7011/immich-backup/internal/rclonebin"
)

// servicePATHDirs are appended after rclone's dir in the scheduled run's PATH.
var servicePATHDirs = []string{"/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}

// ServiceEnv is the environment written into the unit or plist. systemd and
// launchd start jobs with a fixed minimal environment, not the user's shell
// environment, so values that only exist in ~/.bashrc or ~/.zshrc (a PATH
// entry for rclone, DOCKER_HOST for rootless Docker or Colima) are captured
// at install time and passed explicitly.
type ServiceEnv struct {
	Path       string // PATH for the scheduled run; omitted when empty
	DockerHost string // DOCKER_HOST; omitted when empty
}

// vars returns the non-empty variables in a stable order.
func (e ServiceEnv) vars() []envVar {
	var vs []envVar
	if e.Path != "" {
		vs = append(vs, envVar{"PATH", e.Path})
	}
	if e.DockerHost != "" {
		vs = append(vs, envVar{"DOCKER_HOST", e.DockerHost})
	}
	return vs
}

type envVar struct{ Key, Value string }

// BuildServicePATH returns rcloneDir followed by the standard system dirs,
// with duplicates removed. Values are literal: neither systemd nor launchd
// expands $PATH in them.
func BuildServicePATH(rcloneDir string) string {
	seen := map[string]bool{}
	var dirs []string
	for _, d := range append([]string{rcloneDir}, servicePATHDirs...) {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		dirs = append(dirs, d)
	}
	return strings.Join(dirs, ":")
}

// ResolveServiceEnv resolves rclone and captures DOCKER_HOST from the current
// environment. It fails if rclone cannot be found, because every scheduled
// run would then fail its prerequisite check.
func ResolveServiceEnv() (ServiceEnv, error) {
	return resolveServiceEnv(rclonebin.Resolve, os.Getenv)
}

func resolveServiceEnv(resolveRclone func() (string, error), getenv func(string) string) (ServiceEnv, error) {
	rclone, err := resolveRclone()
	if err != nil {
		return ServiceEnv{}, fmt.Errorf(
			"%w; scheduled backups need rclone — install it (https://rclone.org/install/) "+
				"or put it on PATH, then re-run `immich-backup daemon install`", err)
	}
	return ServiceEnv{
		// The unit and plist are Unix-only, so take the dir with "/" separators.
		Path:       BuildServicePATH(path.Dir(filepath.ToSlash(rclone))),
		DockerHost: getenv("DOCKER_HOST"),
	}, nil
}

// systemdQuote renders s as a double-quoted systemd value. Inside quotes
// systemd C-unescapes backslash sequences, then expands % specifiers, so
// backslash and quote are backslash-escaped, control characters are
// C-escaped (a raw newline would end the directive), and % becomes %%.
func systemdQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\' || r == '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '%':
			b.WriteString("%%")
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
