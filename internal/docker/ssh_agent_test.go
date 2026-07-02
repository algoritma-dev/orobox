package docker

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

var sshRepos = []map[string]interface{}{
	{"type": "vcs", "url": "git@gitlab.example.com:org/private.git"},
}

// startTestAgent spawns a real ssh-agent and returns its socket path.
// The agent is killed when the test ends.
func startTestAgent(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("ssh-agent", "-s").Output()
	if err != nil {
		t.Skipf("ssh-agent unavailable: %v", err)
	}
	sockMatch := agentSockRe.FindStringSubmatch(string(out))
	pidMatch := agentPIDRe.FindStringSubmatch(string(out))
	if sockMatch == nil || pidMatch == nil {
		t.Fatalf("could not parse ssh-agent output: %s", out)
	}
	t.Cleanup(func() { _ = exec.Command("kill", pidMatch[1]).Run() })
	return sockMatch[1]
}

// makeTestKey creates a passphrase-less ed25519 key as $HOME/.ssh/id_ed25519
// inside a temp HOME, and points $HOME at it.
func makeTestKey(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519")
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", keyPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ssh-keygen unavailable: %v (%s)", err, out)
	}
	t.Setenv("HOME", home)
	return keyPath
}

// agentIdentityCount returns the exit code of `ssh-add -l` against sock:
// 0 = has identities, 1 = reachable but empty, 2 = unreachable.
func sshAddListExit(t *testing.T, sock string) int {
	t.Helper()
	cmd := exec.Command("ssh-add", "-l")
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sock)
	err := cmd.Run()
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	t.Fatalf("ssh-add -l failed to run: %v", err)
	return -1
}

// An agent is already running but holds no identities (the GNOME keyring
// desktop case). EnsureSSHAgent must load a default key into it instead of
// forwarding an empty agent.
func TestEnsureSSHAgent_LoadsKeyIntoExistingEmptyAgent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only behavior")
	}
	makeTestKey(t)
	sock := startTestAgent(t)
	t.Setenv("SSH_AUTH_SOCK", sock)

	cleanup, err := EnsureSSHAgent(sshRepos)
	if err != nil {
		t.Fatalf("EnsureSSHAgent: %v", err)
	}
	defer cleanup()

	if got := sshAddListExit(t, sock); got != 0 {
		t.Errorf("expected existing agent to hold an identity after EnsureSSHAgent (ssh-add -l exit 0), got exit %d", got)
	}
}

// SSH_AUTH_SOCK points at a dead socket. EnsureSSHAgent must not trust it:
// it should spawn its own agent and load the key.
func TestEnsureSSHAgent_ReplacesUnreachableAgent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only behavior")
	}
	makeTestKey(t)
	deadSock := filepath.Join(t.TempDir(), "dead.sock")
	t.Setenv("SSH_AUTH_SOCK", deadSock)

	cleanup, err := EnsureSSHAgent(sshRepos)
	if err != nil {
		t.Fatalf("EnsureSSHAgent: %v", err)
	}
	defer cleanup()

	newSock := os.Getenv("SSH_AUTH_SOCK")
	if newSock == deadSock {
		t.Fatal("expected EnsureSSHAgent to replace the unreachable agent socket")
	}
	if got := sshAddListExit(t, newSock); got != 0 {
		t.Errorf("expected spawned agent to hold an identity (ssh-add -l exit 0), got exit %d", got)
	}
}

// An agent with identities already loaded must be used as-is.
func TestEnsureSSHAgent_KeepsAgentWithIdentities(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only behavior")
	}
	keyPath := makeTestKey(t)
	sock := startTestAgent(t)
	add := exec.Command("ssh-add", keyPath)
	add.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sock)
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("ssh-add: %v (%s)", err, out)
	}
	t.Setenv("SSH_AUTH_SOCK", sock)

	cleanup, err := EnsureSSHAgent(sshRepos)
	if err != nil {
		t.Fatalf("EnsureSSHAgent: %v", err)
	}
	defer cleanup()

	if got := os.Getenv("SSH_AUTH_SOCK"); got != sock {
		t.Errorf("expected agent with identities to be kept, SSH_AUTH_SOCK changed to %q", got)
	}
}
