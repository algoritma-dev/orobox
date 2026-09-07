package project

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// setRegistry points os.UserConfigDir() at a fresh temp dir and disables the CI/
// OROBOX_LOCAL_CONFIG override, so internalDirFor and Discover behave the same regardless of
// the environment actually running the test.
func setRegistry(t *testing.T) string {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("OROBOX_LOCAL_CONFIG", "")
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	return base
}

func writeComposeWithBindMount(t *testing.T, internalDir, bundlePath string) {
	t.Helper()
	content := fmt.Sprintf(`services:
  application:
    volumes-oro: &volumes-oro
      - "%s:/var/www/oro:cached"
      - "cache:/var/www/oro/var/cache:delegated"
    volumes: *volumes-oro
`, bundlePath)
	if err := os.WriteFile(filepath.Join(internalDir, "docker-compose.yml"), []byte(content), 0644); err != nil {
		t.Fatalf("writeComposeWithBindMount: %v", err)
	}
}

func writeOroboxYaml(t *testing.T, hostPath string) {
	t.Helper()
	content := "type: bundle\nnamespace: Acme/Bundle\noro_version: \"6.1\"\ndomains:\n  - host: oro.demo\n    root: public\n    ssl: false\n"
	if err := os.WriteFile(filepath.Join(hostPath, ".orobox.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("writeOroboxYaml: %v", err)
	}
}

func TestDiscoverReturnsEmptyWhenRegistryMissing(t *testing.T) {
	setRegistry(t)
	// note: registry base itself ("<XDG_CONFIG_HOME>/orobox") is never created

	got, err := Discover()

	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Discover() = %+v, want empty", got)
	}
}

func TestDiscoverFindsValidProject(t *testing.T) {
	configHome := setRegistry(t)
	hostPath := t.TempDir()
	writeOroboxYaml(t, hostPath)

	internalDir := filepath.Join(configHome, "orobox", "mybundle")
	mustMkdirAll(t, internalDir)
	writeComposeWithBindMount(t, internalDir, hostPath)
	mustWriteMarker(t, internalDir, Marker{Schema: CurrentSchema, Name: "mybundle", HostPath: hostPath, Type: "bundle", OroVersion: "6.1", OroboxVersion: "1.0.0-rc7"})

	got, err := Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Discover() = %+v, want 1 project", got)
	}

	p := got[0]
	if p.Name != "mybundle" || p.HostPath != hostPath || p.InternalDir != internalDir {
		t.Errorf("Discover()[0] = %+v", p)
	}
	if p.Stale || p.Conflict {
		t.Errorf("Discover()[0] Stale=%v Conflict=%v, want both false", p.Stale, p.Conflict)
	}
	if p.Config == nil || p.Config.OroVersion != "6.1" {
		t.Errorf("Discover()[0].Config = %+v, want parsed config with oro_version 6.1", p.Config)
	}
}

func TestDiscoverMarksStaleWhenHostPathGone(t *testing.T) {
	configHome := setRegistry(t)
	goneHostPath := filepath.Join(t.TempDir(), "no-longer-there")

	internalDir := filepath.Join(configHome, "orobox", "ghost")
	mustMkdirAll(t, internalDir)
	writeComposeWithBindMount(t, internalDir, goneHostPath)
	mustWriteMarker(t, internalDir, Marker{Schema: CurrentSchema, Name: "ghost", HostPath: goneHostPath})

	got, err := Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got) != 1 || !got[0].Stale {
		t.Errorf("Discover() = %+v, want single Stale project", got)
	}
}

func TestDiscoverFallsBackToBindMountWhenMarkerMissing(t *testing.T) {
	configHome := setRegistry(t)
	hostPath := t.TempDir()
	writeOroboxYaml(t, hostPath)

	internalDir := filepath.Join(configHome, "orobox", "fallback")
	mustMkdirAll(t, internalDir)
	writeComposeWithBindMount(t, internalDir, hostPath)
	// deliberately no project.json

	got, err := Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Discover() = %+v, want 1 project", got)
	}
	if got[0].HostPath != hostPath {
		t.Errorf("Discover()[0].HostPath = %q, want %q (from bind mount fallback)", got[0].HostPath, hostPath)
	}
}

func TestDiscoverDetectsConflictBetweenMarkerAndBindMount(t *testing.T) {
	configHome := setRegistry(t)
	markerHostPath := t.TempDir()
	bindMountHostPath := t.TempDir()

	internalDir := filepath.Join(configHome, "orobox", "clashing")
	mustMkdirAll(t, internalDir)
	writeComposeWithBindMount(t, internalDir, bindMountHostPath)
	mustWriteMarker(t, internalDir, Marker{Schema: CurrentSchema, Name: "clashing", HostPath: markerHostPath})

	got, err := Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got) != 1 || !got[0].Conflict || got[0].ConflictHostPath != bindMountHostPath {
		t.Errorf("Discover() = %+v, want Conflict=true ConflictHostPath=%q", got, bindMountHostPath)
	}
}

func TestDiscoverKeepsProjectWithConfigErrorWhenOroboxYamlMissing(t *testing.T) {
	configHome := setRegistry(t)
	hostPath := t.TempDir()
	// deliberately no .orobox.yaml at hostPath

	internalDir := filepath.Join(configHome, "orobox", "noconfig")
	mustMkdirAll(t, internalDir)
	writeComposeWithBindMount(t, internalDir, hostPath)
	mustWriteMarker(t, internalDir, Marker{Schema: CurrentSchema, Name: "noconfig", HostPath: hostPath})

	got, err := Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Discover() = %+v, want 1 project", got)
	}
	if got[0].Config != nil || got[0].ConfigError == nil {
		t.Errorf("Discover()[0] = %+v, want nil Config and a ConfigError", got[0])
	}
}

func TestDiscoverSortsByName(t *testing.T) {
	configHome := setRegistry(t)
	for _, name := range []string{"zeta", "alpha", "mid"} {
		hostPath := t.TempDir()
		writeOroboxYaml(t, hostPath)
		internalDir := filepath.Join(configHome, "orobox", name)
		mustMkdirAll(t, internalDir)
		writeComposeWithBindMount(t, internalDir, hostPath)
		mustWriteMarker(t, internalDir, Marker{Schema: CurrentSchema, Name: name, HostPath: hostPath})
	}

	got, err := Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got) != 3 || got[0].Name != "alpha" || got[1].Name != "mid" || got[2].Name != "zeta" {
		names := make([]string, len(got))
		for i, p := range got {
			names[i] = p.Name
		}
		t.Errorf("Discover() names = %v, want [alpha mid zeta]", names)
	}
}

func TestLoadReturnsProjectForKnownHostPath(t *testing.T) {
	configHome := setRegistry(t)
	hostPath := t.TempDir()
	writeOroboxYaml(t, hostPath)

	got, err := Load(hostPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	wantInternalDir := filepath.Join(configHome, "orobox", filepath.Base(hostPath))
	if got.Name != filepath.Base(hostPath) || got.InternalDir != wantInternalDir {
		t.Errorf("Load() = %+v, want Name=%q InternalDir=%q", got, filepath.Base(hostPath), wantInternalDir)
	}
	if got.Config == nil {
		t.Errorf("Load().Config = nil, want parsed config")
	}
}

func TestLoadErrorsWhenOroboxYamlMissing(t *testing.T) {
	setRegistry(t)
	hostPath := t.TempDir()

	if _, err := Load(hostPath); err == nil {
		t.Fatal("Load() error = nil, want error for missing .orobox.yaml")
	}
}

func TestLoadErrorsWhenHostPathMissing(t *testing.T) {
	setRegistry(t)

	if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("Load() error = nil, want error for missing host path")
	}
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
}

func mustWriteMarker(t *testing.T, internalDir string, m Marker) {
	t.Helper()
	if err := WriteMarker(internalDir, m); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
}
