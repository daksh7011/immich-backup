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

func TestRequired(t *testing.T) {
	validate := required("Postgres user")
	for _, s := range []string{"", "   ", "\t"} {
		if err := validate(s); err == nil {
			t.Errorf("%q: expected error", s)
		}
	}
	if err := validate("postgres"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateScheduleInput(t *testing.T) {
	for _, s := range []string{"0 3 * * *", "30 23 * * *"} {
		if err := validateScheduleInput(s); err != nil {
			t.Errorf("%q: unexpected error: %v", s, err)
		}
	}
	for _, s := range []string{"", "0 3 * * 0", "0 */6 * * *", "@daily"} {
		if err := validateScheduleInput(s); err == nil {
			t.Errorf("%q: expected error", s)
		}
	}
}

func TestValidatePositiveInt(t *testing.T) {
	for _, s := range []string{"", "0", "-1", "abc"} {
		if err := validatePositiveInt(s); err == nil {
			t.Errorf("%q: expected error", s)
		}
	}
	if err := validatePositiveInt("48"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateUploadLocation(t *testing.T) {
	for _, s := range []string{"", "  ", "./library", "library"} {
		if err := validateUploadLocation(s); err == nil {
			t.Errorf("%q: expected error", s)
		}
	}
	for _, s := range []string{"/mnt/immich/library", "~/immich/library"} {
		if err := validateUploadLocation(s); err != nil {
			t.Errorf("%q: unexpected error: %v", s, err)
		}
	}
}
