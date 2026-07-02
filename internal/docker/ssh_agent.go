package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/algoritma-dev/orobox/internal/utils"
)

// candidateSSHKeys lists the default private-key filenames EnsureSSHAgent will
// load, in preference order.
var candidateSSHKeys = []string{"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"}

var (
	agentSockRe = regexp.MustCompile(`SSH_AUTH_SOCK=([^;]+);`)
	agentPIDRe  = regexp.MustCompile(`SSH_AGENT_PID=(\d+)`)
)

// EnsureSSHAgent makes an SSH agent holding a usable key reachable through
// $SSH_AUTH_SOCK so CredentialRunArgs can forward it into containers.
//
// On Linux, when a repository uses an SSH URL and no agent is already running,
// it starts an ephemeral ssh-agent, runs `ssh-add` on the first existing default
// key, and exports SSH_AUTH_SOCK into this process — the equivalent of
// `eval $(ssh-agent) && ssh-add ~/.ssh/id_rsa`. It errors when an SSH repository
// is configured but no default key exists.
//
// It is a no-op when SSH is not needed, when an agent is already present, or on
// macOS/Windows (where Docker Desktop forwards the host agent itself). The
// returned cleanup kills any agent this function spawned (no-op otherwise) and
// must be called by the caller (typically via defer).
func EnsureSSHAgent(repos []map[string]interface{}, extraURLs ...string) (func(), error) {
	noop := func() {}

	if !reposUseSSH(repos, extraURLs...) {
		return noop, nil
	}
	// Docker Desktop forwards the host agent via a well-known socket; nothing to do.
	if runtime.GOOS != "linux" {
		return noop, nil
	}
	// The caller already has an agent running; use it as-is.
	if os.Getenv("SSH_AUTH_SOCK") != "" {
		return noop, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return noop, fmt.Errorf("cannot locate home directory to find an SSH key: %w", err)
	}

	keyPath := ""
	for _, name := range candidateSSHKeys {
		p := filepath.Join(home, ".ssh", name)
		if _, statErr := os.Stat(p); statErr == nil {
			keyPath = p
			break
		}
	}
	if keyPath == "" {
		return noop, fmt.Errorf(
			"an SSH repository is configured but no SSH key was found in %s (looked for %s); create one with `ssh-keygen` and add the public key to your git host",
			filepath.Join(home, ".ssh"), strings.Join(candidateSSHKeys, ", "),
		)
	}

	out, err := exec.Command("ssh-agent", "-s").Output()
	if err != nil {
		return noop, fmt.Errorf("failed to start ssh-agent: %w", err)
	}
	sockMatch := agentSockRe.FindStringSubmatch(string(out))
	if sockMatch == nil {
		return noop, fmt.Errorf("could not parse SSH_AUTH_SOCK from ssh-agent output")
	}
	sock := sockMatch[1]

	pid := ""
	if pidMatch := agentPIDRe.FindStringSubmatch(string(out)); pidMatch != nil {
		pid = pidMatch[1]
	}

	os.Setenv("SSH_AUTH_SOCK", sock)
	cleanup := func() {
		if pid != "" {
			_ = exec.Command("kill", pid).Run()
		}
		os.Unsetenv("SSH_AUTH_SOCK")
	}

	add := exec.Command("ssh-add", keyPath)
	// Wire the terminal through so ssh-add can prompt for a passphrase when the
	// key is encrypted. Clear SSH_ASKPASS/DISPLAY so ssh-add reads the passphrase
	// from the tty instead of spawning a GUI/askpass helper.
	add.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sock)
	add.Env = append(add.Env, "SSH_ASKPASS_REQUIRE=never")
	add.Stdin = os.Stdin
	add.Stdout = os.Stdout
	add.Stderr = os.Stderr
	if addErr := add.Run(); addErr != nil {
		cleanup()
		return noop, fmt.Errorf("failed to load SSH key %s (wrong passphrase or unreadable key): %w", keyPath, addErr)
	}

	utils.PrintInfo(fmt.Sprintf("Started ssh-agent and loaded key %s for private repository access.", keyPath))
	return cleanup, nil
}
