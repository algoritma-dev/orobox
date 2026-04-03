package utils

import (
	"strings"
	"testing"
)

func TestCheckHostInReader(t *testing.T) {
	content := `
127.0.0.1 localhost
127.0.0.1 oro.demo
# 127.0.0.1 commented.host
::1       localhost
192.168.1.1 custom.host extra.alias
`
	tests := []struct {
		host     string
		expected bool
	}{
		{"localhost", true},
		{"oro.demo", true},
		{"commented.host", false},
		{"custom.host", true},
		{"extra.alias", true},
		{"unknown.host", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			r := strings.NewReader(content)
			result := CheckHostInReader(tt.host, r)
			if result != tt.expected {
				t.Errorf("expected %v for %s, got %v", tt.expected, tt.host, result)
			}
		})
	}
}
