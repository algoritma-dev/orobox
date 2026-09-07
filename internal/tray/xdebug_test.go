package tray

import (
	"errors"
	"testing"
)

func TestXdebugStatusParsesJSONOutput(t *testing.T) {
	orig := runXdebugStatus
	defer func() { runXdebugStatus = orig }()
	runXdebugStatus = func(oroboxBin, hostPath string) ([]byte, error) {
		if oroboxBin != "/opt/orobox" || hostPath != "/home/user/mybundle" {
			t.Errorf("runXdebugStatus called with (%q, %q)", oroboxBin, hostPath)
		}
		return []byte(`{"application":true,"php-fpm-app":true,"consumer":false,"cron":false}`), nil
	}

	got, err := XdebugStatus("/opt/orobox", "/home/user/mybundle")
	if err != nil {
		t.Fatalf("XdebugStatus() error = %v", err)
	}
	if !got["application"] || got["consumer"] {
		t.Errorf("XdebugStatus() = %v, want application=true consumer=false", got)
	}
}

func TestXdebugStatusPropagatesRunnerError(t *testing.T) {
	orig := runXdebugStatus
	defer func() { runXdebugStatus = orig }()
	runXdebugStatus = func(oroboxBin, hostPath string) ([]byte, error) {
		return nil, errors.New("exit status 1")
	}

	if _, err := XdebugStatus("/opt/orobox", "/home/user/mybundle"); err == nil {
		t.Fatal("XdebugStatus() error = nil, want error when the CLI call fails")
	}
}

func TestXdebugCheckedReflectsApplicationKey(t *testing.T) {
	if !XdebugChecked(map[string]bool{"application": true}) {
		t.Error("XdebugChecked() = false, want true when application is enabled")
	}
	if XdebugChecked(map[string]bool{"application": false}) {
		t.Error("XdebugChecked() = true, want false when application is disabled")
	}
	if XdebugChecked(nil) {
		t.Error("XdebugChecked(nil) = true, want false")
	}
}
