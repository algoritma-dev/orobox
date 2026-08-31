package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/utils"
)

// candidateSSHKeys lists the default private-key filenames EnsureSSHAgent will
// load, in preference order.
var candidateSSHKeys = []string{"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"}

var (
	agentSockRe = regexp.MustCompile(`SSH_AUTH_SOCK=([^;]+);`)
	agentPIDRe  = regexp.MustCompile(`SSH_AGENT_PID=(\d+)`)
)

// agentListExitCode probes the agent behind sock with `ssh-add -l`:
// 0 = agent holds identities, 1 = agent reachable but empty,
// 2 (or any other failure) = agent unreachable/stale.
func agentListExitCode(sock string) int {
	cmd := exec.Command("ssh-add", "-l")
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sock)
	err := cmd.Run()
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 2
}

// findDefaultSSHKey returns the first existing default private key in ~/.ssh.
func findDefaultSSHKey() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate home directory to find an SSH key: %w", err)
	}
	for _, name := range candidateSSHKeys {
		p := filepath.Join(home, ".ssh", name)
		if _, statErr := os.Stat(p); statErr == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf(
		"an SSH repository is configured but no SSH key was found in %s (looked for %s); create one with `ssh-keygen` and add the public key to your git host",
		filepath.Join(home, ".ssh"), strings.Join(candidateSSHKeys, ", "),
	)
}

// loadKeyIntoAgent runs `ssh-add keyPath` against the agent behind sock.
// The terminal is wired through so ssh-add can prompt for a passphrase when
// the key is encrypted; SSH_ASKPASS_REQUIRE=never forces the prompt onto the
// tty instead of a GUI/askpass helper.
func loadKeyIntoAgent(sock, keyPath string) error {
	add := exec.Command("ssh-add", keyPath)
	add.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sock)
	add.Env = append(add.Env, "SSH_ASKPASS_REQUIRE=never")
	add.Stdin = os.Stdin
	add.Stdout = os.Stdout
	add.Stderr = os.Stderr
	if err := add.Run(); err != nil {
		return fmt.Errorf("failed to load SSH key %s (wrong passphrase or unreadable key): %w", keyPath, err)
	}
	return nil
}

// EnsureSSHAgent makes an SSH agent holding a usable key reachable through
// $SSH_AUTH_SOCK so CredentialRunArgs can forward it into containers.
//
// On Linux, when needsSSHForwarding says the agent must be forwarded:
//   - an existing agent that already holds identities is used as-is;
//   - an existing but empty agent (the desktop-keyring case) gets the first
//     default key loaded into it, prompting for a passphrase if needed;
//   - an unreachable/stale $SSH_AUTH_SOCK is ignored: an ephemeral ssh-agent
//     is started and the key loaded, the equivalent of
//     `eval $(ssh-agent) && ssh-add ~/.ssh/id_rsa`.
//
// It errors when an SSH repository is configured but no default key exists.
//
// It is a no-op when SSH is not needed or on macOS/Windows (where Docker
// Desktop forwards the host agent itself). The returned cleanup kills any
// agent this function spawned (no-op otherwise) and must be called by the
// caller (typically via defer).
func EnsureSSHAgent(c config.ComposerConfig, extraURLs ...string) (func(), error) {
	noop := func() {}

	if !needsSSHForwarding(c, extraURLs...) {
		return noop, nil
	}
	// Docker Desktop forwards the host agent via a well-known socket; nothing to do.
	if runtime.GOOS != "linux" {
		return noop, nil
	}

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		switch agentListExitCode(sock) {
		case 0:
			// Agent already holds identities; use it as-is.
			return noop, nil
		case 1:
			// Agent reachable but empty: load a key into it so the forwarded
			// agent is actually usable (prompts for a passphrase if needed).
			keyPath, err := findDefaultSSHKey()
			if err != nil {
				return noop, err
			}
			if err := loadKeyIntoAgent(sock, keyPath); err != nil {
				return noop, err
			}
			utils.PrintInfo(fmt.Sprintf("Loaded key %s into the running ssh-agent for private repository access.", keyPath))
			return noop, nil
		default:
			// Stale or unreachable socket: ignore it and start our own agent.
		}
	}

	keyPath, err := findDefaultSSHKey()
	if err != nil {
		return noop, err
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

	if addErr := loadKeyIntoAgent(sock, keyPath); addErr != nil {
		cleanup()
		return noop, addErr
	}

	utils.PrintInfo(fmt.Sprintf("Started ssh-agent and loaded key %s for private repository access.", keyPath))
	return cleanup, nil
}
