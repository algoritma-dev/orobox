package tray

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// terminalArgPrefix maps a terminal emulator's binary name to the argv tokens that precede the
// command to run — the syntax is not uniform across emulators, so this is an explicit table
// rather than a generic -e flag (§6.5: the most likely source of bugs in the whole project).
var terminalArgPrefix = map[string][]string{
	"gnome-terminal": {"--"}, // -e is deprecated and ignores quoting
	"ptyxis":         {"--"}, // default on recent Fedora/GNOME
	"konsole":        {"-e"},
	"xfce4-terminal": {"-x"}, // -e takes a single string, -x the rest of the line
	"alacritty":      {"-e"},
	"kitty":          {}, // the command follows directly
	"foot":           {},
	"wezterm":        {"start", "--"},
	"xterm":          {"-e"}, // final fallback
}

// terminalFallbackOrder is tried, in order, once no explicit preference or $TERMINAL resolves.
var terminalFallbackOrder = []string{
	"ptyxis", "gnome-terminal", "konsole", "alacritty", "kitty", "foot", "wezterm", "xfce4-terminal", "xterm",
}

// lookPathTerminal is a variable so tests can simulate which emulators are installed.
var lookPathTerminal = func(name string) error {
	_, err := exec.LookPath(name)
	return err
}

// resolveXTerminalEmulator follows the x-terminal-emulator alternative (update-alternatives on
// Debian/Ubuntu) to whatever it actually points at, and reports its basename. It is not
// necessarily one of the emulators in terminalArgPrefix: Warp, for one, registers itself as this
// alternative on some installs and accepts none of the argv conventions this package knows —
// exactly the case DetectTerminal must not guess "-e" for.
var resolveXTerminalEmulator = func() (string, bool) {
	path, err := exec.LookPath("x-terminal-emulator")
	if err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	return filepath.Base(resolved), true
}

// DetectTerminal picks a terminal emulator to spawn interactive commands in, in order:
// preference (from tray.yaml), $TERMINAL, x-terminal-emulator, then the known fallback list.
//
// x-terminal-emulator is treated specially: unlike every other candidate here, it is a
// system-wide alternative that can point at literally anything, so its target is resolved and
// checked against the known argv table before it is trusted. A candidate this package cannot
// verify is skipped rather than launched with a guessed "-e" that may not apply.
func DetectTerminal(preference string) (bin string, argPrefix []string, err error) {
	var candidates []string
	if preference != "" {
		candidates = append(candidates, preference)
	}
	if t := os.Getenv("TERMINAL"); t != "" {
		candidates = append(candidates, t)
	}
	candidates = append(candidates, "x-terminal-emulator")
	candidates = append(candidates, terminalFallbackOrder...)

	for _, name := range candidates {
		if lookPathTerminal(name) != nil {
			continue
		}
		if name == "x-terminal-emulator" {
			resolved, ok := resolveXTerminalEmulator()
			if !ok {
				continue
			}
			args, known := terminalArgPrefix[resolved]
			if !known {
				continue
			}
			return name, args, nil
		}
		return name, argsFor(name), nil
	}
	return "", nil, errors.New("no terminal emulator found: install one, set $TERMINAL, or configure `terminal` in tray.yaml")
}

// argsFor returns the known argv prefix for name, or a plain -e for an unrecognized (custom)
// terminal — the most common convention when nothing more specific is known.
func argsFor(name string) []string {
	if args, ok := terminalArgPrefix[name]; ok {
		return args
	}
	return []string{"-e"}
}

// BuildTerminalCmd constructs (but does not start) the command to open a terminal running
// `orobox <oroboxArgs...>` in hostPath. The `cd` guarantees GetProjectName() computes the right
// project name (§2.2), and `exec $SHELL` keeps the window open so a failure's output can be
// read instead of the window vanishing.
func BuildTerminalCmd(bin string, argPrefix []string, hostPath string, oroboxArgs ...string) *exec.Cmd {
	shellCmd := fmt.Sprintf("cd %s && orobox %s; exec $SHELL", shellQuote(hostPath), strings.Join(oroboxArgs, " "))
	return buildTerminalCmdRaw(bin, argPrefix, shellCmd)
}

// BuildMkcertInstallCmd constructs the command to open a terminal running `mkcert -install`,
// so the user can enter their sudo password interactively (§2.6) — no project directory is
// involved, unlike BuildTerminalCmd.
func BuildMkcertInstallCmd(bin string, argPrefix []string) *exec.Cmd {
	return buildTerminalCmdRaw(bin, argPrefix, "mkcert -install; exec $SHELL")
}

func buildTerminalCmdRaw(bin string, argPrefix []string, shellCmd string) *exec.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	args := make([]string, 0, len(argPrefix)+3)
	args = append(args, argPrefix...)
	args = append(args, shell, "-lc", shellCmd)
	return exec.Command(bin, args...)
}

// shellQuote wraps s in single quotes for POSIX shell, escaping any embedded single quote.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
