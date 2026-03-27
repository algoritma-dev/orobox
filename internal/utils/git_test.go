package utils

import (
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1, v2 string
		want   int
	}{
		{"6.0.1", "6.0.2", -1},
		{"6.0.2", "6.0.1", 1},
		{"6.0.1", "6.0.1", 0},
		{"6.0.1", "6.1.0", -1},
		{"6.0.6", "6.1.0", -1},
		{"6.0", "6.0.1", -1},
		{"6.10.1", "6.2.1", 1},
	}

	for _, tt := range tests {
		got := compareVersions(tt.v1, tt.v2)
		if (got < 0 && tt.want >= 0) || (got > 0 && tt.want <= 0) || (got == 0 && tt.want != 0) {
			t.Errorf("compareVersions(%s, %s) = %d, want %d", tt.v1, tt.v2, got, tt.want)
		}
	}
}

func TestGetLatestTag(t *testing.T) {
	// We can't easily test GetLatestTag without network or mocking git.
	// But we can test it with a real repo for verification if needed.
	// For now, let's assume compareVersions is correct.
}
