package tray

import (
	"fmt"
	"strings"
)

// portConflictMarkers are the substrings docker/dockerd use for "another process already has
// this host port". Matched loosely (not anchored) since the offending port/container id varies.
var portConflictMarkers = []string{
	"port is already allocated",
	"address already in use",
}

// IsPortConflictError reports whether output (a failed docker compose command's combined
// output) is a host-port conflict, as opposed to any other failure.
func IsPortConflictError(output string) bool {
	for _, marker := range portConflictMarkers {
		if strings.Contains(output, marker) {
			return true
		}
	}
	return false
}

// BuildSwitchFailureNotification builds the desktop notification for a failed switch (§6.3): a
// port conflict gets a message that names the actual cause instead of Docker's raw wording,
// since "port is already allocated" means nothing to a developer who didn't write the compose
// file — any other failure keeps the underlying error for anyone who does.
func BuildSwitchFailureNotification(target string, err error, output string) (summary, body string) {
	summary = fmt.Sprintf("orobox: switch to %s failed", target)
	if IsPortConflictError(output) {
		return summary, fmt.Sprintf("a port %s needs is already in use by another environment (or a host service) — stop it first", target)
	}
	return summary, fmt.Sprintf("%v", err)
}
