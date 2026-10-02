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
// entry for rclone, DOCKER_HOST for rootless Docker or Colima, the TLS
// settings for a Docker TCP endpoint) are captured at install time and
// passed explicitly. Empty fields are omitted.
type ServiceEnv struct {
	Path       string // PATH for the scheduled run
	DockerHost string // DOCKER_HOST
	// The other variables the Docker client reads (dockerclient.FromEnv), so
	// a scheduled run connects the way the manual run that installed it did.
	DockerTLSVerify  string // DOCKER_TLS_VERIFY
	DockerCertPath   string // DOCKER_CERT_PATH, made absolute
	DockerAPIVersion string // DOCKER_API_VERSION
}

// vars returns the non-empty variables in a stable order.
func (e ServiceEnv) vars() []envVar {
	var vs []envVar
	if e.Path != "" {
		vs = append(vs, envVar{"PATH", e.Path})
	}
	for _, v := range []envVar{
		{"DOCKER_HOST", e.DockerHost},
		{"DOCKER_TLS_VERIFY", e.DockerTLSVerify},
		{"DOCKER_CERT_PATH", e.DockerCertPath},
		{"DOCKER_API_VERSION", e.DockerAPIVersion},
	} {
		if v.Value != "" {
			vs = append(vs, v)
		}
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

// ResolveServiceEnv resolves rclone and captures the Docker client variables
// from the current environment. It fails if rclone cannot be found, because every scheduled
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
		Path:             BuildServicePATH(path.Dir(filepath.ToSlash(rclone))),
		DockerHost:       getenv("DOCKER_HOST"),
		DockerTLSVerify:  getenv("DOCKER_TLS_VERIFY"),
		DockerCertPath:   absCertPath(getenv("DOCKER_CERT_PATH")),
		DockerAPIVersion: getenv("DOCKER_API_VERSION"),
	}, nil
}

// absCertPath makes a relative DOCKER_CERT_PATH absolute against the
// install-time working directory: the scheduled run starts elsewhere.
func absCertPath(p string) string {
	if p == "" || path.IsAbs(filepath.ToSlash(p)) {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(abs)
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
