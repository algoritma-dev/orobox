package tray

import (
	"errors"
	"testing"
)

func TestParseVersionOutputExtractsVersionToken(t *testing.T) {
	got, err := ParseVersionOutput("orobox version 1.0.0-rc30\n")
	if err != nil {
		t.Fatalf("ParseVersionOutput() error = %v", err)
	}
	if got != "1.0.0-rc30" {
		t.Errorf("ParseVersionOutput() = %q, want %q", got, "1.0.0-rc30")
	}
}

func TestParseVersionOutputErrorsOnEmpty(t *testing.T) {
	if _, err := ParseVersionOutput(""); err == nil {
		t.Fatal("ParseVersionOutput(\"\") error = nil, want error")
	}
}

func TestMinorVersion(t *testing.T) {
	tests := []struct{ in, want string }{
		{"1.0.0-rc30", "1.0"},
		{"1.0.0", "1.0"},
		{"2.3.7-rc1", "2.3"},
		{"1", "1"},
	}
	for _, tt := range tests {
		if got := MinorVersion(tt.in); got != tt.want {
			t.Errorf("MinorVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCheckVersionMismatchSameMinorNoWarning(t *testing.T) {
	warning, mismatched := CheckVersionMismatch("1.0.0-rc30", "1.0.0-rc28")
	if mismatched || warning != "" {
		t.Errorf("CheckVersionMismatch() = (%q, %v), want (\"\", false)", warning, mismatched)
	}
}

func TestCheckVersionMismatchDifferentMinorWarns(t *testing.T) {
	warning, mismatched := CheckVersionMismatch("1.1.0", "1.0.0-rc30")
	if !mismatched || warning == "" {
		t.Errorf("CheckVersionMismatch() = (%q, %v), want a non-empty warning and true", warning, mismatched)
	}
}

func TestFetchCLIVersionParsesRunnerOutput(t *testing.T) {
	orig := runVersionCommand
	defer func() { runVersionCommand = orig }()
	runVersionCommand = func(bin string) (string, error) {
		if bin != "/path/to/orobox" {
			t.Errorf("runVersionCommand called with %q, want /path/to/orobox", bin)
		}
		return "orobox version 1.2.3\n", nil
	}

	got, err := FetchCLIVersion("/path/to/orobox")
	if err != nil {
		t.Fatalf("FetchCLIVersion() error = %v", err)
	}
	if got != "1.2.3" {
		t.Errorf("FetchCLIVersion() = %q, want %q", got, "1.2.3")
	}
}

func TestFetchCLIVersionPropagatesRunnerError(t *testing.T) {
	orig := runVersionCommand
	defer func() { runVersionCommand = orig }()
	runVersionCommand = func(bin string) (string, error) {
		return "", errors.New("exec failed")
	}

	if _, err := FetchCLIVersion("/path/to/orobox"); err == nil {
		t.Fatal("FetchCLIVersion() error = nil, want error")
	}
}
