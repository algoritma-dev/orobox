package tray

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/algoritma-dev/orobox/internal/project"
)

func TestPollAllReturnsOneViewPerDiscoveredProject(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("OROBOX_LOCAL_CONFIG", "")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	hostPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostPath, ".orobox.yaml"), []byte("type: bundle\nnamespace: Acme/Bundle\noro_version: \"6.1\"\ndomains:\n  - host: oro.demo\n    root: public\n    ssl: false\n"), 0644); err != nil {
		t.Fatalf("write .orobox.yaml: %v", err)
	}

	internalDir := filepath.Join(configHome, "orobox", "mybundle")
	if err := os.MkdirAll(internalDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(internalDir, "docker-compose.yml"), []byte("services: {}\n"), 0644); err != nil {
		t.Fatalf("write docker-compose.yml: %v", err)
	}
	if err := project.WriteMarker(internalDir, project.Marker{Schema: project.CurrentSchema, Name: "mybundle", HostPath: hostPath}); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	views, err := PollAll()
	if err != nil {
		t.Fatalf("PollAll() error = %v", err)
	}
	if len(views) != 1 || views[0].Project.Name != "mybundle" {
		t.Errorf("PollAll() = %+v, want one view for mybundle", views)
	}
}

func TestPollAllEmptyRegistryReturnsEmpty(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("OROBOX_LOCAL_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	views, err := PollAll()
	if err != nil {
		t.Fatalf("PollAll() error = %v", err)
	}
	if len(views) != 0 {
		t.Errorf("PollAll() = %+v, want empty", views)
	}
}
