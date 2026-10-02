// internal/backup/export_test.go
package backup

// Test-only hooks for the external backup_test package.

// MediaSyncArgs exposes mediaSyncArgs so tests can assert the rclone argv.
var MediaSyncArgs = mediaSyncArgs

// SetRcloneBin swaps the rclone binary used by all runners and returns a
// func that restores the previous value.
func SetRcloneBin(bin string) (restore func()) {
	prev := rcloneBin
	rcloneBin = func() string { return bin }
	return func() { rcloneBin = prev }
}

// DBRemotePath exposes dbRemotePath.
var DBRemotePath = dbRemotePath
