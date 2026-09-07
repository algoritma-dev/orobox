package tray

import "testing"

func TestBuildMkcertInstallCmdRunsMkcertInstallAndKeepsWindowOpen(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")

	cmd := BuildMkcertInstallCmd("xterm", terminalArgPrefix["xterm"])

	want := []string{"xterm", "-e", "/bin/bash", "-lc", "mkcert -install; exec $SHELL"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("cmd.Args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Errorf("cmd.Args[%d] = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
}
