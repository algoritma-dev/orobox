// Package project discovers and describes orobox environments as plain values, with no
// dependency on viper or the current working directory, so a single process (the CLI or the
// tray) can hold many of them at once.
package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// CurrentSchema is the schema version written into project.json. Bump it, and branch on the
// value read back in ReadMarker, whenever a future field needs a migration.
const CurrentSchema = 1

// markerFilename is the file orobox writes into a project's internal directory to make it
// self-describing: the discovery in project.go reads it back to recover the host path an
// internal directory belongs to, since the directory itself only carries the project name.
const markerFilename = "project.json"

// Marker is the content of project.json.
type Marker struct {
	Schema        int       `json:"schema"`
	Name          string    `json:"name"`
	HostPath      string    `json:"host_path"`
	Type          string    `json:"type"`
	OroVersion    string    `json:"oro_version"`
	OroboxVersion string    `json:"orobox_version"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// WriteMarker writes (or idempotently rewrites) project.json into internalDir.
func WriteMarker(internalDir string, m Marker) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(internalDir, markerFilename), data, 0644)
}

// ReadMarker reads project.json from internalDir.
func ReadMarker(internalDir string) (Marker, error) {
	data, err := os.ReadFile(filepath.Join(internalDir, markerFilename))
	if err != nil {
		return Marker{}, err
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return Marker{}, err
	}
	return m, nil
}
