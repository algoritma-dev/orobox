package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestValidatePorts(t *testing.T) {
	t.Run("unknown key names the key and lists the valid ones", func(t *testing.T) {
		err := validatePorts(map[string]int{"dbb": 1})
		if err == nil {
			t.Fatal("expected an error for an unknown key")
		}
		for _, want := range []string{"dbb", "db_test", "mail_smtp", "gotenberg"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should mention %q", err, want)
			}
		}
	})
	t.Run("above 65535", func(t *testing.T) {
		if err := validatePorts(map[string]int{"db": 70000}); err == nil {
			t.Error("expected an error for 70000")
		}
	})
	t.Run("negative", func(t *testing.T) {
		if err := validatePorts(map[string]int{"db": -1}); err == nil {
			t.Error("expected an error for -1")
		}
	})
	t.Run("zero means unpublished", func(t *testing.T) {
		if err := validatePorts(map[string]int{"db": 0}); err != nil {
			t.Errorf("0 must be valid, got %v", err)
		}
	})
	t.Run("web ports cannot be unpublished", func(t *testing.T) {
		for _, key := range []string{"http", "https"} {
			err := validatePorts(map[string]int{key: 0})
			if err == nil {
				t.Errorf("ports.%s: 0 must be rejected", key)
				continue
			}
			if !strings.Contains(err.Error(), "ports."+key) || !strings.Contains(err.Error(), "entry point") {
				t.Errorf("error %q should name ports.%s and explain the entry point", err, key)
			}
		}
		if err := validatePorts(map[string]int{"http": 8090, "https": 8453}); err != nil {
			t.Errorf("non-zero web ports must be valid, got %v", err)
		}
	})
	t.Run("nil map", func(t *testing.T) {
		if err := validatePorts(nil); err != nil {
			t.Errorf("no ports section must be valid, got %v", err)
		}
	})
}

func TestValidateRejectsUnknownPortKey(t *testing.T) {
	c, err := ParseConfig([]byte(phpIniBase + "ports:\n  dbb: 5434\n"))
	if err != nil {
		t.Fatal(err)
	}
	err = c.Validate()
	if err == nil || !strings.Contains(err.Error(), "dbb") {
		t.Errorf("Validate should reject the typo'd key, got %v", err)
	}
}

func TestGetPortsDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	if got := GetPorts(); !reflect.DeepEqual(got, DefaultPorts) {
		t.Errorf("GetPorts() = %v, want the defaults %v", got, DefaultPorts)
	}
	if got := len(DefaultPorts); got != 14 {
		t.Errorf("DefaultPorts has %d keys, want 14", got)
	}
}

func TestGetPortsOverlaysConfig(t *testing.T) {
	loadProject(t, phpIniBase+"ports:\n  db: 5434\n  adminer: 0\n", nil)

	if got := GetPort("db"); got != 5434 {
		t.Errorf("db = %d, want 5434", got)
	}
	if got := GetPort("adminer"); got != 0 {
		t.Errorf("adminer = %d, want 0 (an explicit 0 must not fall back to the default)", got)
	}
	if got := GetPort("redis"); got != 6379 {
		t.Errorf("redis = %d, want the default 6379", got)
	}
}

func TestGetPortsDoesNotMutateDefaults(t *testing.T) {
	loadProject(t, phpIniBase+"ports:\n  db: 5434\n", nil)
	GetPorts()["db"] = 1
	if DefaultPorts["db"] != 5432 {
		t.Errorf("DefaultPorts was mutated: db = %d", DefaultPorts["db"])
	}
}
