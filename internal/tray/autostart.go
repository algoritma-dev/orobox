package tray

import (
	"os"
	"path/filepath"
)

// installedDesktopFile is where nfpm installs orobox-tray's .desktop entry (see
// .goreleaser.yaml). Autostart works by symlinking to this fixed path, so it only functions on
// a package install — the one way this ships.
const installedDesktopFile = "/usr/share/applications/orobox-tray.desktop"

const autostartFilename = "orobox-tray.desktop"

// IsAutostartEnabledIn reports whether the autostart symlink exists under configHome
// (~/.config in production; a temp dir in tests).
func IsAutostartEnabledIn(configHome string) bool {
	_, err := os.Lstat(filepath.Join(configHome, "autostart", autostartFilename))
	return err == nil
}

// EnableAutostartIn (re)creates the autostart symlink, idempotently.
func EnableAutostartIn(configHome string) error {
	dir := filepath.Join(configHome, "autostart")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	link := filepath.Join(dir, autostartFilename)
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(installedDesktopFile, link)
}

// DisableAutostartIn removes the autostart symlink. Not being enabled in the first place is not
// an error — "Start at login" being unchecked is a valid, common state.
func DisableAutostartIn(configHome string) error {
	err := os.Remove(filepath.Join(configHome, "autostart", autostartFilename))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// userConfigHome is ~/.config, the base IsAutostartEnabled/EnableAutostart/DisableAutostart
// resolve against in production.
func userConfigHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// IsAutostartEnabled reports whether orobox-tray is set to start at login.
func IsAutostartEnabled() bool {
	configHome, err := userConfigHome()
	if err != nil {
		return false
	}
	return IsAutostartEnabledIn(configHome)
}

// EnableAutostart turns "Start at login" on.
func EnableAutostart() error {
	configHome, err := userConfigHome()
	if err != nil {
		return err
	}
	return EnableAutostartIn(configHome)
}

// DisableAutostart turns "Start at login" off.
func DisableAutostart() error {
	configHome, err := userConfigHome()
	if err != nil {
		return err
	}
	return DisableAutostartIn(configHome)
}
