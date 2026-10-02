// internal/daemon/executable.go
package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const binaryName = "immich-backup"

// cellarRe matches a Homebrew keg path: <prefix>/Cellar/<formula>/<version>/bin/<file>.
var cellarRe = regexp.MustCompile(`^(.*)/Cellar/([^/]+)/[^/]+/bin/([^/]+)$`)

// StableExecutable returns the path of the running binary to write into the
// unit or plist. On Linux os.Executable resolves symlinks, so a Homebrew
// install yields the versioned Cellar path, which `brew upgrade` deletes and
// every later run fails with 203/EXEC. The stable PATH entry or Homebrew link
// is used instead when it points at the same file. Temporary builds (go run)
// are rejected, since the job would break once they are cleaned up.
func StableExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find executable: %w", err)
	}
	return stableExecutable(exe, exec.LookPath, sameFile, os.TempDir())
}

func stableExecutable(exe string, lookPath func(string) (string, error), same func(a, b string) bool, tmpDir string) (string, error) {
	bin := exe
	if p, err := lookPath(binaryName); err == nil && isAbsPath(p) && same(p, exe) {
		bin = p
	} else if m := cellarRe.FindStringSubmatch(filepath.ToSlash(exe)); m != nil {
		prefix, formula, file := m[1], m[2], m[3]
		for _, link := range []string{
			path.Join(prefix, "opt", formula, "bin", file), // Homebrew's upgrade-stable keg link
			path.Join(prefix, "bin", file),
		} {
			if same(link, exe) {
				bin = link
				break
			}
		}
	}

	if !isAbsPath(bin) {
		return "", fmt.Errorf("executable path %q is not absolute; re-run `immich-backup daemon install` using the installed binary", bin)
	}
	if isTempPath(bin, tmpDir) {
		return "", fmt.Errorf(
			"executable %s is a temporary build (go run or go test) that will be cleaned up; "+
				"install immich-backup (Homebrew, go install, or a release binary) and re-run "+
				"`immich-backup daemon install` from it", bin)
	}
	return bin, nil
}

// sameFile reports whether a and b exist and are the same file after
// following symlinks.
func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// isAbsPath accepts Unix-style absolute paths on every OS, since the unit
// and plist are Unix-only but the generators are tested on Windows too.
func isAbsPath(p string) bool {
	return strings.HasPrefix(p, "/") || filepath.IsAbs(p)
}

// isTempPath reports whether p is a go build cache binary or lies under tmpDir.
func isTempPath(p, tmpDir string) bool {
	p = filepath.ToSlash(p)
	if strings.Contains(p, "/go-build") {
		return true
	}
	if tmpDir == "" {
		return false
	}
	tmp := strings.TrimSuffix(filepath.ToSlash(filepath.Clean(tmpDir)), "/")
	return tmp != "" && strings.HasPrefix(p, tmp+"/")
}

// checkUnitPath rejects paths with control characters. A newline in a path
// written into a unit would end the directive and inject the rest as new
// lines, and paths after append: cannot be quoted.
func checkUnitPath(p string) error {
	if strings.IndexFunc(p, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return fmt.Errorf("path %q contains control characters and cannot be used in a systemd unit", p)
	}
	return nil
}
