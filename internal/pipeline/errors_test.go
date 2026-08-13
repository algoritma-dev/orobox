package pipeline

import (
	"errors"
	"strings"
	"testing"

	"dagger.io/dagger"
)

func testRunner(debug bool) *runner {
	return &runner{
		plan: New(testConf("6.1", true), testStage(), "git@gitlab.com:acme/shop.git"),
		opts: Options{Debug: debug},
		log:  newLogBuffer(nil),
	}
}

func TestLogBufferTail(t *testing.T) {
	buf := newLogBuffer(nil)
	for i := 0; i < logTailLines+20; i++ {
		if _, err := buf.Write([]byte("line\n")); err != nil {
			t.Fatal(err)
		}
	}
	// Blank lines are dropped, so the tail must be exactly the cap.
	if got := len(strings.Split(buf.Tail(), "\n")); got != logTailLines {
		t.Errorf("tail has %d lines, want %d", got, logTailLines)
	}

	buf = newLogBuffer(nil)
	if _, err := buf.Write([]byte("first\n\n  \nlast\n")); err != nil {
		t.Fatal(err)
	}
	if got := buf.Tail(); got != "first\nlast" {
		t.Errorf("Tail() = %q, want blank lines dropped", got)
	}
}

func TestLogBufferDropsEngineBoilerplate(t *testing.T) {
	buf := newLogBuffer(nil)
	// Session startup plus the registry traffic emitted while pulling an image: this is what
	// actually fills the tail on a cold run.
	noise := `Creating new Engine session... OK!
Establishing connection to Engine... OK!
    156 : ┆ HTTP GET DONE [0.9s]
    155 : remotes.docker.resolver.HTTPRequest DONE [0.9s]
    159 : remotes.docker.resolver.HTTPRequest
    121 : ┆ HTTP GET DONE [6.0s]
    129 :
    130 : DONE [1.2s]
    88 : extracting sha256:9f1537a7b5df [1.2s]
    89 : resolve docker.io/algoritmadev/orobox:6.1-project-latest DONE [0.4s]
`
	if _, err := buf.Write([]byte(noise)); err != nil {
		t.Fatal(err)
	}
	// Reporting startup chatter as the cause of a failure is worse than reporting nothing.
	if got := buf.Tail(); got != "" {
		t.Errorf("Tail() = %q, want the engine startup lines dropped", got)
	}

	// Real output survives, including words that also appear in pull progress: a pull line is
	// only recognised when it carries a registry reference or a digest too.
	output := "42 : exec bash -c composer install ERROR\n" +
		"composer: could not resolve dependencies\n" +
		"phpstan: 3 errors\n"
	if _, err := buf.Write([]byte(output)); err != nil {
		t.Fatal(err)
	}
	if got := buf.Tail(); got != strings.TrimRight(output, "\n") {
		t.Errorf("Tail() = %q, want only the real output", got)
	}
}

func TestDescribeSkipsEngineLogWhenTheCommandSpoke(t *testing.T) {
	r := testRunner(false)
	if _, err := r.log.Write([]byte("some engine chatter\n")); err != nil {
		t.Fatal(err)
	}

	execErr := &dagger.ExecError{
		Cmd:      []string{"bash", "-c", "bin/phpstan analyze"},
		ExitCode: 1,
		Stderr:   "phpstan: 3 errors found",
	}

	message := r.describe("QA checks", execErr).Error()
	for _, want := range []string{"bin/phpstan analyze", "exit code: 1", "phpstan: 3 errors found"} {
		if !strings.Contains(message, want) {
			t.Errorf("message is missing %q:\n%s", want, message)
		}
	}
	// The failing command's own output says everything; engine internals would only bury it.
	if strings.Contains(message, "engine log") {
		t.Errorf("engine log should be omitted when stderr is available:\n%s", message)
	}
}

func TestLogBufferTees(t *testing.T) {
	var tee strings.Builder
	buf := newLogBuffer(&tee)
	if _, err := buf.Write([]byte("streamed\n")); err != nil {
		t.Fatal(err)
	}
	if tee.String() != "streamed\n" {
		t.Errorf("tee = %q, want the output streamed through", tee.String())
	}
}

func TestLogBufferTeeFiltersNoiseAndBuffersPartialLines(t *testing.T) {
	var tee strings.Builder
	buf := newLogBuffer(&tee)

	// The registry traffic must not reach a --debug stream either: it drowns out the output
	// someone turned --debug on to read.
	writes := []string{
		"155 : remotes.docker.resolver.HTTPRequest DONE [0.9s]\n",
		"phpstan: ", "3 errors\n",
		"    121 : ┆ HTTP GET DONE [6.0s]\n",
		"incomplete without a newline",
	}
	for _, chunk := range writes {
		if _, err := buf.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}

	if tee.String() != "phpstan: 3 errors\n" {
		t.Errorf("tee = %q, want the noise filtered and the split line reassembled", tee.String())
	}
}

func TestDescribeIncludesEngineLogAndDebugHint(t *testing.T) {
	r := testRunner(false)
	if _, err := r.log.Write([]byte("composer: could not resolve dependencies\n")); err != nil {
		t.Fatal(err)
	}

	err := r.describe("QA checks", errors.New("exit status 1 [traceparent:abc-def]"))
	message := err.Error()

	for _, want := range []string{
		"QA checks failed",
		"exit status 1",
		"engine log (last lines)",
		"composer: could not resolve dependencies",
		"--debug",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("message is missing %q:\n%s", want, message)
		}
	}
	// The OpenTelemetry id is noise for anyone not debugging the engine.
	if strings.Contains(message, "traceparent") {
		t.Errorf("message still contains the traceparent:\n%s", message)
	}
}

func TestDescribeOmitsDebugHintWhenDebugging(t *testing.T) {
	message := testRunner(true).describe("tests", errors.New("boom")).Error()
	if strings.Contains(message, "--debug") {
		t.Errorf("the hint should not be shown when already running with --debug:\n%s", message)
	}
}

func TestCloneErrorHints(t *testing.T) {
	r := testRunner(false)

	// No credentials at all.
	message := r.cloneError(errors.New("git error: exit status 128")).Error()
	for _, want := range []string{
		"cloning git@gitlab.com:acme/shop.git at develop failed",
		`check that the ref "develop" exists`,
		"no clone credentials were available",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("message is missing %q:\n%s", want, message)
		}
	}

	// With an https token configured the hint must point at the token instead.
	r.opts.GitHTTPToken = "job-token"
	message = r.cloneError(errors.New("git error: exit status 128")).Error()
	if !strings.Contains(message, "OROBOX_DEPLOY_GIT_TOKEN") {
		t.Errorf("message does not mention the token:\n%s", message)
	}
}

func TestStripTraceparent(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"git error: exit status 128 [traceparent:abc-def]", "git error: exit status 128"},
		{"no id here", "no id here"},
		{"broken [traceparent:abc", "broken"},
	}
	for _, tt := range tests {
		if got := stripTraceparent(tt.in); got != tt.want {
			t.Errorf("stripTraceparent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTrimOutputKeepsTheEnd(t *testing.T) {
	long := strings.Repeat("a", outputLimit+500) + "THE-ACTUAL-ERROR"
	got := trimOutput(long)

	if !strings.Contains(got, "THE-ACTUAL-ERROR") {
		t.Error("trimOutput dropped the end of the stream, which is where the error is")
	}
	if !strings.HasPrefix(got, "... (truncated)") {
		t.Errorf("truncation is not signalled: %q", got[:40])
	}
}

func TestTrimOutputRescuesFailingLinesFromTheDroppedPart(t *testing.T) {
	// What oro:install prints: a long requirements table whose failing row is far from the end,
	// and a summary that says only how many requirements failed.
	var out strings.Builder
	out.WriteString("| ERROR   | PHP extension pdo_pgsql must be installed |\n")
	out.WriteString("| OK      | display_errors is disabled |\n")
	for out.Len() < outputLimit*2 {
		out.WriteString("| OK      | some other requirement is satisfied |\n")
	}
	out.WriteString("Found 1 not fulfilled requirement\n")

	got := trimOutput(out.String())

	if !strings.Contains(got, "pdo_pgsql must be installed") {
		t.Errorf("the failing row was dropped, so the report says nothing:\n%s", got)
	}
	if !strings.Contains(got, "Found 1 not fulfilled requirement") {
		t.Errorf("the end of the stream was dropped:\n%s", got)
	}
	// Satisfied rows are the bulk of the output and rescuing them would defeat the truncation.
	if strings.Contains(got[:strings.Index(got, "... (truncated)")], "display_errors") {
		t.Errorf("a passing row was rescued as a failure:\n%s", got)
	}
	if len(got) > outputLimit*2 {
		t.Errorf("output is %d bytes, truncation is not doing its job", len(got))
	}
}
