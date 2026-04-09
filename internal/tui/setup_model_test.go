package tui

import "testing"

func TestSplitRemote(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantPath string
	}{
		{"b2-encrypted:immich-backup", "b2-encrypted", "immich-backup"},
		{"gdrive:", "gdrive", ""},
		{"local", "local", ""},
		{"s3:bucket/folder", "s3", "bucket/folder"},
	}
	for _, tc := range tests {
		name, path := splitRemote(tc.input)
		if name != tc.wantName || path != tc.wantPath {
			t.Errorf("splitRemote(%q) = (%q, %q), want (%q, %q)",
				tc.input, name, path, tc.wantName, tc.wantPath)
		}
	}
}
