// Package e2e contains the end-to-end test suite for the orobox CLI.
package e2e

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/algoritma-dev/orobox/internal/config"
)

// InstallType is an orobox install type exercised by the suite.
type InstallType string

// Install types exercised by the suite.
const (
	TypeProject InstallType = "project"
	TypeBundle  InstallType = "bundle"
)

var defaultTypes = []InstallType{TypeProject, TypeBundle}

// defaultNginxHTTPPort mirrors the default in docker.GetNginxPorts: orobox publishes nginx
// on 8080 unless the config or ORO_NGINX_HTTP_PORT says otherwise.
const defaultNginxHTTPPort = "8080"

// e2eRunCommand is the custom command the fixtures define and the suite invokes through
// `orobox run`. Shared so the fixture and the call site cannot drift apart.
const e2eRunCommand = "e2e-cache-clear"

// projectOnlyGenerators are the generators orobox refuses outside install type "project".
// Keep in sync with the guards in cmd/deploy_init.go and cmd/ci_init.go; run
// `grep -rn 'only available for install type' cmd/` to check for new ones.
var projectOnlyGenerators = []string{"deploy-init", "ci-init"}

// Case is one (version, install type) matrix entry.
type Case struct {
	Version string
	Type    InstallType
}

// sanitizeVersion makes a version string safe for docker names: lowercase, no dots.
func sanitizeVersion(v string) string {
	return strings.ToLower(strings.ReplaceAll(v, ".", ""))
}

// ProjectName is the docker compose project name for this case: lowercase, dot-free, unique.
func (c Case) ProjectName() string {
	return "oroboxe2e-" + string(c.Type) + "-" + sanitizeVersion(c.Version)
}

// Host is a unique domain for this case so parallel cases cannot clash.
func (c Case) Host() string {
	return "oro-" + string(c.Type) + "-" + sanitizeVersion(c.Version) + ".e2e.test"
}

// BaseURL is where the case's stack answers HTTP, port included.
//
// The port matters: orobox publishes nginx on 8080 by default (docker.GetNginxPorts), not
// on 80, so an assertion against a bare host name polls a port nothing listens on and
// fails with "connection refused" however healthy the stack is. getenv supplies
// ORO_NGINX_HTTP_PORT for setups that publish somewhere else — the same variable orobox
// itself reads — so the suite follows the port the stack actually uses.
func (c Case) BaseURL(getenv func(string) string) string {
	port := getenv("ORO_NGINX_HTTP_PORT")
	if port == "" {
		port = defaultNginxHTTPPort
	}
	return "http://" + c.Host() + ":" + port
}

// ParseMatrix builds the case list from CSV overrides. Empty inputs fall back to
// config.SupportedOroVersions and both install types. Cases are ordered version-outer,
// type-inner. Unknown install types are rejected.
func ParseMatrix(versionsCSV, typesCSV string) ([]Case, error) {
	versions := splitCSV(versionsCSV)
	if len(versions) == 0 {
		versions = append([]string(nil), config.SupportedOroVersions...)
	}

	var types []InstallType
	if raw := splitCSV(typesCSV); len(raw) > 0 {
		for _, r := range raw {
			t := InstallType(strings.ToLower(r))
			if t != TypeProject && t != TypeBundle {
				return nil, fmt.Errorf("unknown install type %q (expected %q or %q)", r, TypeProject, TypeBundle)
			}
			types = append(types, t)
		}
	} else {
		types = append([]InstallType(nil), defaultTypes...)
	}

	var cases []Case
	for _, v := range versions {
		for _, t := range types {
			cases = append(cases, Case{Version: v, Type: t})
		}
	}
	return cases, nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// RenderConfig executes tmplText as a text/template with the case as data.
func RenderConfig(tmplText string, c Case) (string, error) {
	t, err := template.New("orobox.yaml").Parse(tmplText)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	// Expose derived values to templates via an anonymous struct.
	data := struct {
		Version     string
		Type        InstallType
		Host        string
		ProjectName string
	}{c.Version, c.Type, c.Host(), c.ProjectName()}
	if err := t.Execute(&sb, data); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// RunResult captures one orobox invocation's output.
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// errorMarker is the glyph utils.PrintError writes for every fatal message. Orobox
// commands use cobra Run (not RunE) and exit 0 even on failure, so the exit code alone
// cannot be trusted; a printed error marker is the reliable failure signal.
const errorMarker = "✘"

// failed reports whether a result indicates failure: a nonzero exit, or an error marker
// printed to stdout/stderr despite a zero exit.
func failed(res RunResult) bool {
	return res.ExitCode != 0 ||
		strings.Contains(res.Stdout, errorMarker) ||
		strings.Contains(res.Stderr, errorMarker)
}

// ResolveBinary returns the orobox binary path from OROBOX_BIN, or signals that one must be built.
func ResolveBinary(getenv func(string) string) (string, bool) {
	if p := getenv("OROBOX_BIN"); p != "" {
		return p, false
	}
	return "", true
}
