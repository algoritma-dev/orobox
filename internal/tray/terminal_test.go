package tray

import (
	"errors"
	"strings"
	"testing"
)

func TestDetectTerminalPrefersExplicitPreference(t *testing.T) {
	orig := lookPathTerminal
	defer func() { lookPathTerminal = orig }()
	lookPathTerminal = func(name string) error { return nil } // everything "exists"

	bin, _, err := DetectTerminal("kitty")
	if err != nil || bin != "kitty" {
		t.Errorf("DetectTerminal(kitty) = (%q, %v), want (kitty, nil)", bin, err)
	}
}

func TestDetectTerminalFallsBackToTERMINALEnvVar(t *testing.T) {
	orig := lookPathTerminal
	defer func() { lookPathTerminal = orig }()
	t.Setenv("TERMINAL", "foot")
	lookPathTerminal = func(name string) error { return nil }

	bin, _, err := DetectTerminal("")
	if err != nil || bin != "foot" {
		t.Errorf("DetectTerminal(\"\") = (%q, %v), want (foot, nil) from $TERMINAL", bin, err)
	}
}

func TestDetectTerminalFallsBackToXTerminalEmulatorWhenResolvedToKnownTerminal(t *testing.T) {
	origLookPath, origResolve := lookPathTerminal, resolveXTerminalEmulator
	defer func() { lookPathTerminal, resolveXTerminalEmulator = origLookPath, origResolve }()
	t.Setenv("TERMINAL", "")
	lookPathTerminal = func(name string) error {
		if name == "x-terminal-emulator" {
			return nil
		}
		return errors.New("not found")
	}
	resolveXTerminalEmulator = func() (string, bool) { return "gnome-terminal", true }

	bin, args, err := DetectTerminal("")
	if err != nil || bin != "x-terminal-emulator" {
		t.Errorf("DetectTerminal(\"\") = (%q, %v), want (x-terminal-emulator, nil)", bin, err)
	}
	if len(args) != 1 || args[0] != "--" {
		t.Errorf("args = %v, want gnome-terminal's [--] (the resolved target's convention)", args)
	}
}

// Regression test: on some installs (observed with Warp) the x-terminal-emulator alternative
// resolves to a terminal with a completely different CLI that does not accept "-e" at all —
// blindly trusting the alternative and guessing "-e" launches something that ignores or errors
// on the command entirely. DetectTerminal must skip it and keep looking instead.
func TestDetectTerminalSkipsXTerminalEmulatorWhenResolvedToUnknownTerminal(t *testing.T) {
	origLookPath, origResolve := lookPathTerminal, resolveXTerminalEmulator
	defer func() { lookPathTerminal, resolveXTerminalEmulator = origLookPath, origResolve }()
	t.Setenv("TERMINAL", "")
	lookPathTerminal = func(name string) error {
		if name == "x-terminal-emulator" || name == "alacritty" {
			return nil
		}
		return errors.New("not found")
	}
	resolveXTerminalEmulator = func() (string, bool) { return "warp-terminal", true }

	bin, _, err := DetectTerminal("")
	if err != nil || bin != "alacritty" {
		t.Errorf("DetectTerminal(\"\") = (%q, %v), want (alacritty, nil) — x-terminal-emulator resolving to an unknown terminal must be skipped", bin, err)
	}
}

func TestDetectTerminalSkipsXTerminalEmulatorWhenUnresolvable(t *testing.T) {
	origLookPath, origResolve := lookPathTerminal, resolveXTerminalEmulator
	defer func() { lookPathTerminal, resolveXTerminalEmulator = origLookPath, origResolve }()
	t.Setenv("TERMINAL", "")
	lookPathTerminal = func(name string) error {
		if name == "x-terminal-emulator" || name == "alacritty" {
			return nil
		}
		return errors.New("not found")
	}
	resolveXTerminalEmulator = func() (string, bool) { return "", false }

	bin, _, err := DetectTerminal("")
	if err != nil || bin != "alacritty" {
		t.Errorf("DetectTerminal(\"\") = (%q, %v), want (alacritty, nil) when x-terminal-emulator cannot be resolved", bin, err)
	}
}

func TestDetectTerminalFallsBackToKnownList(t *testing.T) {
	orig := lookPathTerminal
	defer func() { lookPathTerminal = orig }()
	t.Setenv("TERMINAL", "")
	lookPathTerminal = func(name string) error {
		if name == "alacritty" {
			return nil
		}
		return errors.New("not found")
	}

	bin, _, err := DetectTerminal("")
	if err != nil || bin != "alacritty" {
		t.Errorf("DetectTerminal(\"\") = (%q, %v), want (alacritty, nil)", bin, err)
	}
}

func TestDetectTerminalErrorsWhenNoneFound(t *testing.T) {
	orig := lookPathTerminal
	defer func() { lookPathTerminal = orig }()
	t.Setenv("TERMINAL", "")
	lookPathTerminal = func(name string) error { return errors.New("not found") }

	if _, _, err := DetectTerminal(""); err == nil {
		t.Fatal("DetectTerminal(\"\") error = nil, want error when nothing is found")
	}
}

// One case per row of the spec's argument table (§6.5), each with a hostPath containing a
// space to prove the shell command is quoted correctly.
func TestBuildTerminalCmdPerEmulator(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	hostPath := "/home/user/my project"

	tests := []struct {
		bin      string
		wantArgs []string
	}{
		{"gnome-terminal", []string{"--", "/bin/bash", "-lc", `cd '/home/user/my project' && orobox up; exec $SHELL`}},
		{"ptyxis", []string{"--", "/bin/bash", "-lc", `cd '/home/user/my project' && orobox up; exec $SHELL`}},
		{"konsole", []string{"-e", "/bin/bash", "-lc", `cd '/home/user/my project' && orobox up; exec $SHELL`}},
		{"xfce4-terminal", []string{"-x", "/bin/bash", "-lc", `cd '/home/user/my project' && orobox up; exec $SHELL`}},
		{"alacritty", []string{"-e", "/bin/bash", "-lc", `cd '/home/user/my project' && orobox up; exec $SHELL`}},
		{"kitty", []string{"/bin/bash", "-lc", `cd '/home/user/my project' && orobox up; exec $SHELL`}},
		{"foot", []string{"/bin/bash", "-lc", `cd '/home/user/my project' && orobox up; exec $SHELL`}},
		{"wezterm", []string{"start", "--", "/bin/bash", "-lc", `cd '/home/user/my project' && orobox up; exec $SHELL`}},
		{"xterm", []string{"-e", "/bin/bash", "-lc", `cd '/home/user/my project' && orobox up; exec $SHELL`}},
	}

	for _, tt := range tests {
		t.Run(tt.bin, func(t *testing.T) {
			cmd := BuildTerminalCmd(tt.bin, terminalArgPrefix[tt.bin], hostPath, "up")
			if cmd.Path != tt.bin && !strings.HasSuffix(cmd.Path, "/"+tt.bin) {
				t.Errorf("cmd.Path = %q, want %q (or resolved via PATH)", cmd.Path, tt.bin)
			}
			gotArgs := cmd.Args[1:]
			if len(gotArgs) != len(tt.wantArgs) {
				t.Fatalf("cmd.Args = %v, want %v", gotArgs, tt.wantArgs)
			}
			for i := range gotArgs {
				if gotArgs[i] != tt.wantArgs[i] {
					t.Errorf("cmd.Args[%d] = %q, want %q", i, gotArgs[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestBuildTerminalCmdDefaultsShellWhenUnset(t *testing.T) {
	t.Setenv("SHELL", "")
	cmd := BuildTerminalCmd("xterm", terminalArgPrefix["xterm"], "/home/user/mybundle", "up")

	found := false
	for _, a := range cmd.Args {
		if a == "/bin/sh" {
			found = true
		}
	}
	if !found {
		t.Errorf("cmd.Args = %v, want /bin/sh when $SHELL is unset", cmd.Args)
	}
}
