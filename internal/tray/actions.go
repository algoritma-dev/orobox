package tray

import "os/exec"

// buildActionCmd constructs the command for an orobox action, never with --config: setting
// cmd.Dir is the only way GetProjectName() (CWD-based) resolves the right project, and
// --config would compute a different, wrong one (see internal/project package doc, §2.2).
func buildActionCmd(oroboxBin, hostPath string, args ...string) *exec.Cmd {
	cmd := exec.Command(oroboxBin, args...)
	cmd.Dir = hostPath
	return cmd
}

// RunAction runs `<oroboxBin> <args...>` (e.g. "up", "down", or "xdebug" "on") with the working
// directory set to hostPath, and returns its combined output.
func RunAction(oroboxBin, hostPath string, args ...string) ([]byte, error) {
	return buildActionCmd(oroboxBin, hostPath, args...).CombinedOutput()
}
