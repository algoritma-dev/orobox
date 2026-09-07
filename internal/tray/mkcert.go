package tray

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/algoritma-dev/orobox/internal/project"
	"github.com/algoritma-dev/orobox/internal/utils"
)

// NeedsSSL reports whether p has at least one domain configured with ssl: true — the trigger
// for the mkcert preflight before `up` (§2.6).
func NeedsSSL(p project.Project) bool {
	if p.Config == nil {
		return false
	}
	for _, d := range p.Config.Domains {
		if d.Ssl {
			return true
		}
	}
	return false
}

// checkHostInEtcHosts wraps internal/utils.CheckHostInEtcHosts so tests can drive MissingHosts
// without reading the real /etc/hosts.
var checkHostInEtcHosts = utils.CheckHostInEtcHosts

// MissingHosts returns p's domains absent from /etc/hosts: Open would build a URL that never
// resolves for these.
func MissingHosts(p project.Project) []string {
	if p.Config == nil {
		return nil
	}
	var missing []string
	for _, d := range p.Config.Domains {
		if !checkHostInEtcHosts(d.Host) {
			missing = append(missing, d.Host)
		}
	}
	return missing
}

var (
	lookPathMkcert = func() error {
		_, err := exec.LookPath("mkcert")
		return err
	}
	runMkcertCAROOT = func() (string, error) {
		out, err := exec.Command("mkcert", "-CAROOT").Output()
		return strings.TrimSpace(string(out)), err
	}
	statPath = func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}
)

// MkcertAvailable reports whether the mkcert binary is on PATH.
func MkcertAvailable() bool {
	return lookPathMkcert() == nil
}

// MkcertCAInstalled reports whether mkcert's local CA has been generated: `mkcert -CAROOT`
// resolves, and rootCA.pem exists there. It does not additionally check the OS trust store —
// harder to introspect portably, and the CI-facing symptom (an untrusted HTTPS cert) is the
// same either way; this is the concrete check the spec gives (§2.6).
func MkcertCAInstalled() bool {
	caRoot, err := runMkcertCAROOT()
	if err != nil || caRoot == "" {
		return false
	}
	return statPath(filepath.Join(caRoot, "rootCA.pem"))
}
