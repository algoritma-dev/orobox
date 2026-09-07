package tray

import "testing"

func TestBuildActionCmdSetsBinArgsAndDir(t *testing.T) {
	cmd := buildActionCmd("/opt/orobox/orobox", "/home/user/projects/mybundle", "up")

	if cmd.Path != "/opt/orobox/orobox" {
		t.Errorf("cmd.Path = %q, want %q", cmd.Path, "/opt/orobox/orobox")
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "/opt/orobox/orobox" || cmd.Args[1] != "up" {
		t.Errorf("cmd.Args = %v, want [%q up]", cmd.Args, "/opt/orobox/orobox")
	}
	if cmd.Dir != "/home/user/projects/mybundle" {
		t.Errorf("cmd.Dir = %q, want %q", cmd.Dir, "/home/user/projects/mybundle")
	}
}

func TestBuildActionCmdSupportsMultipleArgs(t *testing.T) {
	cmd := buildActionCmd("/opt/orobox/orobox", "/home/user/projects/mybundle", "xdebug", "on")

	want := []string{"/opt/orobox/orobox", "xdebug", "on"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("cmd.Args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Errorf("cmd.Args[%d] = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
}

func TestBuildActionCmdNeverPassesConfigFlag(t *testing.T) {
	// §2.2: the tray must never use --config, since GetProjectName() derives the compose
	// project name from the CWD, not from --config's target — cmd.Dir is the only lever.
	cmd := buildActionCmd("/opt/orobox/orobox", "/home/user/projects/mybundle", "down")

	for _, arg := range cmd.Args {
		if arg == "--config" {
			t.Errorf("cmd.Args = %v, must never contain --config", cmd.Args)
		}
	}
}
