package tray

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ParseVersionOutput extracts the version token from `orobox --version`, which cobra prints as
// "orobox version X.Y.Z\n".
func ParseVersionOutput(output string) (string, error) {
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return "", errors.New("empty version output")
	}
	return fields[len(fields)-1], nil
}

// MinorVersion reduces a version string to its "major.minor" prefix, dropping the patch and
// any -rcN suffix, since self-update only ever moves the CLI, never the tray: patch-level
// drift between the two is expected and not worth warning about.
func MinorVersion(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return parts[0]
	}
	return parts[0] + "." + parts[1]
}

// CheckVersionMismatch compares the CLI's and the tray's own compiled-in version and, when
// their minor versions differ, returns a non-blocking warning message.
func CheckVersionMismatch(cliVersion, trayVersion string) (warning string, mismatched bool) {
	if MinorVersion(cliVersion) == MinorVersion(trayVersion) {
		return "", false
	}
	return fmt.Sprintf("orobox CLI is %s but orobox-tray was built against %s — self-update only updates the CLI, run your package manager to update the tray too", cliVersion, trayVersion), true
}

// runVersionCommand runs `<bin> --version` and returns its raw output. A variable so tests can
// replace it without a real orobox binary.
var runVersionCommand = func(bin string) (string, error) {
	out, err := exec.Command(bin, "--version").Output()
	return string(out), err
}

// FetchCLIVersion runs `<oroboxBin> --version` and parses the version token out of it.
func FetchCLIVersion(oroboxBin string) (string, error) {
	output, err := runVersionCommand(oroboxBin)
	if err != nil {
		return "", fmt.Errorf("running %s --version: %w", oroboxBin, err)
	}
	return ParseVersionOutput(output)
}
