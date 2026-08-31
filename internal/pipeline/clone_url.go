package pipeline

import (
	"net/url"
	"regexp"
	"strings"
)

// scpLikeRepository matches the scp-style syntax git accepts for SSH remotes — user@host:path —
// which is not a URL and so cannot be parsed as one.
var scpLikeRepository = regexp.MustCompile(`^[^@/]+@([^:/]+):(.+)$`)

// httpsCloneURL converts an SSH repository URL into the HTTPS one a token can clone. It reports
// false for anything already usable over HTTPS or not recognisably SSH, so the caller leaves the
// configured value alone.
//
// Only the clone inside the pipeline is affected. The value exported to Deployer as
// OROBOX_DEPLOY_REPOSITORY stays the configured URL: the release runs on the stage host with a
// key, and a token minted for one CI job would be gone by the next deploy.
//
// A custom SSH port is dropped rather than carried over. It addresses sshd, and the HTTPS
// endpoint of every host that serves both is on the default port.
func httpsCloneURL(repository string) (string, bool) {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return "", false
	}

	if m := scpLikeRepository.FindStringSubmatch(repository); m != nil {
		return "https://" + m[1] + "/" + strings.TrimPrefix(m[2], "/"), true
	}

	parsed, err := url.Parse(repository)
	if err != nil || parsed.Scheme != "ssh" || parsed.Hostname() == "" {
		return "", false
	}
	return "https://" + parsed.Hostname() + "/" + strings.TrimPrefix(parsed.Path, "/"), true
}
