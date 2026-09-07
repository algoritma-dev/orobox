package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
)

func TestXdebugEnabledParsesOnOff(t *testing.T) {
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	t.Cleanup(func() { docker.RunComposeCommandWithOutput = oldRunWithOutput })

	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		if contains(args, "application") {
			return []byte("on\n"), nil
		}
		return []byte("off\n"), nil
	}

	enabled, err := xdebugEnabled("application")
	if err != nil || !enabled {
		t.Errorf("xdebugEnabled(application) = (%v, %v), want (true, nil)", enabled, err)
	}
	enabled, err = xdebugEnabled("cron")
	if err != nil || enabled {
		t.Errorf("xdebugEnabled(cron) = (%v, %v), want (false, nil)", enabled, err)
	}
}

func TestXdebugEnabledPropagatesError(t *testing.T) {
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	t.Cleanup(func() { docker.RunComposeCommandWithOutput = oldRunWithOutput })

	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		return nil, errors.New("service not running")
	}

	if _, err := xdebugEnabled("application"); err == nil {
		t.Fatal("xdebugEnabled() error = nil, want error when compose exec fails")
	}
}

func TestShowXdebugStatusJSONPrintsAllFourServices(t *testing.T) {
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	t.Cleanup(func() { docker.RunComposeCommandWithOutput = oldRunWithOutput })

	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		if contains(args, "application") || contains(args, "php-fpm-app") {
			return []byte("on\n"), nil
		}
		return []byte("off\n"), nil
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := showXdebugStatusJSON()
	w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("showXdebugStatusJSON() error = %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)

	var got map[string]bool
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output %q is not valid JSON: %v", buf.String(), err)
	}

	want := map[string]bool{"application": true, "php-fpm-app": true, "consumer": false, "cron": false}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("json[%q] = %v, want %v (full output: %v)", k, got[k], v, got)
		}
	}
}

func TestShowXdebugStatusJSONPropagatesError(t *testing.T) {
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	t.Cleanup(func() { docker.RunComposeCommandWithOutput = oldRunWithOutput })

	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		return nil, errors.New("service not running")
	}

	if err := showXdebugStatusJSON(); err == nil {
		t.Fatal("showXdebugStatusJSON() error = nil, want error when a service query fails")
	}
}
