package tray

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsAutostartEnabledInFalseWhenNoSymlink(t *testing.T) {
	dir := t.TempDir()
	if IsAutostartEnabledIn(dir) {
		t.Error("IsAutostartEnabledIn() = true, want false when nothing has been created")
	}
}

func TestEnableAutostartInCreatesSymlinkToInstalledDesktopFile(t *testing.T) {
	dir := t.TempDir()

	if err := EnableAutostartIn(dir); err != nil {
		t.Fatalf("EnableAutostartIn() error = %v", err)
	}

	if !IsAutostartEnabledIn(dir) {
		t.Error("IsAutostartEnabledIn() = false, want true after EnableAutostartIn")
	}

	link := filepath.Join(dir, "autostart", "orobox-tray.desktop")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink(%s) error = %v", link, err)
	}
	if target != installedDesktopFile {
		t.Errorf("symlink target = %q, want %q", target, installedDesktopFile)
	}
}

func TestEnableAutostartInIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	if err := EnableAutostartIn(dir); err != nil {
		t.Fatalf("EnableAutostartIn() first call error = %v", err)
	}
	if err := EnableAutostartIn(dir); err != nil {
		t.Fatalf("EnableAutostartIn() second call error = %v", err)
	}
	if !IsAutostartEnabledIn(dir) {
		t.Error("IsAutostartEnabledIn() = false after two EnableAutostartIn calls, want true")
	}
}

func TestDisableAutostartInRemovesSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := EnableAutostartIn(dir); err != nil {
		t.Fatalf("EnableAutostartIn() error = %v", err)
	}

	if err := DisableAutostartIn(dir); err != nil {
		t.Fatalf("DisableAutostartIn() error = %v", err)
	}
	if IsAutostartEnabledIn(dir) {
		t.Error("IsAutostartEnabledIn() = true after DisableAutostartIn, want false")
	}
}

func TestDisableAutostartInWhenNeverEnabledIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	if err := DisableAutostartIn(dir); err != nil {
		t.Fatalf("DisableAutostartIn() on a never-enabled dir error = %v, want nil", err)
	}
}
