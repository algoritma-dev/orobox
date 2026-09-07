package tray

import (
	"os/exec"
	"strings"
)

// DockerState is the tray-wide (not per-project) availability of Docker itself.
type DockerState int

const (
	DockerOK DockerState = iota
	DockerBinaryMissing
	DockerDaemonNotRunning
	DockerPermissionDenied
)

// Message returns the tooltip text for the header when Docker is not fully available. A
// generic "Docker error" would send the user to a terminal, exactly what the tray exists to
// avoid, so each state names its own fix.
func (s DockerState) Message() string {
	switch s {
	case DockerBinaryMissing:
		return "docker not found in PATH — install Docker to use orobox"
	case DockerDaemonNotRunning:
		return "Docker daemon is not running — start it and orobox-tray will pick it up"
	case DockerPermissionDenied:
		return "permission denied on the Docker socket — add your user to the docker group and log back in"
	default:
		return ""
	}
}

// lookPathDocker and runDockerInfo are variables so tests can drive CheckDocker without a real
// Docker installation.
var (
	lookPathDocker = func() error {
		_, err := exec.LookPath("docker")
		return err
	}
	runDockerInfo = func() (string, error) {
		out, err := exec.Command("docker", "info").CombinedOutput()
		return string(out), err
	}
)

// CheckDocker distinguishes the three ways Docker can be unavailable, each needing a different
// fix from the user: the binary is missing, the daemon is not running, or the socket denied
// permission (user not in the docker group).
func CheckDocker() DockerState {
	if err := lookPathDocker(); err != nil {
		return DockerBinaryMissing
	}
	output, err := runDockerInfo()
	if err == nil {
		return DockerOK
	}
	return classifyDockerInfoError(output)
}

// classifyDockerInfoError maps `docker info`'s failure output to a DockerState. Anything that
// does not look like a permission error is treated as the daemon being unreachable, the most
// common cause once the binary itself is confirmed present.
func classifyDockerInfoError(output string) DockerState {
	if strings.Contains(output, "permission denied") {
		return DockerPermissionDenied
	}
	return DockerDaemonNotRunning
}
