package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestParseConfigDockerfile(t *testing.T) {
	const base = `
type: project
oro_version: "6.1"
domains:
  - host: example.com
`

	t.Run("absent means the published image", func(t *testing.T) {
		conf, err := ParseConfig([]byte(base))
		if err != nil {
			t.Fatalf("ParseConfig failed: %v", err)
		}
		if conf.Dockerfile != "" {
			t.Errorf("expected no dockerfile, got %q", conf.Dockerfile)
		}
	})

	t.Run("path is read", func(t *testing.T) {
		conf, err := ParseConfig([]byte(base + "dockerfile: docker/Dockerfile\n"))
		if err != nil {
			t.Fatalf("ParseConfig failed: %v", err)
		}
		if conf.Dockerfile != "docker/Dockerfile" {
			t.Errorf("expected docker/Dockerfile, got %q", conf.Dockerfile)
		}
	})
}

// The path is joined onto the project directory and its parent becomes a Docker build context,
// so a path that escapes the project has to be refused while the config is being read.
func TestValidateDockerfilePath(t *testing.T) {
	newConf := func(path string) OroConfig {
		return OroConfig{
			Type:       InstallTypeProject,
			OroVersion: "6.1",
			Domains:    []DomainConfig{{Host: "example.com"}},
			Dockerfile: path,
		}
	}

	valid := []string{"", "Dockerfile", "docker/Dockerfile", "./docker/Dockerfile", "docker/oro/Dockerfile.dev"}
	for _, path := range valid {
		conf := newConf(path)
		if err := conf.Validate(); err != nil {
			t.Errorf("expected %q to be accepted, got %v", path, err)
		}
	}

	invalid := []string{"/etc/Dockerfile", "../other-project/Dockerfile", "..", "."}
	for _, path := range invalid {
		conf := newConf(path)
		err := conf.Validate()
		if err == nil {
			t.Errorf("expected %q to be rejected", path)
			continue
		}
		if !strings.Contains(err.Error(), "dockerfile") {
			t.Errorf("expected the error for %q to name the field, got %v", path, err)
		}
	}
}

func TestGetDockerfileNormalizesThePath(t *testing.T) {
	cases := map[string]string{
		"docker/Dockerfile":   "docker/Dockerfile",
		"./docker/Dockerfile": "docker/Dockerfile",
		"docker//Dockerfile":  "docker/Dockerfile",
		"  Dockerfile  ":      "Dockerfile",
		"":                    "",
	}
	for in, want := range cases {
		viper.Reset()
		viper.Set("dockerfile", in)
		if got := GetDockerfile(); got != want {
			t.Errorf("GetDockerfile() with %q = %q, want %q", in, got, want)
		}
	}
	viper.Reset()
}

// The absolute path resolves against the directory holding .orobox.yaml, not the working
// directory: every orobox command has to find the same Dockerfile from anywhere in the project.
func TestGetDockerfilePathResolvesAgainstTheConfigDirectory(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".orobox.yaml")
	if err := os.WriteFile(configPath, []byte("type: project\noro_version: \"6.1\"\ndockerfile: docker/Dockerfile\n"), 0644); err != nil {
		t.Fatal(err)
	}

	viper.Reset()
	defer viper.Reset()
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(dir, "docker", "Dockerfile")
	if got := GetDockerfilePath(); got != want {
		t.Errorf("GetDockerfilePath() = %q, want %q", got, want)
	}
}

func TestGetDockerfilePathUnset(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	if got := GetDockerfilePath(); got != "" {
		t.Errorf("expected an empty path, got %q", got)
	}
}

// `orobox deploy-init` reads the config through viper.Unmarshal and rewrites the whole file
// through SaveConfig. A missing mapstructure tag would drop the key on that round-trip and
// silently put the project back on the published image.
func TestSaveConfigRoundTripsDockerfile(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	viper.SetConfigType("yaml")
	source := "type: project\noro_version: \"6.1\"\ndockerfile: docker/Dockerfile\n"
	if err := viper.ReadConfig(strings.NewReader(source)); err != nil {
		t.Fatal(err)
	}

	var conf OroConfig
	if err := viper.Unmarshal(&conf); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if conf.Dockerfile != "docker/Dockerfile" {
		t.Fatalf("Unmarshal lost the path: %q", conf.Dockerfile)
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
	if got := GetDockerfile(); got != "docker/Dockerfile" {
		t.Errorf("expected docker/Dockerfile after the round-trip, got %q", got)
	}
}

// A project that never mentioned the key must not gain an empty one: an `orobox deploy-init`
// rewrite would otherwise start writing noise into every config file it touches.
func TestSaveConfigOmitsUnsetDockerfile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".orobox.yaml")
	conf := OroConfig{Type: InstallTypeProject, OroVersion: "6.1"}
	if err := SaveConfig(configPath, &conf); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "dockerfile") {
		t.Errorf("expected the key to be omitted when unset, got:\n%s", data)
	}
}
