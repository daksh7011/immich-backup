package tui

import "testing"

func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		secs float64
		want string
	}{
		{0, "0s"},
		{-1, "0s"},
		{30, "30s"},
		{60, "1m00s"},
		{90, "1m30s"},
		{3600, "1h00m00s"},
		{3661, "1h01m01s"},
	}
	for _, tt := range tests {
		got := formatElapsed(tt.secs)
		if got != tt.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", tt.secs, got, tt.want)
		}
	}
}

func TestTruncateMid(t *testing.T) {
	tests := []struct {
		s        string
		maxRunes int
		want     string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 7, "hel…rld"},
		{"abcdefghij", 5, "ab…ij"},
		{"abc", 0, ""},
		{"résumé", 4, "r…mé"},
	}
	for _, tt := range tests {
		got := truncateMid(tt.s, tt.maxRunes)
		if got != tt.want {
			t.Errorf("truncateMid(%q, %d) = %q, want %q", tt.s, tt.maxRunes, got, tt.want)
		}
	}
}
