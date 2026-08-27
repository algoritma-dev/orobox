// Package e2e contains the end-to-end test suite for the orobox CLI.
package e2e

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/qatools"
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

// errorMarker is the glyph utils.PrintError writes for every fatal message.
//
// It is checked alongside the exit code, not instead of it. The commands this suite drives now
// propagate their failures (cobra RunE, or os.Exit where the command already used it), so the
// exit code is meaningful — but a printed error the command then swallowed is still a failure
// the suite should catch, and this is what catches it.
const errorMarker = "✘"

// failed reports whether a result indicates failure: a nonzero exit, or an error marker
// printed to stdout/stderr despite a zero exit.
func failed(res RunResult) bool {
	return res.ExitCode != 0 ||
		strings.Contains(res.Stdout, errorMarker) ||
		strings.Contains(res.Stderr, errorMarker)
}

// ResolveLogDir returns the directory the harness writes its artifacts into.
//
// The CI job archives this directory, so it has to be a path the harness actually creates: the
// workflow previously uploaded /tmp/orobox*.log, which nothing ever wrote, and reported "No
// files were found with the provided path" on every run.
func ResolveLogDir(getenv func(string) string) string {
	if dir := getenv("E2E_LOG_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(os.TempDir(), "orobox-e2e-logs")
}

// stepLogName names one invocation's log: a zero-padded step number so the files read in
// execution order, followed by the arguments so the failing step is identifiable without
// opening anything.
//
// Everything outside a small safe set collapses to a dash, so an argument carrying a path — a
// database dump, a report file — cannot put a separator in the name and write outside the log
// directory.
func stepLogName(step int, args []string) string {
	label := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_':
			return r
		default:
			return '-'
		}
	}, strings.Join(args, "-"))

	label = strings.Trim(label, "-")
	for strings.Contains(label, "--") {
		label = strings.ReplaceAll(label, "--", "-")
	}
	if label == "" {
		label = "orobox"
	}

	return fmt.Sprintf("%02d-%s.log", step, label)
}

// WriteStepLog records one orobox invocation under logDir.
//
// Kept here rather than on Box so it is exercised by the ordinary test run: the harness itself
// is behind the e2e build tag, and a guard that only runs inside the hour-long matrix job is a
// guard that never runs when it is needed.
func WriteStepLog(logDir string, step int, args []string, res RunResult) error {
	body := fmt.Sprintf("$ orobox %s\nexit: %d\n\n--- stdout ---\n%s\n--- stderr ---\n%s",
		strings.Join(args, " "), res.ExitCode, res.Stdout, res.Stderr)
	return os.WriteFile(filepath.Join(logDir, stepLogName(step, args)), []byte(body), 0o644)
}

// CaptureGeneratedConfig copies the .orobox directory orobox generated for a case — the compose
// files, the Dockerfile, the env files — from caseDir into logDir.
//
// It is the evidence the job log cannot carry: every bug this suite has caught so far came down
// to what those generated files ended up containing. A case that failed before init generated
// anything has nothing to copy, which is not an error. Only the top level is copied, since that
// is where the generated files are, and only regular files.
func CaptureGeneratedConfig(caseDir, logDir string) error {
	src := filepath.Join(caseDir, ".orobox")
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil
	}

	dst := filepath.Join(logDir, "orobox-config")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(dst, entry.Name()), content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ResolveBinary returns the orobox binary path from OROBOX_BIN, or signals that one must be built.
func ResolveBinary(getenv func(string) string) (string, bool) {
	if p := getenv("OROBOX_BIN"); p != "" {
		return p, false
	}
	return "", true
}

// e2eTestSuites are the PHPUnit suites the `test` step gates on, one orobox invocation each.
// Oro's phpunit.xml also defines "selenium", which needs a browser stack and is out of scope.
//
// Running them one at a time is what makes the report readable: `orobox test -t unit -t
// functional` produces a single JUnit document, so a functional suite that executed nothing
// would hide behind the unit suite's count.
var e2eTestSuites = []string{"unit", "functional"}

// e2eTestFilter narrows each suite to a handful of tests.
//
// The step exists to prove that `orobox test` really drives PHPUnit against the environment
// `test-init` provisioned — unit and functional alike. It is not here to run
// OroPlatform/OroCommerce's own suites: those take hours and their outcome says nothing about
// orobox. The filter is a PHPUnit regex over "Class::method".
//
// "UserTest" is chosen because UserBundle carries a class matching it in both suites in every
// supported version (Tests/Unit/Entity/UserTest and Tests/Functional/Api/RestJsonApi/UserTest,
// verified for 5.1, 6.0, 6.1 and 7.0), and because it holds no backslash: PHPUnit treats the
// filter as a regex, so a namespace-shaped filter would have its separators read as escapes.
const e2eTestFilter = "UserTest"

// junitTotals is the part of a JUnit report the suite gates on.
type junitTotals struct {
	Tests    int
	Failures int
	Errors   int
}

// parseJUnitTotals sums the counters of a JUnit document's top-level suites.
//
// The counts are what distinguishes a passing run from a run that executed nothing: PHPUnit
// exits 0 when a --filter matches no test at all, so the exit code alone would grade a
// silently empty run as green.
//
// Both roots PHPUnit and report.MergeJUnit can produce are accepted: <testsuites>, and the
// bare <testsuite> a single invocation may write.
func parseJUnitTotals(data []byte) (junitTotals, error) {
	type suite struct {
		Tests    int `xml:"tests,attr"`
		Failures int `xml:"failures,attr"`
		Errors   int `xml:"errors,attr"`
	}

	var root struct {
		XMLName xml.Name `xml:"testsuites"`
		Suites  []suite  `xml:"testsuite"`
	}
	if err := xml.Unmarshal(bytes.TrimSpace(data), &root); err != nil {
		var single struct {
			XMLName  xml.Name `xml:"testsuite"`
			Tests    int      `xml:"tests,attr"`
			Failures int      `xml:"failures,attr"`
			Errors   int      `xml:"errors,attr"`
		}
		if singleErr := xml.Unmarshal(bytes.TrimSpace(data), &single); singleErr != nil {
			return junitTotals{}, fmt.Errorf("not a JUnit document: %w", err)
		}
		return junitTotals{Tests: single.Tests, Failures: single.Failures, Errors: single.Errors}, nil
	}

	var totals junitTotals
	for _, s := range root.Suites {
		totals.Tests += s.Tests
		totals.Failures += s.Failures
		totals.Errors += s.Errors
	}
	return totals, nil
}

// qaToolOutcome is what one QA tool's own files in the raw report directory say about it: the
// exit code ReportScript recorded, and how many findings its Code Quality report holds.
type qaToolOutcome struct {
	Tool     string
	ExitCode int
	Findings int
	// ReportErr is set when the tool wrote something that is not a Code Quality document — a PHP
	// fatal, a configuration error, half a JSON array. It is never a finding.
	ReportErr error
}

// Lint reports whether this outcome is a tool that ran and disagreed with the code: the
// non-zero exit every QA tool uses for "I found something", next to a report that says what.
//
// Broken reports whether the tool could not do its job at all: it exited non-zero without
// producing a single finding, or produced something unreadable. That is an installation or
// configuration problem — a missing binary, an unparsable phpstan.neon, a failed cache warmup —
// and not a verdict about the code under analysis.
func (o qaToolOutcome) Lint() bool { return o.ExitCode != 0 && o.ReportErr == nil && o.Findings > 0 }
func (o qaToolOutcome) Broken() bool {
	return o.ExitCode != 0 && (o.ReportErr != nil || o.Findings == 0)
}

// ReadQaOutcomes reads the per-tool status and report files `orobox qa --report gitlab` leaves in
// rawDir, one outcome per tool that was actually invoked.
//
// The status files are the index rather than the reports: a tool that crashed before writing
// anything still has a status, while a report file alone cannot say whether the tool succeeded.
// An empty report is zero findings — that is what a clean tool writes.
func ReadQaOutcomes(rawDir string) ([]qaToolOutcome, error) {
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		return nil, fmt.Errorf("could not read the raw QA reports in %s: %w", rawDir, err)
	}

	var outcomes []qaToolOutcome
	prefix := qatools.StatusFile + "-"
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		tool := strings.TrimPrefix(entry.Name(), prefix)

		outcome := qaToolOutcome{Tool: tool}
		data, err := os.ReadFile(filepath.Join(rawDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("could not read the status of %s: %w", tool, err)
		}
		code, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, fmt.Errorf("the status of %s is not an exit code: %q", tool, string(data))
		}
		outcome.ExitCode = code
		outcome.Findings, outcome.ReportErr = qaFindings(filepath.Join(rawDir, tool+".json"))

		outcomes = append(outcomes, outcome)
	}

	sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].Tool < outcomes[j].Tool })
	return outcomes, nil
}

// qaFindings counts the issues in one tool's Code Quality report. A missing or empty file is zero
// findings: the tools that write the report themselves skip the file when they have nothing to
// say, and a redirect leaves an empty one.
func qaFindings(path string) (int, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return 0, nil
	}

	var issues []map[string]any
	if err := json.Unmarshal(data, &issues); err != nil {
		return 0, fmt.Errorf("the report is not GitLab Code Quality JSON: %w", err)
	}
	return len(issues), nil
}
