package project

import (
	"testing"
	"time"
)

func TestWriteMarkerThenReadMarkerRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := Marker{
		Schema:        CurrentSchema,
		Name:          "mybundle",
		HostPath:      "/home/user/projects/mybundle",
		Type:          "bundle",
		OroVersion:    "6.1",
		OroboxVersion: "1.0.0-rc7",
		UpdatedAt:     time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC),
	}

	if err := WriteMarker(dir, want); err != nil {
		t.Fatalf("WriteMarker() error = %v", err)
	}

	got, err := ReadMarker(dir)
	if err != nil {
		t.Fatalf("ReadMarker() error = %v", err)
	}
	if got != want {
		t.Errorf("ReadMarker() = %+v, want %+v", got, want)
	}
}

func TestReadMarkerMissingFileReturnsError(t *testing.T) {
	dir := t.TempDir()

	if _, err := ReadMarker(dir); err == nil {
		t.Fatal("ReadMarker() error = nil, want error for missing project.json")
	}
}

func TestWriteMarkerOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	first := Marker{Schema: CurrentSchema, Name: "a", HostPath: "/a", Type: "bundle", OroVersion: "6.1", OroboxVersion: "1.0.0-rc7", UpdatedAt: time.Unix(0, 0).UTC()}
	second := Marker{Schema: CurrentSchema, Name: "b", HostPath: "/b", Type: "project", OroVersion: "7.0", OroboxVersion: "1.0.0-rc8", UpdatedAt: time.Unix(100, 0).UTC()}

	if err := WriteMarker(dir, first); err != nil {
		t.Fatalf("WriteMarker(first) error = %v", err)
	}
	if err := WriteMarker(dir, second); err != nil {
		t.Fatalf("WriteMarker(second) error = %v", err)
	}

	got, err := ReadMarker(dir)
	if err != nil {
		t.Fatalf("ReadMarker() error = %v", err)
	}
	if got != second {
		t.Errorf("ReadMarker() = %+v, want %+v (second write should win)", got, second)
	}
}
