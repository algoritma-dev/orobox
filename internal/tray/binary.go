// Package tray builds the orobox-tray UI: menu construction, Docker/binary preflight checks
// and the systray wiring, kept separate from internal/project so the pure discovery logic
// stays testable without a display or a D-Bus session.
package tray

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ResolveOroboxBin locates the orobox binary the tray shells out to: OROBOX_BIN first, then the
// first "orobox" on PATH, then a short list of common install locations a graphical session's
// PATH often omits — a process launched from GNOME Activities, a systemd --user unit, or a
// .desktop Exec= does not source ~/.bashrc or ~/.zshrc, so it never sees the PATH additions
// those make. `go install .../orobox@latest` — the install method this repo's own README
// recommends — lands in $HOME/go/bin, which is exactly such an addition.
func ResolveOroboxBin() (string, error) {
	if bin := os.Getenv("OROBOX_BIN"); bin != "" {
		return bin, nil
	}
	if bin, err := exec.LookPath("orobox"); err == nil {
		return bin, nil
	}

	for _, candidate := range fallbackBinDirs() {
		bin := filepath.Join(candidate, "orobox")
		if isExecutableFile(bin) {
			return bin, nil
		}
	}

	return "", fmt.Errorf("orobox not found in PATH: exec: \"orobox\": executable file not found in $PATH")
}

// fallbackBinDirs are checked, in order, after a bare PATH lookup fails.
func fallbackBinDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return []string{"/usr/local/bin"}
	}
	return []string{
		filepath.Join(home, "go", "bin"), // `go install`
		filepath.Join(home, ".local", "bin"),
		"/usr/local/bin",
	}
}

// isExecutableFile reports whether path is both marked executable and actually looks like
// something exec.Command could run: an ELF binary or a #!-script. The executable bit alone is
// not enough — a stray non-binary file left behind by a failed download or install (observed: a
// gzip-compressed leftover) can carry it too, and handing that to exec.Command fails at run time
// with an opaque "exec format error" well after this function claimed success.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Mode()&0111 == 0 {
		return false
	}

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var header [4]byte
	n, _ := f.Read(header[:])
	if n >= 2 && header[0] == '#' && header[1] == '!' {
		return true
	}
	return n >= 4 && header[0] == 0x7f && header[1] == 'E' && header[2] == 'L' && header[3] == 'F'
}
