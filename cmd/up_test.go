package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"
	"github.com/spf13/viper"
)

// upComposeCalls records the arguments of every RunComposeCommandSilently call made by the
// last runUpCaptured, so a test can assert on how `up` invoked compose.
var upComposeCalls [][]string

// runUpCaptured runs `orobox up` against a mocked compose layer and returns what it printed.
func runUpCaptured(t *testing.T, settings map[string]any) string {
	t.Helper()

	oldRun := docker.RunComposeCommand
	oldSilently := docker.RunComposeCommandSilently
	oldWithOutput := docker.RunComposeCommandWithOutput
	t.Cleanup(func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandSilently = oldSilently
		docker.RunComposeCommandWithOutput = oldWithOutput
		rootCmd.SetArgs(nil)
		resetGlobalFlags(t)
		docker.ResetEnsuredServices()
		for key := range settings {
			viper.Set(key, nil)
		}
		viper.Set("type", nil)
	})

	docker.RunComposeCommand = func(string, ...string) error { return nil }
	upComposeCalls = nil
	docker.RunComposeCommandSilently = func(_ string, args ...string) error {
		upComposeCalls = append(upComposeCalls, args)
		return nil
	}
	docker.RunComposeCommandWithOutput = func(a ...string) ([]byte, error) {
		if len(a) > 0 && a[0] == "ps" {
			return psRunningRequested(a), nil
		}
		return []byte("[]"), nil
	}

	viper.Set("type", "project")
	for key, value := range settings {
		viper.Set(key, value)
	}

	// utils holds the os.Stdout it saw at start-up, so redirecting the file descriptor would
	// miss everything up prints; swap its writer instead.
	var printed bytes.Buffer
	restore := utils.SetWriter(&printed)
	defer restore()

	rootCmd.SetArgs([]string{"up"})
	if err := rootCmd.Execute(); err != nil {
		t.Errorf("rootCmd.Execute() failed: %v", err)
	}
	return printed.String()
}

func TestUpAdvertisesConfiguredPorts(t *testing.T) {
	out := runUpCaptured(t, map[string]any{
		"services.adminer":       true,
		"services.mailpit":       true,
		"services.redis":         true,
		"services.rabbitmq":      true,
		"services.elasticsearch": true,
		"ports.adminer":          8090,
		"ports.db":               5434,
		"ports.mail_ui":          8026,
		"ports.redisinsight":     8002,
		"ports.rabbitmq_ui":      15673,
		"ports.kibana":           5602,
	})

	for _, want := range []string{
		"http://localhost:8090",
		"Port: 5434",
		"http://localhost:8026",
		"http://localhost:8002",
		"http://localhost:15673",
		"http://localhost:5602",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("up output should contain %q\n---\n%s", want, out)
		}
	}
	for _, stale := range []string{"localhost:8081", "Port: 5432", "localhost:8025", "localhost:8001"} {
		if strings.Contains(out, stale) {
			t.Errorf("up output must not advertise the default %q once it is overridden\n---\n%s", stale, out)
		}
	}
}

func TestUpAdvertisesDefaultPorts(t *testing.T) {
	out := runUpCaptured(t, map[string]any{"services.adminer": true, "services.mailpit": true})
	for _, want := range []string{"http://localhost:8081", "Port: 5432", "http://localhost:8025"} {
		if !strings.Contains(out, want) {
			t.Errorf("up output should contain %q\n---\n%s", want, out)
		}
	}
}

func TestUpSkipsURLOfUnpublishedPort(t *testing.T) {
	out := runUpCaptured(t, map[string]any{
		"services.adminer": true,
		"services.mailpit": true,
		"ports.adminer":    0,
		"ports.mail_ui":    0,
		"ports.db":         0,
	})
	for _, bad := range []string{"localhost:0", "localhost:8081", "localhost:8025", "Port: 0", "Port: 5432", "Adminer", "External Database Connection"} {
		if strings.Contains(out, bad) {
			t.Errorf("up output must not print %q for an unpublished port\n---\n%s", bad, out)
		}
	}
}

// upArgs returns the arguments of the `up` call recorded by runUpCaptured.
func upArgs(t *testing.T) []string {
	t.Helper()
	for _, args := range upComposeCalls {
		if len(args) > 0 && args[0] == "up" {
			return args
		}
	}
	t.Fatalf("no `up` call was made, got %v", upComposeCalls)
	return nil
}

func TestUpPassesBuildWhenOverrideHasBuild(t *testing.T) {
	for _, tc := range []struct {
		name     string
		override string
		want     bool
	}{
		{"with build", "services:\n  docs:\n    build: ./docs\n", true},
		{"without build", "services:\n  minio:\n    image: minio/minio\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			t.Chdir(project)
			t.Setenv("OROBOX_LOCAL_CONFIG", "1")
			if err := os.WriteFile(filepath.Join(project, ".orobox.compose.yaml"), []byte(tc.override), 0644); err != nil {
				t.Fatal(err)
			}

			runUpCaptured(t, nil)

			if got := slices.Contains(upArgs(t), "--build"); got != tc.want {
				t.Errorf("--build passed = %v, want %v (args %v)", got, tc.want, upArgs(t))
			}
		})
	}
}

// A project service that advertises itself with the dev.orobox.url label is listed after the
// built-in blocks, so the user finds the MinIO console without reading their own override.
func TestUpPrintsProjectServiceURLs(t *testing.T) {
	project := t.TempDir()
	t.Chdir(project)
	t.Setenv("OROBOX_LOCAL_CONFIG", "1")
	override := "services:\n  minio:\n    image: minio/minio\n    labels:\n      dev.orobox.url: http://localhost:9001\n"
	if err := os.WriteFile(filepath.Join(project, ".orobox.compose.yaml"), []byte(override), 0644); err != nil {
		t.Fatal(err)
	}

	out := runUpCaptured(t, nil)

	for _, want := range []string{"Project services", "minio: http://localhost:9001"} {
		if !strings.Contains(out, want) {
			t.Errorf("up output should contain %q\n---\n%s", want, out)
		}
	}
}

func TestUpOmitsProjectServicesWithoutURLLabels(t *testing.T) {
	project := t.TempDir()
	t.Chdir(project)
	t.Setenv("OROBOX_LOCAL_CONFIG", "1")
	override := "services:\n  minio:\n    image: minio/minio\n"
	if err := os.WriteFile(filepath.Join(project, ".orobox.compose.yaml"), []byte(override), 0644); err != nil {
		t.Fatal(err)
	}

	out := runUpCaptured(t, nil)

	if strings.Contains(out, "Project services") {
		t.Errorf("up output must not print an empty Project services block\n---\n%s", out)
	}
}

// up advertises what compose really runs: Adminer is on unless disabled, RedisInsight follows
// Redis and Kibana follows Elasticsearch unless set explicitly, and the database connection
// does not depend on Adminer being enabled.
func TestUpOutputFollowsTheServicesComposeRuns(t *testing.T) {
	out := runUpCaptured(t, map[string]any{
		"services.redis":         true,
		"services.redisinsight":  false,
		"services.elasticsearch": true,
		"services.kibana":        false,
		"services.adminer":       false,
	})
	if strings.Contains(out, "RedisInsight") {
		t.Errorf("RedisInsight is disabled and must not be advertised:\n%s", out)
	}
	if strings.Contains(out, "Kibana") {
		t.Errorf("Kibana is disabled and must not be advertised:\n%s", out)
	}
	if strings.Contains(out, "Adminer") {
		t.Errorf("Adminer is disabled and must not be advertised:\n%s", out)
	}
	if !strings.Contains(out, "External Database Connection") || !strings.Contains(out, "Port: 5432") {
		t.Errorf("the database connection must be printed without Adminer:\n%s", out)
	}
}

func TestUpAdvertisesAdminerByDefault(t *testing.T) {
	out := runUpCaptured(t, map[string]any{})
	if !strings.Contains(out, "Adminer is available at:") {
		t.Errorf("Adminer runs by default and must be advertised:\n%s", out)
	}
}
