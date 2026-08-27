package e2e

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/qatools"
)

func TestProjectNameIsDockerSafe(t *testing.T) {
	c := Case{Version: "6.1", Type: TypeProject}
	got := c.ProjectName()
	if got != "oroboxe2e-project-61" {
		t.Fatalf("ProjectName() = %q, want %q", got, "oroboxe2e-project-61")
	}
	if strings.ContainsAny(got, ". ") || strings.ToLower(got) != got {
		t.Fatalf("ProjectName() %q not docker-safe", got)
	}
}

func TestHostIsUnique(t *testing.T) {
	a := Case{Version: "7.0", Type: TypeProject}.Host()
	b := Case{Version: "7.0", Type: TypeBundle}.Host()
	if a == b {
		t.Fatalf("hosts collide: %q", a)
	}
}

func TestParseMatrixDefaults(t *testing.T) {
	cases, err := ParseMatrix("", "")
	if err != nil {
		t.Fatal(err)
	}
	want := len(config.SupportedOroVersions) * 2
	if len(cases) != want {
		t.Fatalf("got %d cases, want %d", len(cases), want)
	}
	// version-outer, type-inner
	if cases[0].Type != TypeProject || cases[1].Type != TypeBundle {
		t.Fatalf("unexpected ordering: %+v", cases[:2])
	}
}

func TestParseMatrixSubsetAndTrim(t *testing.T) {
	cases, err := ParseMatrix(" 6.1 , 7.0 ", "bundle")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(cases))
	}
	for _, c := range cases {
		if c.Type != TypeBundle {
			t.Fatalf("unexpected type %q", c.Type)
		}
	}
}

func TestParseMatrixRejectsUnknownType(t *testing.T) {
	if _, err := ParseMatrix("6.1", "demo"); err == nil {
		t.Fatal("expected error for unknown type 'demo'")
	}
}

func TestRenderConfigSubstitutes(t *testing.T) {
	out, err := RenderConfig("oro_version: \"{{.Version}}\"\nhost: {{.Host}}\n", Case{Version: "6.0", Type: TypeProject})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `oro_version: "6.0"`) || !strings.Contains(out, "oro-project-60.e2e.test") {
		t.Fatalf("render missing substitutions:\n%s", out)
	}
}

func TestResolveBinaryPrefersEnv(t *testing.T) {
	path, needBuild := ResolveBinary(func(k string) string {
		if k == "OROBOX_BIN" {
			return "/custom/orobox"
		}
		return ""
	})
	if path != "/custom/orobox" || needBuild {
		t.Fatalf("ResolveBinary env = (%q,%v)", path, needBuild)
	}
	_, needBuild = ResolveBinary(func(string) string { return "" })
	if !needBuild {
		t.Fatal("expected needBuild when OROBOX_BIN unset")
	}
}

// TestCaseBaseURLUsesThePublishedPort guards against asserting HTTP on port 80: orobox
// publishes nginx on 8080 by default, so a bare host name never answers and every case
// failed with "connection refused" no matter how healthy the stack was.
func TestCaseBaseURLUsesThePublishedPort(t *testing.T) {
	c := Case{Version: "6.1", Type: TypeBundle}

	// The default must track orobox's own default, or the suite polls the wrong port.
	httpPort, _ := docker.GetNginxPorts()
	if want := "http://" + c.Host() + ":" + httpPort; c.BaseURL(func(string) string { return "" }) != want {
		t.Errorf("BaseURL default = %q, want %q (orobox publishes %s)", c.BaseURL(func(string) string { return "" }), want, httpPort)
	}

	// An explicit port wins, using the same variable orobox reads.
	got := c.BaseURL(func(k string) string {
		if k == "ORO_NGINX_HTTP_PORT" {
			return "80"
		}
		return ""
	})
	if want := "http://" + c.Host() + ":80"; got != want {
		t.Errorf("BaseURL with ORO_NGINX_HTTP_PORT = %q, want %q", got, want)
	}
}

func TestFailedDetectsErrorMarkerDespiteZeroExit(t *testing.T) {
	// Orobox init prints "✘ OroCommerce installation failed" but exits 0.
	res := RunResult{Stdout: "\x1b[31m✘ OroCommerce installation failed: exit status 1\x1b[0m\n", ExitCode: 0}
	if !failed(res) {
		t.Fatal("failed() must catch the error marker on a zero exit")
	}
}

func TestFailedOnNonzeroExit(t *testing.T) {
	if !failed(RunResult{ExitCode: 1}) {
		t.Fatal("failed() must catch a nonzero exit")
	}
}

func TestFailedFalseOnCleanSuccess(t *testing.T) {
	res := RunResult{Stdout: "✔ Orobox is up and running!\n", ExitCode: 0}
	if failed(res) {
		t.Fatal("failed() must not flag a clean success (✔ only)")
	}
}

func TestFixturesRenderValid(t *testing.T) {
	for _, f := range []struct {
		path string
		c    Case
	}{
		{"fixtures/project.orobox.yaml", Case{Version: "6.1", Type: TypeProject}},
		{"fixtures/bundle.orobox.yaml", Case{Version: "6.1", Type: TypeBundle}},
	} {
		raw, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatalf("read %s: %v", f.path, err)
		}
		out, err := RenderConfig(string(raw), f.c)
		if err != nil {
			t.Fatalf("render %s: %v", f.path, err)
		}
		if !strings.Contains(out, `oro_version: "6.1"`) {
			t.Fatalf("%s missing oro_version:\n%s", f.path, out)
		}
		if strings.Contains(out, "{{") {
			t.Fatalf("%s has unrendered template markers:\n%s", f.path, out)
		}

		// The suite runs `orobox run e2e-cache-clear`, which dispatches a command defined
		// here by name. A rename on either side used to surface only as a mid-run
		// "command 'x' not found in .orobox.yaml" after a full install, so pin the
		// contract: the fixture must parse and must define exactly that command.
		conf, err := config.ParseConfig([]byte(out))
		if err != nil {
			t.Fatalf("%s does not parse: %v", f.path, err)
		}
		var found bool
		for _, c := range conf.Commands {
			if c.Name == e2eRunCommand {
				found = true
				if c.Command == "" {
					t.Errorf("%s defines %q with an empty command", f.path, e2eRunCommand)
				}
			}
		}
		if !found {
			t.Errorf("%s must define the %q command the suite runs", f.path, e2eRunCommand)
		}
	}
}

// The workflow used to upload /tmp/orobox*.log, a path the harness never wrote: every run
// reported "No files were found with the provided path" and no artifact was ever produced. The
// log directory is now something the harness actually creates, and the workflow uploads it.
func TestResolveLogDirPrefersEnv(t *testing.T) {
	got := ResolveLogDir(func(k string) string {
		if k == "E2E_LOG_DIR" {
			return "/artifacts/e2e"
		}
		return ""
	})
	if got != "/artifacts/e2e" {
		t.Fatalf("ResolveLogDir() = %q, want /artifacts/e2e", got)
	}
}

func TestResolveLogDirHasADefault(t *testing.T) {
	got := ResolveLogDir(func(string) string { return "" })
	if got == "" {
		t.Fatal("ResolveLogDir() is empty: the upload step would have nothing to point at")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("ResolveLogDir() = %q, want an absolute path so the workflow can name it", got)
	}
}

// One file per invocation, ordered, and named after the command — so a failed run is read by
// looking at the last file rather than by scrolling the whole job log.
func TestStepLogNameIsOrderedAndSafe(t *testing.T) {
	cases := []struct {
		step int
		args []string
		want string
	}{
		{1, []string{"init", "-t", "project", "-v", "5.1"}, "01-init-t-project-v-5.1.log"},
		{2, []string{"up"}, "02-up.log"},
		{12, []string{"db", "backup", "/tmp/x/backup.sql"}, "12-db-backup-tmp-x-backup.sql.log"},
		{3, nil, "03-orobox.log"},
	}
	for _, c := range cases {
		if got := stepLogName(c.step, c.args); got != c.want {
			t.Errorf("stepLogName(%d, %v) = %q, want %q", c.step, c.args, got, c.want)
		}
	}
}

func TestStepLogNameHoldsNoPathSeparators(t *testing.T) {
	got := stepLogName(1, []string{"db", "restore", "/tmp/TestMatrix/001/dump.sql"})
	if strings.ContainsAny(got, `/\`) {
		t.Fatalf("stepLogName produced %q, which would escape the log directory", got)
	}
}

// The log has to carry the command, its exit code and both streams: a step that failed on a
// printed error marker with a zero exit is exactly the case where the exit code alone says
// nothing.
func TestWriteStepLogRecordsTheWholeInvocation(t *testing.T) {
	dir := t.TempDir()
	res := RunResult{Stdout: "installing\n", Stderr: "boom\n", ExitCode: 1}

	if err := WriteStepLog(dir, 4, []string{"init", "-t", "bundle"}, res); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, "04-init-t-bundle.log"))
	if err != nil {
		t.Fatalf("the log was not written where its name says: %v", err)
	}
	for _, want := range []string{"orobox init -t bundle", "exit: 1", "installing", "boom"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("log is missing %q:\n%s", want, body)
		}
	}
}

// The generated compose files and env files are what the job log cannot show, and they live in
// a workdir Go deletes once the case ends.
func TestCaptureGeneratedConfigCopiesTheGeneratedFiles(t *testing.T) {
	caseDir, logDir := t.TempDir(), t.TempDir()
	internal := filepath.Join(caseDir, ".orobox")
	if err := os.MkdirAll(filepath.Join(internal, "certs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"docker-compose.yml": "services: {}\n",
		".env":               "ORO_DB_HOST=db\n",
	} {
		if err := os.WriteFile(filepath.Join(internal, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := CaptureGeneratedConfig(caseDir, logDir); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{
		"docker-compose.yml": "services: {}\n",
		".env":               "ORO_DB_HOST=db\n",
	} {
		got, err := os.ReadFile(filepath.Join(logDir, "orobox-config", name))
		if err != nil {
			t.Errorf("%s was not captured: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// A case that failed before init generated anything has nothing to copy. That is the common
// shape of a broken run, so it must not turn into a teardown error on top of the real failure.
func TestCaptureGeneratedConfigToleratesAMissingInternalDir(t *testing.T) {
	if err := CaptureGeneratedConfig(t.TempDir(), t.TempDir()); err != nil {
		t.Fatalf("CaptureGeneratedConfig() = %v, want nil when nothing was generated", err)
	}
}

func TestParseJUnitTotalsSumsSuites(t *testing.T) {
	doc := []byte(`<?xml version="1.0"?>
<testsuites>
  <testsuite name="unit" tests="4" failures="1" errors="0"/>
  <testsuite name="functional" tests="2" failures="0" errors="1"/>
</testsuites>`)

	got, err := parseJUnitTotals(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got != (junitTotals{Tests: 6, Failures: 1, Errors: 1}) {
		t.Fatalf("parseJUnitTotals() = %+v", got)
	}
}

func TestParseJUnitTotalsAcceptsBareSuiteRoot(t *testing.T) {
	doc := []byte(`<testsuite name="unit" tests="3" failures="0" errors="0"/>`)

	got, err := parseJUnitTotals(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tests != 3 {
		t.Fatalf("parseJUnitTotals() = %+v, want 3 tests", got)
	}
}

// An empty document is what report mode writes when PHPUnit never produced a log: it must parse,
// and it must report zero tests, because that is the "executed nothing" case the step gates on.
func TestParseJUnitTotalsEmptyDocumentCountsNoTests(t *testing.T) {
	doc := []byte(xml.Header + "<testsuites></testsuites>")

	got, err := parseJUnitTotals(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tests != 0 {
		t.Fatalf("parseJUnitTotals() = %+v, want no tests", got)
	}
}

func TestParseJUnitTotalsRejectsGarbage(t *testing.T) {
	if _, err := parseJUnitTotals([]byte("not xml at all")); err == nil {
		t.Fatal("expected an error for a non-JUnit document")
	}
}

// The filter has to be usable as a PHPUnit regex over "Class::method": a namespace-shaped value
// would have its backslashes read as regex escapes (\B is a word-boundary assertion).
func TestTestFilterHoldsNoRegexEscapes(t *testing.T) {
	if strings.ContainsAny(e2eTestFilter, `\/`) {
		t.Fatalf("e2eTestFilter %q must not contain path or escape characters", e2eTestFilter)
	}
	if len(e2eTestSuites) == 0 {
		t.Fatal("no test suites declared for the test step")
	}
}

func TestReadQaOutcomesSeparatesFindingsFromToolsThatCouldNotRun(t *testing.T) {
	rawDir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(rawDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// phpstan: found something. rector: clean. php-cs-fixer: exited non-zero with nothing to
	// show for it, which is how a missing configuration or a broken install looks. twig-cs-fixer:
	// wrote a PHP fatal where the report belongs.
	write(qatools.ToolStatusFile("phpstan"), "1")
	write("phpstan.json", `[{"description":"x","location":{"path":"src/A.php"}}]`)
	write(qatools.ToolStatusFile("rector"), "0")
	write("rector.json", "[]")
	write(qatools.ToolStatusFile("php-cs-fixer"), "255")
	write("php-cs-fixer.json", "")
	write(qatools.ToolStatusFile("twig-cs-fixer"), "1")
	write("twig-cs-fixer.json", "PHP Fatal error: nope")
	// The aggregate status is not a tool and must not be graded as one.
	write(qatools.StatusFile, "1")

	outcomes, err := ReadQaOutcomes(rawDir)
	if err != nil {
		t.Fatalf("ReadQaOutcomes: %v", err)
	}
	if len(outcomes) != 4 {
		t.Fatalf("got %d outcomes, want 4: %+v", len(outcomes), outcomes)
	}

	byTool := map[string]QaToolOutcome{}
	for _, o := range outcomes {
		byTool[o.Tool] = o
	}

	for tool, want := range map[string]struct{ lint, broken bool }{
		"phpstan":       {lint: true},
		"rector":        {},
		"php-cs-fixer":  {broken: true},
		"twig-cs-fixer": {broken: true},
	} {
		got := byTool[tool]
		if got.Lint() != want.lint || got.Broken() != want.broken {
			t.Errorf("%s: lint=%v broken=%v, want lint=%v broken=%v (%+v)",
				tool, got.Lint(), got.Broken(), want.lint, want.broken, got)
		}
	}
	if byTool["phpstan"].Findings != 1 {
		t.Errorf("phpstan findings = %d, want 1", byTool["phpstan"].Findings)
	}
}

func TestReadQaOutcomesFailsWhenTheStepWroteNothing(t *testing.T) {
	if _, err := ReadQaOutcomes(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing raw report directory must be an error: the step never ran")
	}
}
