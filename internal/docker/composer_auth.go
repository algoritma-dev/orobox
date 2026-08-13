package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"

	"github.com/algoritma-dev/orobox/internal/utils"
)

// containerSSHAgentSocket is the path the host SSH agent socket is bind-mounted to
// inside the container. SSH_AUTH_SOCK is pointed at it so git/composer can talk to
// the forwarded agent.
const containerSSHAgentSocket = "/ssh-agent"

// dockerDesktopSSHAgentSocket is the well-known socket Docker Desktop (macOS/Windows)
// exposes to forward the host SSH agent into containers.
const dockerDesktopSSHAgentSocket = "/run/host-services/ssh-auth.sock"

// sshURLPattern matches SSH-transport git URLs: scp-like (git@github.com:org/repo.git)
// and explicit ssh:// URLs. It deliberately does not match http(s):// or file paths.
var sshURLPattern = regexp.MustCompile(`^(ssh://|[A-Za-z0-9._-]+@[A-Za-z0-9._-]+:)`)

// IsSSHRepoURL reports whether url uses an SSH transport.
func IsSSHRepoURL(url string) bool {
	return sshURLPattern.MatchString(url)
}

// reposUseSSH reports whether any repository definition, or any of the extra URLs,
// uses an SSH transport.
func reposUseSSH(repos []map[string]interface{}, extraURLs ...string) bool {
	for _, u := range extraURLs {
		if IsSSHRepoURL(u) {
			return true
		}
	}
	for _, repo := range repos {
		if u, ok := repo["url"].(string); ok && IsSSHRepoURL(u) {
			return true
		}
	}
	return false
}

// composerAuthEnv builds the "COMPOSER_AUTH=<json>" env entry from the composer.auth
// section of orobox.yml, or "" when no auth is configured.
func composerAuthEnv(auth map[string]interface{}) string {
	if len(auth) == 0 {
		return ""
	}
	encoded, err := json.Marshal(auth)
	if err != nil {
		utils.PrintWarning("Could not serialize composer.auth; skipping COMPOSER_AUTH injection.")
		return ""
	}
	return "COMPOSER_AUTH=" + string(encoded)
}

// ComposerAuthJSON returns the JSON value for COMPOSER_AUTH built from the composer.auth
// section, or "" when no auth is configured. Callers that inject it as a secret rather than
// a plain env entry use this instead of CredentialRunArgs.
func ComposerAuthJSON(auth map[string]interface{}) string {
	env := composerAuthEnv(auth)
	if env == "" {
		return ""
	}
	return env[len("COMPOSER_AUTH="):]
}

// hostSSHAgentSocket returns the host-side path to bind-mount as the SSH agent
// socket, and whether an agent is available. On Docker Desktop the well-known
// forwarding socket is always used; on native Linux the live $SSH_AUTH_SOCK is used.
func hostSSHAgentSocket() (string, bool) {
	switch runtime.GOOS {
	case "darwin", "windows":
		return dockerDesktopSSHAgentSocket, true
	default:
		sock := os.Getenv("SSH_AUTH_SOCK")
		return sock, sock != ""
	}
}

// CredentialRunArgs returns extra `docker compose run` flags that forward private
// repository credentials into a composer/git command:
//   - COMPOSER_AUTH (token / basic auth) from composer.auth in orobox.yml
//   - the host SSH agent socket + SSH_AUTH_SOCK/GIT_SSH_COMMAND when any repository
//     (or one of extraURLs) uses an SSH URL
//
// The flags are meant to be spliced before the SERVICE name of a `run` invocation.
// They are no-ops (nil) when nothing needs forwarding.
func CredentialRunArgs(auth map[string]interface{}, repos []map[string]interface{}, extraURLs ...string) []string {
	var args []string

	if env := composerAuthEnv(auth); env != "" {
		args = append(args, "-e", env)
	}

	if reposUseSSH(repos, extraURLs...) {
		sock, ok := hostSSHAgentSocket()
		if !ok {
			utils.PrintWarning("An SSH repository URL is configured but no SSH agent was found ($SSH_AUTH_SOCK is empty). Start ssh-agent and add your key, e.g. `eval $(ssh-agent) && ssh-add`.")
		} else {
			args = append(args,
				"-v", sock+":"+containerSSHAgentSocket,
				"-e", "SSH_AUTH_SOCK="+containerSSHAgentSocket,
				// The container may run as a host UID with no home directory, leaving
				// HOME=/ (not writable). Point HOME at /tmp so ssh can create ~/.ssh.
				"-e", "HOME=/tmp",
				// accept-new avoids the interactive host-key prompt that would hang
				// a non-TTY (`-T`) composer/git run on first connect. UserKnownHostsFile
				// points at /tmp so the write never depends on a writable $HOME/.ssh.
				"-e", "GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/tmp/known_hosts",
			)
			// A unix socket is only connectable with write permission, which the
			// agent socket grants to its owning host user, not to the image
			// default (www-data). Run the credential command as the host user so
			// ssh inside the container can actually reach the forwarded agent.
			if runtime.GOOS == "linux" {
				args = append(args, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
			}
		}
	}

	return args
}
