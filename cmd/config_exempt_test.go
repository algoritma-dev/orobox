package cmd

import "testing"

func TestIsConfigExempt(t *testing.T) {
	tests := []struct {
		name string
		cmd  string // command path to look up under rootCmd
		want bool
	}{
		{"init exempt", "init", true},
		{"self-update exempt", "self-update", true},
		{"create parent exempt", "create", true},
		{"create project exempt", "create project", true},
		{"create bundle exempt", "create bundle", true},
		{"up not exempt", "up", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, err := rootCmd.Find(splitArgs(tt.cmd))
			if err != nil {
				t.Fatalf("find %q: %v", tt.cmd, err)
			}
			if got := isConfigExempt(c); got != tt.want {
				t.Errorf("isConfigExempt(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func splitArgs(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
