# `--agent` Output Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a global `--agent` flag that reduces every Orobox command's output to payload plus errors, with `orobox qa --agent` and `orobox test --agent` printing compact one-line findings instead of the QA tools' full human-readable output.

**Architecture:** A new dependency-free `internal/output` package owns the mode and the error writer. The existing `utils.Print*` helpers consult it, so all 290 existing call sites stay untouched. `orobox qa` and `orobox test` gain agent-mode formatters that reuse the machine-readable report machinery already built for `--report=gitlab`, parsing the tools' JSON into one line per finding.

**Tech Stack:** Go 1.x, cobra, viper, `go test ./...`, golangci-lint. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-17-agent-output-mode-design.md`

## Execution status (2026-09-17)

Tasks 1-12 are implemented and verified: `go build ./...`, `go vet ./...` and `go test ./...` are
all clean, and `gofmt -l .` reports nothing. Nothing has been committed — the repository owner does
every git operation.

Two items remain:

- **Task 13 (e2e)** — `assertQaAgent` is written, compiles and vets, and is called from the matrix.
  `make e2e` has **not** been run: it provisions a full OroCommerce install per matrix case and the
  Makefile allows it six hours.
- **Task 14 (fix-mode classification)** — **not started.** It needs a working box with the QA tools
  installed inside the container, which only a real install provides. Until it is done,
  `twig-cs-fixer` stays out of `appliedFixReporters`, so its findings are listed rather than
  counted as fixes — the safe default.

One deviation from the plan as written: the chrome conversion in Task 10 uses two new helpers,
`utils.PrintPlain` and `utils.PrintPlainf`, instead of `utils.PrintInfo`. They write a line verbatim
through the same agent-mode gate. Routing the URL and credential blocks through `PrintInfo` would
have prepended `ℹ ` to every line and changed what every existing user sees, which the plan's own
"non-agent output must be byte-identical" constraint forbids. The spec records this too.

Task 10 also found six `Warning: could not read template ...` calls in `internal/docker/compose.go`
that the plan's per-file catalogue missed. They are chrome and were converted with the rest.

---

## Global Constraints

- **Never run git commands.** This repository's owner performs every commit, branch and push themselves. Where this plan says "commit checkpoint", stop and hand the working tree back — do not stage, commit or push.
- Work directly in the working tree on `main`. Do not create branches or worktrees.
- No new third-party dependencies.
- `--agent` is a flag only: not bound to viper, not readable from an environment variable, never auto-detected from a non-TTY stdout.
- `--debug` wins over `--agent` when both are passed.
- Agent-mode stdout carries payload only; agent-mode errors go to stderr, one line each, prefixed `error: `, with no ANSI colour.
- A clean `orobox qa --agent` writes zero bytes and exits 0.
- Exit codes do not change from current behaviour.
- Existing non-agent output must be byte-identical after every task. The e2e harness greps for the `✘` glyph that `utils.PrintError` writes (`e2e/support.go:154`); changing default-mode output breaks it.
- Run `go test ./...` and `golangci-lint run ./...` before every commit checkpoint.
- Every exported identifier gets a doc comment explaining *why*, matching the density of the surrounding code. This codebase comments heavily and the comments carry reasoning, not restatement.

---

### Task 1: The `internal/output` package

**Files:**
- Create: `internal/output/output.go`
- Test: `internal/output/output_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `output.SetAgent(bool)`, `output.Agent() bool`, `output.Err(string)`, `output.Payload() io.Writer`, `output.SetWriters(payload, errw io.Writer) func()`.

This package must import nothing from the rest of the codebase. `internal/utils` is imported by `internal/docker`, `internal/pipeline`, `internal/certificates` and `cmd`; putting the mode flag there would make `utils` both the gate and the gated.

- [ ] **Step 1: Write the failing test**

Create `internal/output/output_test.go`:

```go
package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestErrWritesOneLinePerMessageLine(t *testing.T) {
	var payload, errw bytes.Buffer
	restore := SetWriters(&payload, &errw)
	defer restore()

	Err("QA tools reported errors or warnings.")
	Err("invalid config file .orobox.yaml:\nfield type is required")

	want := strings.Join([]string{
		"error: QA tools reported errors or warnings.",
		"error: invalid config file .orobox.yaml:",
		"error: field type is required",
		"",
	}, "\n")

	if got := errw.String(); got != want {
		t.Errorf("Err() wrote %q, want %q", got, want)
	}
	if payload.Len() != 0 {
		t.Errorf("Err() wrote %q to the payload stream, want nothing", payload.String())
	}
}

func TestAgentDefaultsOff(t *testing.T) {
	if Agent() {
		t.Fatal("Agent() is true before SetAgent, want false")
	}
	SetAgent(true)
	defer SetAgent(false)
	if !Agent() {
		t.Error("Agent() is false after SetAgent(true)")
	}
}

func TestErrTrimsTrailingNewlines(t *testing.T) {
	var payload, errw bytes.Buffer
	restore := SetWriters(&payload, &errw)
	defer restore()

	Err("boom\n\n")

	if got, want := errw.String(), "error: boom\n"; got != want {
		t.Errorf("Err() wrote %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/output/...`
Expected: FAIL — the package does not exist (`no Go files in .../internal/output`).

- [ ] **Step 3: Write the implementation**

Create `internal/output/output.go`:

```go
// Package output decides how much of Orobox's own output reaches the caller.
//
// It exists as its own package rather than as a flag inside internal/utils because utils is what
// every other package prints through: a mode flag living there would make the same package both
// the gate and the thing being gated, and internal/docker, internal/pipeline and cmd would all be
// importing the gate to ask whether they are allowed to print.
//
// Nothing here imports anything of Orobox's own, which is what lets utils import it.
package output

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// agent is process-global because the mode is decided once, from a persistent flag, before any
// command runs. Threading it through every call site would touch ~290 of them to say the same
// thing at each.
var agent bool

var (
	payloadWriter io.Writer = os.Stdout
	errWriter     io.Writer = os.Stderr
)

// SetAgent records whether this process runs for an automated caller. Called once, from the root
// command's PersistentPreRun.
func SetAgent(on bool) { agent = on }

// Agent reports whether output must stay minimal.
func Agent() bool { return agent }

// Payload is the stream a command's actual result goes to — QA findings, test failures, the
// verbatim output of a passthrough command. Never diagnostics.
func Payload() io.Writer { return payloadWriter }

// SetWriters redirects both streams and returns a function restoring them, for tests.
func SetWriters(payload, errs io.Writer) func() {
	prevPayload, prevErr := payloadWriter, errWriter
	payloadWriter, errWriter = payload, errs
	return func() { payloadWriter, errWriter = prevPayload, prevErr }
}

// Err writes one diagnostic to stderr, one line per line of the message.
//
// Multi-line messages are real: a config validation failure arrives as the file name followed by
// each invalid field. Prefixing every line keeps a caller that splits on newlines from mistaking
// the continuation for payload.
func Err(msg string) {
	for _, line := range strings.Split(strings.TrimRight(msg, "\n"), "\n") {
		fmt.Fprintf(errWriter, "error: %s\n", line)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/output/...`
Expected: PASS

- [ ] **Step 5: Lint**

Run: `golangci-lint run ./internal/output/...`
Expected: no findings.

- [ ] **Step 6: Commit checkpoint**

Stop. Report the new package and its tests to the repository owner, who commits. Do not run git.

---

### Task 2: Gate the print helpers

**Files:**
- Modify: `internal/utils/ui.go`
- Test: `internal/utils/ui_test.go`

**Interfaces:**
- Consumes: `output.Agent()`, `output.Err(string)`, `output.SetWriters(...)` from Task 1.
- Produces: `utils.SetWriter(io.Writer) func()` — redirects the helpers' stream for tests.

The six helpers currently call `fmt.Printf` directly, which cannot be captured. Route them through a package-level writer first, then add the gate.

- [ ] **Step 1: Write the failing test**

Append to `internal/utils/ui_test.go`:

```go
func TestPrintHelpersAreSilentInAgentMode(t *testing.T) {
	var human bytes.Buffer
	restoreUtils := SetWriter(&human)
	defer restoreUtils()

	var payload, errs bytes.Buffer
	restoreOutput := output.SetWriters(&payload, &errs)
	defer restoreOutput()

	output.SetAgent(true)
	defer output.SetAgent(false)

	PrintSuccess("All selected QA tools passed!")
	PrintInfo("Running QA tools...")
	PrintWarning("No QA tools enabled.")
	PrintTitle("Summary")
	StartLoader("Starting the containers")
	StopLoader()
	PrintError("QA tools reported errors or warnings.")

	if human.Len() != 0 {
		t.Errorf("agent mode wrote %q to the human stream, want nothing", human.String())
	}
	if payload.Len() != 0 {
		t.Errorf("agent mode wrote %q to the payload stream, want nothing", payload.String())
	}
	if got, want := errs.String(), "error: QA tools reported errors or warnings.\n"; got != want {
		t.Errorf("agent mode wrote %q to stderr, want %q", got, want)
	}
}

func TestPrintHelpersKeepHumanOutputWhenAgentModeIsOff(t *testing.T) {
	var human bytes.Buffer
	restore := SetWriter(&human)
	defer restore()

	PrintSuccess("done")
	PrintError("boom")

	got := human.String()
	if !strings.Contains(got, "✔ done") {
		t.Errorf("PrintSuccess wrote %q, want it to contain %q", got, "✔ done")
	}
	if !strings.Contains(got, "✘ boom") {
		t.Errorf("PrintError wrote %q, want it to contain %q", got, "✘ boom")
	}
}
```

Add `"bytes"` and `"github.com/algoritma-dev/orobox/internal/output"` to the test file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/utils/ -run TestPrintHelpers -v`
Expected: FAIL — `undefined: SetWriter`.

- [ ] **Step 3: Route the helpers through a writer**

In `internal/utils/ui.go`, add above `PrintSuccess`:

```go
// out is where the human-facing helpers write. It is a variable so tests can capture what a
// command printed, which is the only way to assert that agent mode printed nothing.
var out io.Writer = os.Stdout

// SetWriter redirects the print helpers and returns a function restoring the previous writer.
func SetWriter(w io.Writer) func() {
	prev := out
	out = w
	return func() { out = prev }
}
```

Replace `fmt.Printf(...)` with `fmt.Fprintf(out, ...)` in `PrintSuccess`, `PrintError`, `PrintWarning`, `PrintInfo` and `PrintTitle`, and the two `fmt.Print`/`fmt.Printf` calls inside `StartLoader` with `fmt.Fprint(out, ...)` / `fmt.Fprintf(out, ...)`. Leave the `Ask*` functions alone — Task 4 handles those.

- [ ] **Step 4: Add the gate**

```go
// PrintSuccess prints a success message in green. Agent mode drops it: a caller that reads the
// exit code does not need to be told the exit code was zero.
func PrintSuccess(message string) {
	if output.Agent() {
		return
	}
	fmt.Fprintf(out, "%s✔ %s%s\n", colorGreen, message, colorReset)
}

// PrintError prints an error message in red. Agent mode is the one case that still prints: an
// error is the only thing an automated caller cannot reconstruct from the exit code.
func PrintError(message string) {
	if output.Agent() {
		output.Err(message)
		return
	}
	fmt.Fprintf(out, "%s✘ %s%s\n", colorRed, message, colorReset)
}
```

Apply the same early return to `PrintWarning`, `PrintInfo` and `PrintTitle`. In `StartLoader`, return immediately after the `loaderMu` unlock when `output.Agent()` is true, before the `stdoutIsTerminal` branch, so no spinner goroutine starts and no fallback message prints.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/utils/...`
Expected: PASS, including the pre-existing `TestLoader`.

- [ ] **Step 6: Verify nothing else regressed**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit checkpoint**

Stop and hand the tree to the repository owner.

---

### Task 3: The `--agent` flag

**Files:**
- Modify: `cmd/root.go:20-47`
- Test: `cmd/root_test.go`

**Interfaces:**
- Consumes: `output.SetAgent(bool)` from Task 1.
- Produces: the `--agent` persistent flag; `output.Agent()` returns true for the rest of the process when it is passed without `--debug`.

- [ ] **Step 1: Write the failing test**

Append to `cmd/root_test.go`:

```go
func TestAgentFlagSetsAgentMode(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"absent", []string{"help"}, false},
		{"present", []string{"--agent", "help"}, true},
		{"debug wins", []string{"--agent", "--debug", "help"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer output.SetAgent(false)
			defer rootCmd.SetArgs(nil)
			defer viper.Set("debug", false)

			rootCmd.SetArgs(tc.args)
			rootCmd.SetOut(io.Discard)
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("rootCmd.Execute() failed: %v", err)
			}
			if got := output.Agent(); got != tc.want {
				t.Errorf("output.Agent() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAgentFlagIgnoresTheEnvironment(t *testing.T) {
	t.Setenv("ORO_AGENT", "1")
	defer output.SetAgent(false)
	defer rootCmd.SetArgs(nil)

	rootCmd.SetArgs([]string{"help"})
	rootCmd.SetOut(io.Discard)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}
	if output.Agent() {
		t.Error("ORO_AGENT switched agent mode on; the flag must be the only way in")
	}
}
```

The `help` **subcommand** is used, not the `--help` **flag**: cobra short-circuits the flag before `PersistentPreRun` ever runs, so a test using it would pass without the wiring existing. The subcommand reaches `PersistentPreRun` and needs neither a configured project nor a running Docker stack.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ -run TestAgentFlag -v`
Expected: FAIL — `unknown flag: --agent`.

- [ ] **Step 3: Add the flag**

In `cmd/root.go`, inside `init()`:

```go
	// Deliberately not bound to viper. Binding is what exposes a setting to AutomaticEnv under the
	// ORO_ prefix, and agent mode must never switch on because of an inherited environment: a
	// developer whose shell exported it once would get silent commands for the rest of the day.
	rootCmd.PersistentFlags().Bool("agent", false,
		"minimal output for automated callers: payload and errors only (ignored with --debug)")
```

In `PersistentPreRun`, before the existing `ConfigError` check:

```go
		// --debug wins. It exists to show everything, and silently discarding it would be a worse
		// surprise than ignoring the flag that asked for silence.
		agent, _ := cmd.Flags().GetBool("agent")
		output.SetAgent(agent && !viper.GetBool("debug"))
```

`cmd.Flags()` rather than `rootCmd.PersistentFlags()`: cobra merges inherited flags into the executing command, so this reads the value whether `--agent` came before or after the subcommand name.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ -run TestAgentFlag -v`
Expected: PASS

- [ ] **Step 5: Verify the whole suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit checkpoint**

Stop and hand the tree to the repository owner.

---

### Task 4: Non-interactive prompts

**Files:**
- Modify: `internal/utils/ui.go` (`AskQuestion`, `AskQuestionOrEOF`, `AskYesNo`, `AskSelection`)
- Test: `internal/utils/ui_test.go`

**Interfaces:**
- Consumes: `output.Agent()`.
- Produces: `Ask*` return their default without reading or printing in agent mode; `AskQuestionOrEOF` reports `eof == true` so existing required-value loops terminate.

The `eof` return is the mechanism that already exists for non-interactive runs — `AskQuestionOrEOF`'s doc comment describes it. Agent mode reuses it rather than inventing a second path.

- [ ] **Step 1: Write the failing test**

Append to `internal/utils/ui_test.go`:

```go
func TestAskFunctionsTakeDefaultsInAgentMode(t *testing.T) {
	var human bytes.Buffer
	restoreUtils := SetWriter(&human)
	defer restoreUtils()

	output.SetAgent(true)
	defer output.SetAgent(false)

	// A reader that would supply an answer, to prove it is never read.
	reader := bufio.NewReader(strings.NewReader("typed answer\ny\n2\n"))

	if got := AskQuestion(reader, "Bundle name", "acme"); got != "acme" {
		t.Errorf("AskQuestion() = %q, want the default %q", got, "acme")
	}
	if got, eof := AskQuestionOrEOF(reader, "Bundle name", "acme"); got != "acme" || !eof {
		t.Errorf("AskQuestionOrEOF() = (%q, %v), want (%q, true)", got, eof, "acme")
	}
	if got := AskYesNo(reader, "Overwrite?", false); got != false {
		t.Errorf("AskYesNo() = %v, want the default false", got)
	}
	if got := AskSelection(reader, "Type", []string{"project", "bundle"}, "bundle"); got != "bundle" {
		t.Errorf("AskSelection() = %q, want the default %q", got, "bundle")
	}
	if human.Len() != 0 {
		t.Errorf("agent mode printed the prompts: %q", human.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/utils/ -run TestAskFunctions -v`
Expected: FAIL — the functions print their prompts and consume `"typed answer"`.

- [ ] **Step 3: Implement**

Add an early return at the top of each, before any printing:

```go
func AskQuestionOrEOF(reader *bufio.Reader, question string, defaultValue string) (string, bool) {
	// Agent mode never prompts. The eof result is what a caller looping for a required value
	// tests, so returning true here ends that loop with the same "nothing more is coming" answer
	// a closed stdin gives — see SkipPrompts.
	if output.Agent() {
		return defaultValue, true
	}
	fmt.Fprintf(out, "%s%s%s [%s]: ", colorCyan, question, colorReset, defaultValue)
	...
}
```

`AskYesNo` returns `defaultValue`, `AskSelection` returns `defaultValue`. `AskQuestion` needs nothing: it already delegates to `AskQuestionOrEOF`.

Convert the remaining `fmt.Printf` calls in these four functions to `fmt.Fprintf(out, ...)` so the test's assertion about printing is meaningful.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/utils/...`
Expected: PASS

- [ ] **Step 5: Find the required-value callers**

Run: `grep -rn --include='*.go' "AskQuestionOrEOF" cmd internal`

For each caller, confirm what happens when the default is empty. Where a command cannot proceed without the value, it must already print an error and exit — verify that error names the flag or config key to set, as the spec requires, and reword it if it does not. Do not add new validation: report any caller that would proceed with an empty required value rather than fixing it silently, since that is a pre-existing bug with its own blast radius.

- [ ] **Step 6: Run the whole suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit checkpoint**

Stop and hand the tree to the repository owner.

---

### Task 5: `report.Findings`

**Files:**
- Create: `internal/report/findings.go`
- Test: `internal/report/findings_test.go`

**Interfaces:**
- Consumes: `ToolReport`, `PathPrefix`, `toolIssues`, `rewriteIssuePath` — all existing, in `internal/report/codequality.go`.
- Produces:
  - `type Finding struct { Tool, Path, Check, Severity, Message string; Line int }`
  - `func Findings(reports []ToolReport, prefix PathPrefix) ([]Finding, error)`

`MergeCodeQuality` cannot serve this: it flattens every tool's issues into one list and keeps only per-tool counts, so tool attribution is gone by the time it returns. Both functions stay; `MergeCodeQuality` still backs `--report`.

- [ ] **Step 1: Write the failing test**

Create `internal/report/findings_test.go`:

```go
package report

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindingsCarriesToolAttributionAndSorts(t *testing.T) {
	reports := []ToolReport{
		{Tool: "eslint", Data: []byte(`[{"check_name":"semi","description":"Missing semicolon.","severity":"major","location":{"path":"/var/www/oro/src/b.js","lines":{"begin":11}}}]`)},
		{Tool: "phpstan", Data: []byte(`[{"description":"Access to undefined property.","severity":"blocker","location":{"path":"/var/www/oro/src/a.php","lines":{"begin":42}}}]`)},
	}

	got, err := Findings(reports, PathPrefix{ContainerRoot: "/var/www/oro"})
	if err != nil {
		t.Fatalf("Findings() failed: %v", err)
	}

	want := []Finding{
		{Tool: "phpstan", Path: "src/a.php", Line: 42, Severity: "blocker", Message: "Access to undefined property."},
		{Tool: "eslint", Path: "src/b.js", Line: 11, Check: "semi", Severity: "major", Message: "Missing semicolon."},
	}
	if len(got) != len(want) {
		t.Fatalf("Findings() returned %d findings, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("finding %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFindingsCollapsesMultiLineMessages(t *testing.T) {
	reports := []ToolReport{
		{Tool: "phpstan", Data: []byte(`[{"description":"Parameter #1 $x\n  expects int,\n  string given.","location":{"path":"a.php","lines":{"begin":1}}}]`)},
	}

	got, err := Findings(reports, PathPrefix{})
	if err != nil {
		t.Fatalf("Findings() failed: %v", err)
	}
	want := "Parameter #1 $x expects int, string given."
	if got[0].Message != want {
		t.Errorf("Message = %q, want %q", got[0].Message, want)
	}
}

func TestFindingsDefaultsSeverityWhenTheToolOmitsIt(t *testing.T) {
	reports := []ToolReport{
		{Tool: "twig-cs-fixer", Data: []byte(`[{"description":"d","location":{"path":"a.twig","lines":{"begin":2}}}]`)},
	}

	got, err := Findings(reports, PathPrefix{})
	if err != nil {
		t.Fatalf("Findings() failed: %v", err)
	}
	if got[0].Severity != "info" {
		t.Errorf("Severity = %q, want %q", got[0].Severity, "info")
	}
}

func TestFindingsReadsEveryCapturedFixture(t *testing.T) {
	for _, tool := range []string{"phpstan", "rector", "php-cs-fixer", "twig-cs-fixer", "eslint", "stylelint"} {
		t.Run(tool, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tool+".json"))
			if err != nil {
				t.Fatalf("could not read the fixture: %v", err)
			}

			got, err := Findings([]ToolReport{{Tool: tool, Data: data}}, PathPrefix{ContainerRoot: "/var/www/oro"})
			if err != nil {
				t.Fatalf("Findings() failed on the real %s output: %v", tool, err)
			}
			if len(got) == 0 {
				t.Fatalf("Findings() read no findings from the real %s output", tool)
			}
			for i, f := range got {
				if f.Tool != tool {
					t.Errorf("finding %d has tool %q, want %q", i, f.Tool, tool)
				}
				if f.Path == "" {
					t.Errorf("finding %d has no path", i)
				}
				if f.Message == "" {
					t.Errorf("finding %d has no message", i)
				}
			}
		})
	}
}
```

The fixture test is the important one: those six files are the tools' real output, captured from an OroCommerce 5.1 project (see `internal/report/testdata/NOTES.md`). Rector's is its own JSON format, converted by `rector.go` — `Findings` gets that for free by reusing `toolIssues`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/report/ -run TestFindings -v`
Expected: FAIL — `undefined: Finding`, `undefined: Findings`.

- [ ] **Step 3: Implement**

Create `internal/report/findings.go`:

```go
package report

import (
	"sort"
	"strings"
)

// Finding is one violation flattened to what a single output line needs.
//
// It is a struct rather than the map[string]any the merge carries because this is the end of the
// road: nothing downstream re-serialises it, so the fields a tool invented and Orobox has no
// opinion about are genuinely not needed here — unlike in MergeCodeQuality, where dropping them
// would corrupt the document GitLab reads.
type Finding struct {
	Tool     string
	Path     string
	Check    string
	Severity string
	Message  string
	Line     int
}

// Findings reads every tool's report into a flat, sorted list that keeps the tool's name.
//
// MergeCodeQuality cannot be reused: it concatenates the issues into one document and keeps only
// per-tool counts, so by the time it returns there is no way to say which tool reported which
// finding — which is the first thing a caller reading one line per finding needs.
func Findings(reports []ToolReport, prefix PathPrefix) ([]Finding, error) {
	var out []Finding

	for _, report := range reports {
		if len(strings.TrimSpace(string(report.Data))) == 0 {
			continue
		}

		issues, err := toolIssues(report)
		if err != nil {
			return nil, err
		}

		for _, issue := range issues {
			rewriteIssuePath(issue, prefix)
			out = append(out, findingFrom(report.Tool, issue))
		}
	}

	// Sorted by location so two runs over an unchanged tree produce identical output, which is
	// what lets a caller diff them.
	sort.SliceStable(out, func(i, j int) bool {
		switch {
		case out[i].Path != out[j].Path:
			return out[i].Path < out[j].Path
		case out[i].Line != out[j].Line:
			return out[i].Line < out[j].Line
		default:
			return out[i].Tool < out[j].Tool
		}
	})

	return out, nil
}

// findingFrom reads one CodeClimate issue. Every field is optional: the tools agree on the shape
// but not on which parts of it they fill in, and a finding missing its severity is still a finding
// worth printing.
func findingFrom(tool string, issue map[string]any) Finding {
	f := Finding{Tool: tool, Severity: "info"}

	if s, ok := issue["severity"].(string); ok && s != "" {
		f.Severity = s
	}
	if s, ok := issue["check_name"].(string); ok {
		f.Check = s
	}
	if s, ok := issue["description"].(string); ok {
		f.Message = collapseWhitespace(s)
	}

	location, ok := issue["location"].(map[string]any)
	if !ok {
		return f
	}
	if s, ok := location["path"].(string); ok {
		f.Path = s
	}
	f.Line = issueLine(location)

	return f
}

// issueLine reads the begin line from either shape CodeClimate allows. The captured fixtures use
// `lines`, but the `positions` form is equally valid and a tool upgrade can switch between them.
func issueLine(location map[string]any) int {
	if lines, ok := location["lines"].(map[string]any); ok {
		if begin, ok := lines["begin"].(float64); ok {
			return int(begin)
		}
	}
	positions, ok := location["positions"].(map[string]any)
	if !ok {
		return 0
	}
	begin, ok := positions["begin"].(map[string]any)
	if !ok {
		return 0
	}
	if line, ok := begin["line"].(float64); ok {
		return int(line)
	}
	return 0
}

// collapseWhitespace folds a message onto one line.
//
// PHPStan in particular wraps long messages, and a finding that spans three lines breaks the one
// finding, one line contract the agent-mode grammar rests on.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/report/ -run TestFindings -v`
Expected: PASS, all six fixture subtests included.

- [ ] **Step 5: Verify the merge still works**

Run: `go test ./internal/report/...`
Expected: PASS — `MergeCodeQuality`'s own tests are untouched.

- [ ] **Step 6: Commit checkpoint**

Stop and hand the tree to the repository owner.

---

### Task 6: The agent-mode QA format

**Files:**
- Create: `internal/report/agent.go`
- Test: `internal/report/agent_test.go`

**Interfaces:**
- Consumes: `Finding` from Task 5.
- Produces:
  - `const AgentFindingLimit = 50`
  - `func FormatAgent(findings []Finding, fixed map[string]int, limit int) string`

Pure formatting, no I/O, so the grammar is testable without Docker, a container or a command.

- [ ] **Step 1: Write the failing test**

Create `internal/report/agent_test.go`:

```go
package report

import "testing"

func TestFormatAgentGrammar(t *testing.T) {
	findings := []Finding{
		{Tool: "phpstan", Path: "src/Entity/Order.php", Line: 42, Severity: "error", Message: "Access to undefined property App\\Entity\\Order::$foo"},
		{Tool: "eslint", Path: "src/Form/Type/OrderType.php", Line: 11, Check: "semi", Severity: "major", Message: "Missing semicolon."},
	}
	fixed := map[string]int{"php-cs-fixer": 5, "rector": 2}

	want := "fixed 7 files (php-cs-fixer 5, rector 2)\n" +
		"src/Entity/Order.php:42 error phpstan Access to undefined property App\\Entity\\Order::$foo\n" +
		"src/Form/Type/OrderType.php:11 major eslint semi Missing semicolon.\n"

	if got := FormatAgent(findings, fixed, AgentFindingLimit); got != want {
		t.Errorf("FormatAgent() =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatAgentPrintsNothingForACleanRun(t *testing.T) {
	if got := FormatAgent(nil, nil, AgentFindingLimit); got != "" {
		t.Errorf("FormatAgent() = %q, want the empty string", got)
	}
	if got := FormatAgent(nil, map[string]int{"rector": 0}, AgentFindingLimit); got != "" {
		t.Errorf("FormatAgent() with only zero counts = %q, want the empty string", got)
	}
}

func TestFormatAgentTruncates(t *testing.T) {
	var findings []Finding
	for i := 0; i < 53; i++ {
		findings = append(findings, Finding{Tool: "phpstan", Path: "a.php", Line: i, Severity: "error", Message: "m"})
	}

	got := FormatAgent(findings, nil, 50)

	lines := splitLines(got)
	if len(lines) != 51 {
		t.Fatalf("FormatAgent() produced %d lines, want 51 (50 findings plus the truncation line)", len(lines))
	}
	if want := "... 3 more (--report=gitlab for the full list)"; lines[50] != want {
		t.Errorf("last line = %q, want %q", lines[50], want)
	}
}

func TestFormatAgentOmitsTheLineNumberWhenTheToolReportedNone(t *testing.T) {
	findings := []Finding{{Tool: "rector", Path: "src/a.php", Severity: "minor", Message: "m"}}

	want := "src/a.php minor rector m\n"
	if got := FormatAgent(findings, nil, AgentFindingLimit); got != want {
		t.Errorf("FormatAgent() = %q, want %q", got, want)
	}
}
```

Add a `splitLines` helper to the test file:

```go
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
```

with `"strings"` imported.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/report/ -run TestFormatAgent -v`
Expected: FAIL — `undefined: FormatAgent`.

- [ ] **Step 3: Implement**

Create `internal/report/agent.go`:

```go
package report

import (
	"fmt"
	"sort"
	"strings"
)

// AgentFindingLimit caps how many findings an agent-mode run prints.
//
// The cap exists because the whole point of the mode is the caller's token budget: a first run
// against an unanalysed tree can report thousands of findings, and an agent cannot act on the
// four-hundredth one before it has fixed the first. The count of what was dropped is printed, so
// nothing is silently lost.
const AgentFindingLimit = 50

// FormatAgent renders a QA run as the lines an automated caller reads.
//
// The grammar is one finding per line, `path:line severity tool [check] message`, preceded by a
// single line counting what the fixers already rewrote. A clean run renders to the empty string
// rather than to a success message: an exit code already says that, and printing it costs the
// caller tokens to learn nothing.
func FormatAgent(findings []Finding, fixed map[string]int, limit int) string {
	var b strings.Builder

	if line := fixedLine(fixed); line != "" {
		b.WriteString(line)
		b.WriteString("\n")
	}

	shown := findings
	if limit > 0 && len(findings) > limit {
		shown = findings[:limit]
	}
	for _, f := range shown {
		b.WriteString(formatFinding(f))
		b.WriteString("\n")
	}
	if dropped := len(findings) - len(shown); dropped > 0 {
		fmt.Fprintf(&b, "... %d more (--report=gitlab for the full list)\n", dropped)
	}

	return b.String()
}

// fixedLine summarises what the fixers rewrote, or returns "" when they rewrote nothing.
//
// The tools are named with their counts rather than the files listed, because the caller's next
// move is the same whatever the file names are — re-read the tree — and the names would cost a
// line each to say so.
func fixedLine(fixed map[string]int) string {
	total := 0
	tools := make([]string, 0, len(fixed))
	for tool, count := range fixed {
		if count == 0 {
			continue
		}
		total += count
		tools = append(tools, fmt.Sprintf("%s %d", tool, count))
	}
	if total == 0 {
		return ""
	}
	sort.Strings(tools)
	return fmt.Sprintf("fixed %d files (%s)", total, strings.Join(tools, ", "))
}

// formatFinding renders one line. The line number is omitted rather than printed as `:0` when the
// tool reported none: a fabricated line one would send a reader to the wrong place.
func formatFinding(f Finding) string {
	location := f.Path
	if f.Line > 0 {
		location = fmt.Sprintf("%s:%d", f.Path, f.Line)
	}

	parts := []string{location, f.Severity, f.Tool}
	if f.Check != "" {
		parts = append(parts, f.Check)
	}
	if f.Message != "" {
		parts = append(parts, f.Message)
	}
	return strings.Join(parts, " ")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/report/ -run TestFormatAgent -v`
Expected: PASS

- [ ] **Step 5: Lint and run the suite**

Run: `golangci-lint run ./internal/report/... && go test ./...`
Expected: no findings, PASS.

- [ ] **Step 6: Commit checkpoint**

Stop and hand the tree to the repository owner.

---

### Task 7: Wire `orobox qa --agent`

**Files:**
- Create: `cmd/qa_agent.go`
- Create: `cmd/qa_agent_test.go`
- Modify: `cmd/qa.go` (`runQaOnCompose`, `runQaOnDagger`)

**Interfaces:**
- Consumes: `report.Findings`, `report.FormatAgent`, `report.AgentFindingLimit`, `report.Finding`, `output.Agent()`, `output.Err`, `output.Payload()`, `qatools.ToolStatusFile`, `qatools.StatusFile`.
- Produces:
  - `type qaAgentResult struct { Stdout string; Errors []string; Failed bool }`
  - `func qaAgentResultFrom(reports []report.ToolReport, statuses map[string]int, prefix report.PathPrefix, mode qatools.Mode) (qaAgentResult, error)`
  - `func finishQaAgent(rawDir, engine string, mode qatools.Mode, runErr error, captured []byte)`

The pure function is separate from the command because `cmd/qa.go` calls `os.Exit` on every failure path, which a test cannot survive.

- [ ] **Step 1: Write the failing test**

Create `cmd/qa_agent_test.go`:

```go
package cmd

import (
	"testing"

	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/algoritma-dev/orobox/internal/report"
)

func TestQaAgentResultCollapsesFixersAndListsTheRest(t *testing.T) {
	reports := []report.ToolReport{
		{Tool: "php-cs-fixer", Data: []byte(`[{"description":"no_unused_imports","location":{"path":"src/A.php","lines":{"begin":3}}},{"description":"braces","location":{"path":"src/B.php","lines":{"begin":9}}}]`)},
		{Tool: "phpstan", Data: []byte(`[{"description":"Undefined property.","severity":"error","location":{"path":"src/C.php","lines":{"begin":42}}}]`)},
	}
	statuses := map[string]int{"php-cs-fixer": 0, "phpstan": 1}

	got, err := qaAgentResultFrom(reports, statuses, report.PathPrefix{}, qatools.ModeFix)
	if err != nil {
		t.Fatalf("qaAgentResultFrom() failed: %v", err)
	}

	want := "fixed 2 files (php-cs-fixer 2)\nsrc/C.php:42 error phpstan Undefined property.\n"
	if got.Stdout != want {
		t.Errorf("Stdout = %q, want %q", got.Stdout, want)
	}
	if !got.Failed {
		t.Error("Failed = false, want true: a remaining PHPStan finding fails the run")
	}
	if len(got.Errors) != 0 {
		t.Errorf("Errors = %v, want none", got.Errors)
	}
}

func TestQaAgentResultListsFixersInCheckMode(t *testing.T) {
	reports := []report.ToolReport{
		{Tool: "php-cs-fixer", Data: []byte(`[{"description":"no_unused_imports","severity":"minor","location":{"path":"src/A.php","lines":{"begin":3}}}]`)},
	}
	statuses := map[string]int{"php-cs-fixer": 8}

	got, err := qaAgentResultFrom(reports, statuses, report.PathPrefix{}, qatools.ModeCheck)
	if err != nil {
		t.Fatalf("qaAgentResultFrom() failed: %v", err)
	}

	want := "src/A.php:3 minor php-cs-fixer no_unused_imports\n"
	if got.Stdout != want {
		t.Errorf("Stdout = %q, want %q: in check mode a fixer reports what it did not fix", got.Stdout, want)
	}
	if !got.Failed {
		t.Error("Failed = false, want true")
	}
}

func TestQaAgentResultReportsAToolThatCouldNotRun(t *testing.T) {
	reports := []report.ToolReport{{Tool: "phpstan", Data: []byte("")}}
	statuses := map[string]int{"phpstan": 255}

	got, err := qaAgentResultFrom(reports, statuses, report.PathPrefix{}, qatools.ModeFix)
	if err != nil {
		t.Fatalf("qaAgentResultFrom() failed: %v", err)
	}

	if got.Stdout != "" {
		t.Errorf("Stdout = %q, want nothing", got.Stdout)
	}
	if len(got.Errors) != 1 || got.Errors[0] != "phpstan could not run (exit 255)" {
		t.Errorf("Errors = %v, want one \"phpstan could not run (exit 255)\"", got.Errors)
	}
	if !got.Failed {
		t.Error("Failed = false, want true")
	}
}

func TestQaAgentResultIsSilentForACleanRun(t *testing.T) {
	reports := []report.ToolReport{
		{Tool: "phpstan", Data: []byte("[]")},
		{Tool: "php-cs-fixer", Data: []byte("")},
	}
	statuses := map[string]int{"phpstan": 0, "php-cs-fixer": 0}

	got, err := qaAgentResultFrom(reports, statuses, report.PathPrefix{}, qatools.ModeFix)
	if err != nil {
		t.Fatalf("qaAgentResultFrom() failed: %v", err)
	}

	if got.Stdout != "" || len(got.Errors) != 0 || got.Failed {
		t.Errorf("clean run produced %+v, want an empty, passing result", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ -run TestQaAgentResult -v`
Expected: FAIL — `undefined: qaAgentResultFrom`.

- [ ] **Step 3: Implement the pure part**

Create `cmd/qa_agent.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/algoritma-dev/orobox/internal/report"
)

// appliedFixReporters are the tools whose report, in fix mode, lists what they rewrote rather than
// what is left to fix.
//
// The distinction decides whether a finding fails the run. A file PHP-CS-Fixer already reformatted
// is not a problem the caller has to act on, and failing the command over it would make a passing
// tree unreachable: the next run would report the same file as fixed again only if it had been
// edited again.
//
// twig-cs-fixer is deliberately absent. Its --fix --report=gitlab behaviour has not been verified
// against the real tool, and the safe default for an unknown tool is to list its findings: a wrong
// guess then shows up as noise the developer can see, rather than as findings silently swallowed.
var appliedFixReporters = map[string]bool{
	"php-cs-fixer": true,
	"rector":       true,
}

// qaAgentResult is a formatted agent-mode run, kept separate from printing it so the grammar can
// be tested: every failure path in cmd/qa.go ends in os.Exit, which a test cannot survive.
type qaAgentResult struct {
	Stdout string
	Errors []string
	Failed bool
}

// qaAgentResultFrom turns the per-tool reports and their recorded exit codes into what the run
// prints.
//
// mode matters because the same tool means different things by the same document: in fix mode
// PHP-CS-Fixer's report lists the files it rewrote, in check mode the ones it would have.
func qaAgentResultFrom(reports []report.ToolReport, statuses map[string]int, prefix report.PathPrefix, mode qatools.Mode) (qaAgentResult, error) {
	var result qaAgentResult
	var listed []report.ToolReport
	fixed := map[string]int{}

	for _, r := range reports {
		// A tool that exited non-zero without writing findings did not fail the tree, it failed to
		// run — a missing binary, a broken config, a PHP fatal. Reported as an error, because the
		// caller's fix is not in the code under analysis.
		if status, ok := statuses[r.Tool]; ok && status != 0 && len(strings.TrimSpace(string(r.Data))) == 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("%s could not run (exit %d)", r.Tool, status))
			result.Failed = true
			continue
		}

		if mode == qatools.ModeFix && appliedFixReporters[r.Tool] {
			count, err := report.Findings([]report.ToolReport{r}, prefix)
			if err != nil {
				return qaAgentResult{}, err
			}
			fixed[r.Tool] = len(count)
			continue
		}

		listed = append(listed, r)
	}

	findings, err := report.Findings(listed, prefix)
	if err != nil {
		return qaAgentResult{}, err
	}
	if len(findings) > 0 {
		result.Failed = true
	}

	result.Stdout = report.FormatAgent(findings, fixed, report.AgentFindingLimit)
	return result, nil
}

// readToolStatuses reads the exit code ReportScript recorded for each tool. A missing file is not
// an error: it means the script never reached that tool, which the aggregate status already says.
func readToolStatuses(rawDir string, tools []string) map[string]int {
	statuses := map[string]int{}
	for _, tool := range tools {
		data, err := os.ReadFile(filepath.Join(rawDir, qatools.ToolStatusFile(tool)))
		if err != nil {
			continue
		}
		code, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			continue
		}
		statuses[tool] = code
	}
	return statuses
}

// finishQaAgent prints an agent-mode QA run and exits with its verdict.
//
// captured is the container's own stdout, which agent mode swallows: ReportScript echoes a header
// per tool and the JS linters print their human output there while writing JSON to a file (see
// internal/report/testdata/NOTES.md). It is written to stderr only when the engine itself failed,
// where it is the only diagnostic there is.
func finishQaAgent(rawDir, engine string, mode qatools.Mode, runErr error, captured []byte) {
	if runErr != nil {
		if len(captured) > 0 {
			output.Err(strings.TrimSpace(string(captured)))
		}
		output.Err(runErr.Error())
		os.Exit(1)
	}

	reports, tools, err := readRawReports(rawDir)
	if err != nil {
		output.Err(err.Error())
		os.Exit(1)
	}

	result, err := qaAgentResultFrom(reports, readToolStatuses(rawDir, tools), qaPathPrefix(engine), mode)
	if err != nil {
		output.Err(err.Error())
		os.Exit(1)
	}

	if result.Stdout != "" {
		fmt.Fprint(output.Payload(), result.Stdout)
	}
	for _, msg := range result.Errors {
		output.Err(msg)
	}

	if result.Failed {
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Extract the raw-report reader**

`finishQaReport` in `cmd/qa.go:487-508` already walks the raw directory reading `*.json` into `[]report.ToolReport`. Extract that loop into `readRawReports(rawDir string) ([]report.ToolReport, []string, error)` in `cmd/qa_agent.go`, returning the reports and their tool names, and have `finishQaReport` call it. Do not duplicate the loop — both paths must read the directory the same way, or a tool whose report one path skips becomes invisible in only one mode.

- [ ] **Step 5: Wire the compose path**

In `runQaOnCompose`, the format used to build the tools and the format the user asked for become two values:

```go
	// Agent mode needs the tools' machine-readable output whatever the caller asked for, so the
	// report machinery runs underneath it.
	effectiveFormat := format
	agentOnly := false
	if output.Agent() && format == qatools.ReportNone {
		effectiveFormat = qatools.ReportGitLab
		agentOnly = true
	}
```

Then:

- The `reportPath` / `containerReportDir` block keys off `effectiveFormat`, but `resolveReportPath` is called only when `!agentOnly` — an agent-mode run writes no merged document.
- **The mode derivation keeps testing `format`, not `effectiveFormat`.** This is the one non-additive change in the whole plan. The existing lines are:

```go
	mode := qatools.ModeFix
	if format != qatools.ReportNone {
		mode = qatools.ModeCheck
	}
```

They must stay on `format`. Switching them to `effectiveFormat` would silently make `orobox qa --agent` check-only, which is the opposite of the agreed behaviour: agent mode still lets the tools fix what they can. The `staged != nil` override below them is unaffected and stays.

- `qatools.Tools` receives `Report: effectiveFormat`, and the script choice becomes `if effectiveFormat != qatools.ReportNone { script = qatools.ReportScript(...) }`.
- In agent mode always append `-T` instead of consulting `isTTY()`: there is no terminal to allocate for an automated caller, and a TTY would let the tools re-enable colour.
- Run through `docker.RunComposeCommandWithOutput` rather than `docker.RunComposeCommand`, keeping the returned bytes:

```go
	if output.Agent() {
		captured, runErr := docker.RunComposeCommandWithOutput(args...)
		if agentOnly {
			// The raw per-tool files were an implementation detail of this run, not something the
			// caller asked to keep.
			defer func() { _ = os.RemoveAll(rawReportDir("qa")) }()
		}
		finishQaAgent(rawReportDir("qa"), engineCompose, mode, runErr, captured)
		return
	}
```

placed after the existing baseline branch — `--generate-baseline --agent` writes its file, prints nothing through the already-gated helpers, and exits 0 on the existing path.

- [ ] **Step 6: Wire the dagger path**

In `runQaOnDagger`, apply the same `effectiveFormat` / `agentOnly` split, pass `effectiveFormat` into `pipeline.ChecksOptions.Report`, and replace the `finishQaReport` call with `finishQaAgent(result.QAReportDir, engineDagger, qatools.ModeCheck, runErr, nil)` when `output.Agent()` is set. The mode is `ModeCheck` unconditionally there: the Dagger engine already runs the tools check-only, as its own doc comment explains.

- [ ] **Step 7: Run the tests**

Run: `go test ./cmd/ -run 'TestQa' -v`
Expected: PASS, including the pre-existing `TestQaCommand` and `TestQaBaseline*`.

- [ ] **Step 8: Verify the default path is unchanged**

Run: `go test ./...`
Expected: PASS. Any failure in a non-agent test is a regression in default output, not a test to update.

- [ ] **Step 9: Commit checkpoint**

Stop and hand the tree to the repository owner.

---

### Task 8: `report.FailuresFromJUnit`

**Files:**
- Create: `internal/report/junitfailures.go`
- Create: `internal/report/testdata/junit-failures.xml`
- Test: `internal/report/junitfailures_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type TestFailure struct { Suite, Test, Kind, Message, File string; Line int }`
  - `type TestCounts struct { Passed, Failed, Errored int }`
  - `func FailuresFromJUnit(doc []byte) ([]TestFailure, TestCounts, error)`

`MergeJUnit` cannot be reused or extended: it deliberately keeps each suite's body as opaque `innerxml` so that no PHPUnit element is silently dropped on the way to GitLab. Reading failures needs the opposite — a typed view. Both live in the package; neither changes the other.

**A note against the spec:** the spec says PHPUnit reports a failure's file and line inside the failure body. PHPUnit's JUnit logger also writes `file` and `line` attributes on `<testcase>`, which is more reliable than parsing prose. This plan reads the attributes and leaves them zero when absent. Update the spec's sentence when this task lands.

- [ ] **Step 1: Write the fixture**

Create `internal/report/testdata/junit-failures.xml`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="Unit" tests="3" failures="1" errors="1">
    <testsuite name="Oro\Bundle\AcmeBundle\Tests\Unit\OrderTest" file="/var/www/oro/src/Acme/Tests/Unit/OrderTest.php" tests="3" failures="1" errors="1">
      <testcase name="testTotal" class="Oro\Bundle\AcmeBundle\Tests\Unit\OrderTest" file="/var/www/oro/src/Acme/Tests/Unit/OrderTest.php" line="88" assertions="1" time="0.001">
        <failure type="PHPUnit\Framework\ExpectationFailedException">Oro\Bundle\AcmeBundle\Tests\Unit\OrderTest::testTotal
Failed asserting that 2 matches expected 3.

/var/www/oro/src/Acme/Tests/Unit/OrderTest.php:88</failure>
      </testcase>
      <testcase name="testBroken" class="Oro\Bundle\AcmeBundle\Tests\Unit\OrderTest" file="/var/www/oro/src/Acme/Tests/Unit/OrderTest.php" line="95" assertions="0" time="0.001">
        <error type="Error">Call to a member function total() on null</error>
      </testcase>
      <testcase name="testPasses" class="Oro\Bundle\AcmeBundle\Tests\Unit\OrderTest" file="/var/www/oro/src/Acme/Tests/Unit/OrderTest.php" line="101" assertions="1" time="0.001"/>
    </testsuite>
  </testsuite>
</testsuites>
```

The nested `<testsuite>` is not decoration: PHPUnit nests one suite element per test class inside the configured suite, so a parser that only reads the top level finds no test cases at all.

- [ ] **Step 2: Write the failing test**

Create `internal/report/junitfailures_test.go`:

```go
package report

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFailuresFromJUnitReadsNestedSuites(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("testdata", "junit-failures.xml"))
	if err != nil {
		t.Fatalf("could not read the fixture: %v", err)
	}

	failures, counts, err := FailuresFromJUnit(doc)
	if err != nil {
		t.Fatalf("FailuresFromJUnit() failed: %v", err)
	}

	if len(failures) != 2 {
		t.Fatalf("read %d failures, want 2", len(failures))
	}

	first := failures[0]
	if first.Suite != "Oro\\Bundle\\AcmeBundle\\Tests\\Unit\\OrderTest" {
		t.Errorf("Suite = %q", first.Suite)
	}
	if first.Test != "testTotal" {
		t.Errorf("Test = %q, want %q", first.Test, "testTotal")
	}
	if first.Kind != "failure" {
		t.Errorf("Kind = %q, want %q", first.Kind, "failure")
	}
	if first.Message != "Failed asserting that 2 matches expected 3." {
		t.Errorf("Message = %q", first.Message)
	}
	if first.Line != 88 {
		t.Errorf("Line = %d, want 88", first.Line)
	}

	if failures[1].Kind != "error" {
		t.Errorf("second Kind = %q, want %q", failures[1].Kind, "error")
	}

	want := TestCounts{Passed: 1, Failed: 1, Errored: 1}
	if counts != want {
		t.Errorf("counts = %+v, want %+v", counts, want)
	}
}

func TestFailuresFromJUnitAcceptsABareTestsuiteRoot(t *testing.T) {
	doc := []byte(`<testsuite name="Unit" tests="1" failures="1"><testcase name="t" class="C" line="4"><failure>C::t
boom</failure></testcase></testsuite>`)

	failures, counts, err := FailuresFromJUnit(doc)
	if err != nil {
		t.Fatalf("FailuresFromJUnit() failed: %v", err)
	}
	if len(failures) != 1 || failures[0].Message != "boom" {
		t.Fatalf("failures = %+v, want one with the message %q", failures, "boom")
	}
	if counts.Failed != 1 || counts.Passed != 0 {
		t.Errorf("counts = %+v, want 1 failed and 0 passed", counts)
	}
}

func TestFailuresFromJUnitOnAnEmptyDocument(t *testing.T) {
	failures, counts, err := FailuresFromJUnit(nil)
	if err != nil {
		t.Fatalf("FailuresFromJUnit() failed on an empty document: %v", err)
	}
	if len(failures) != 0 || counts != (TestCounts{}) {
		t.Errorf("empty document produced %+v / %+v, want nothing", failures, counts)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/report/ -run TestFailuresFromJUnit -v`
Expected: FAIL — `undefined: FailuresFromJUnit`.

- [ ] **Step 4: Implement**

Create `internal/report/junitfailures.go`:

```go
package report

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// TestFailure is one failing test, flattened to what a single output line needs.
type TestFailure struct {
	Suite   string
	Test    string
	Kind    string // "failure" for an assertion, "error" for an exception
	Message string
	File    string
	Line    int
}

// TestCounts is the run's tally, which is what a passing run prints instead of nothing: a test run
// that reports no failures because it ran no tests is a different outcome from one that passed.
type TestCounts struct {
	Passed  int
	Failed  int
	Errored int
}

// parsedSuite is a typed view of a suite, used only here.
//
// MergeJUnit models the same elements as opaque innerxml on purpose, so that nothing PHPUnit or an
// extension emits is dropped from the document GitLab reads. That is the right trade there and the
// wrong one here, where the point is to read inside a test case.
type parsedSuite struct {
	Name   string       `xml:"name,attr"`
	Suites []parsedSuite `xml:"testsuite"`
	Cases  []parsedCase  `xml:"testcase"`
}

type parsedCase struct {
	Name     string         `xml:"name,attr"`
	Class    string         `xml:"class,attr"`
	File     string         `xml:"file,attr"`
	Line     int            `xml:"line,attr"`
	Failures []parsedResult `xml:"failure"`
	Errors   []parsedResult `xml:"error"`
}

type parsedResult struct {
	Body string `xml:",chardata"`
}

type parsedRoot struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []parsedSuite `xml:"testsuite"`
}

// FailuresFromJUnit reads the failing tests and the run's tally out of a JUnit document.
//
// An empty document is not an error: it is the shape of a run cancelled before PHPUnit wrote
// anything, the same case MergeJUnit tolerates.
func FailuresFromJUnit(doc []byte) ([]TestFailure, TestCounts, error) {
	trimmed := bytes.TrimSpace(doc)
	if len(trimmed) == 0 {
		return nil, TestCounts{}, nil
	}

	var root parsedRoot
	if err := xml.Unmarshal(trimmed, &root); err != nil {
		// A bare <testsuite> root is legal for a single-suite run, so it is retried before the
		// document is called invalid — the same fallback MergeJUnit makes.
		var single parsedSuite
		if singleErr := xml.Unmarshal(trimmed, &single); singleErr != nil {
			return nil, TestCounts{}, fmt.Errorf("the JUnit document is not valid XML: %w", err)
		}
		root.Suites = []parsedSuite{single}
	}

	var failures []TestFailure
	var counts TestCounts
	for _, suite := range root.Suites {
		collectFailures(suite, &failures, &counts)
	}
	return failures, counts, nil
}

// collectFailures walks a suite and its nested suites. PHPUnit nests one suite element per test
// class inside the configured suite, so a walk that stopped at the top level would find no cases.
func collectFailures(suite parsedSuite, failures *[]TestFailure, counts *TestCounts) {
	for _, nested := range suite.Suites {
		collectFailures(nested, failures, counts)
	}

	for _, c := range suite.Cases {
		switch {
		case len(c.Failures) > 0:
			counts.Failed++
			*failures = append(*failures, testFailure(c, "failure", c.Failures[0].Body))
		case len(c.Errors) > 0:
			counts.Errored++
			*failures = append(*failures, testFailure(c, "error", c.Errors[0].Body))
		default:
			counts.Passed++
		}
	}
}

// testFailure builds one entry, taking the file and line from the case's own attributes.
//
// PHPUnit repeats the location at the end of the failure body as prose, but the attributes are
// what it writes for every case, so they are the ones read here.
func testFailure(c parsedCase, kind, body string) TestFailure {
	return TestFailure{
		Suite:   c.Class,
		Test:    c.Name,
		Kind:    kind,
		Message: failureMessage(c, body),
		File:    c.File,
		Line:    c.Line,
	}
}

// failureMessage strips the two things PHPUnit wraps around the actual message: a first line
// repeating the test's own name, and a trailing file:line that the File and Line fields already
// carry.
func failureMessage(c parsedCase, body string) string {
	lines := strings.Split(strings.TrimSpace(body), "\n")

	header := c.Class + "::" + c.Name
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == header {
		lines = lines[1:]
	}
	for len(lines) > 0 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "" || (c.File != "" && strings.HasPrefix(last, c.File+":")) {
			lines = lines[:len(lines)-1]
			continue
		}
		break
	}

	return collapseWhitespace(strings.Join(lines, " "))
}
```

`collapseWhitespace` comes from `internal/report/findings.go` (Task 5) — same package, no import needed.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/report/ -run TestFailuresFromJUnit -v`
Expected: PASS

- [ ] **Step 6: Verify the merge still works**

Run: `go test ./internal/report/...`
Expected: PASS — `MergeJUnit` and its tests are untouched.

- [ ] **Step 7: Commit checkpoint**

Stop and hand the tree to the repository owner.

---

### Task 9: Wire `orobox test --agent`

**Files:**
- Create: `cmd/test_agent.go`
- Create: `cmd/test_agent_test.go`
- Modify: `cmd/test.go` (`runTestOnCompose`, `runTestOnDagger`)

**Interfaces:**
- Consumes: `report.FailuresFromJUnit`, `report.TestFailure`, `report.TestCounts`, `report.AgentFindingLimit`, `output.*`.
- Produces:
  - `func formatTestAgent(failures []report.TestFailure, counts report.TestCounts, limit int) string`
  - `func finishTestAgent(rawDir string, runErr error, captured []byte)`

- [ ] **Step 1: Write the failing test**

Create `cmd/test_agent_test.go`:

```go
package cmd

import (
	"testing"

	"github.com/algoritma-dev/orobox/internal/report"
)

func TestFormatTestAgent(t *testing.T) {
	failures := []report.TestFailure{
		{Suite: "Oro\\Bundle\\AcmeBundle\\Tests\\Unit\\OrderTest", Test: "testTotal", Kind: "failure", Message: "Failed asserting that 2 matches expected 3.", File: "/var/www/oro/src/Acme/Tests/Unit/OrderTest.php", Line: 88},
	}
	counts := report.TestCounts{Passed: 7, Failed: 1}

	want := "Oro\\Bundle\\AcmeBundle\\Tests\\Unit\\OrderTest::testTotal failure Failed asserting that 2 matches expected 3.\n" +
		"7 passed, 1 failed, 0 errors\n"

	if got := formatTestAgent(failures, counts, report.AgentFindingLimit); got != want {
		t.Errorf("formatTestAgent() =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatTestAgentPrintsOnlyTheTallyForAPassingRun(t *testing.T) {
	want := "12 passed, 0 failed, 0 errors\n"
	if got := formatTestAgent(nil, report.TestCounts{Passed: 12}, report.AgentFindingLimit); got != want {
		t.Errorf("formatTestAgent() = %q, want %q", got, want)
	}
}

func TestFormatTestAgentTruncates(t *testing.T) {
	var failures []report.TestFailure
	for i := 0; i < 52; i++ {
		failures = append(failures, report.TestFailure{Suite: "S", Test: "t", Kind: "failure", Message: "m"})
	}

	got := formatTestAgent(failures, report.TestCounts{Failed: 52}, 50)

	lines := splitAgentLines(got)
	if len(lines) != 52 {
		t.Fatalf("produced %d lines, want 52 (50 failures, the truncation line and the tally)", len(lines))
	}
	if want := "... 2 more"; lines[50] != want {
		t.Errorf("line 51 = %q, want %q", lines[50], want)
	}
}
```

Add to the same file:

```go
func splitAgentLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
```

with `"strings"` imported.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ -run TestFormatTestAgent -v`
Expected: FAIL — `undefined: formatTestAgent`.

- [ ] **Step 3: Implement**

Create `cmd/test_agent.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/report"
)

// formatTestAgent renders a PHPUnit run as the lines an automated caller reads: one per failing
// test, then the tally.
//
// The tally is printed even for a passing run, unlike the QA formatter's silence. A test run that
// reported nothing because it executed no tests is a different outcome from one that passed, and
// the exit code cannot tell the two apart.
func formatTestAgent(failures []report.TestFailure, counts report.TestCounts, limit int) string {
	var b strings.Builder

	shown := failures
	if limit > 0 && len(failures) > limit {
		shown = failures[:limit]
	}
	for _, f := range shown {
		fmt.Fprintf(&b, "%s::%s %s %s\n", f.Suite, f.Test, f.Kind, f.Message)
	}
	if dropped := len(failures) - len(shown); dropped > 0 {
		fmt.Fprintf(&b, "... %d more\n", dropped)
	}

	fmt.Fprintf(&b, "%d passed, %d failed, %d errors\n", counts.Passed, counts.Failed, counts.Errored)
	return b.String()
}

// finishTestAgent prints an agent-mode test run and exits with its verdict.
//
// runErr is PHPUnit's own non-zero exit on a failing suite, which is not a reason to print the
// captured output: the failures are in the JUnit log and printing both would double the cost. It
// is only surfaced when the log says nothing, which means the run never reached PHPUnit.
func finishTestAgent(rawDir string, runErr error, captured []byte) {
	doc, readErr := os.ReadFile(filepath.Join(rawDir, "junit.xml"))
	if readErr != nil {
		if len(captured) > 0 {
			output.Err(strings.TrimSpace(string(captured)))
		}
		output.Err(fmt.Sprintf("the test run wrote no JUnit log: %v", readErr))
		os.Exit(1)
	}

	failures, counts, err := report.FailuresFromJUnit(doc)
	if err != nil {
		output.Err(err.Error())
		os.Exit(1)
	}

	fmt.Fprint(output.Payload(), formatTestAgent(failures, counts, report.AgentFindingLimit))

	if len(failures) > 0 || runErr != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Wire the command**

In `runTestOnCompose` (`cmd/test.go:120-166`), mirror Task 7's shape: when `output.Agent()` and no `--report` was asked for, create `rawReportDir("test")` and append the `--log-junit` argument that the report branch already appends, run through `docker.RunComposeCommandWithOutput`, then call `finishTestAgent(rawReportDir("test"), err, captured)` and return. Remove the raw directory afterwards when the caller did not ask for a report.

Apply the same treatment to `runTestOnDagger` against its own report directory.

- [ ] **Step 5: Run the tests**

Run: `go test ./cmd/ -run 'TestFormatTestAgent|TestTest' -v`
Expected: PASS

- [ ] **Step 6: Run the whole suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit checkpoint**

Stop and hand the tree to the repository owner.

---

### Task 10: Convert the decorative raw prints

**Files:**
- Modify: `cmd/up.go` (24 calls), `cmd/deploy.go` (14), `cmd/test_init.go` (5), `internal/pipeline/run.go` (4), `internal/certificates/certs.go` (3), `cmd/run.go` (3), `cmd/ci_init.go` (2), `cmd/init.go` (1), `internal/docker/customimage.go` (1)
- Test: `cmd/agent_silence_test.go` (create)

**Interfaces:**
- Consumes: `utils.PrintInfo`, `utils.PrintSuccess`, `utils.PrintWarning`, `utils.SetWriter` (Task 2), `output.SetAgent` (Task 1).
- Produces: nothing new.

Roughly 55 calls. Each one is a judgement: is this line Orobox talking about itself, or is it the payload the caller asked for? A wrong call leaks a banner into agent output or swallows a diagnostic. The test in Step 1 is what catches the first kind; nothing but reading catches the second, so read each call's surroundings before converting it.

- [ ] **Step 1: Write the failing test**

Create `cmd/agent_silence_test.go`:

```go
package cmd

import (
	"bytes"
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/utils"
	"github.com/spf13/viper"
)

// TestAgentModeSilencesTheSilentCommands is the net under the conversion of the raw fmt.Print
// calls: a command that still writes its own chrome fails here rather than in a caller's context
// window.
func TestAgentModeSilencesTheSilentCommands(t *testing.T) {
	for _, args := range [][]string{{"up"}, {"down"}} {
		t.Run(args[0], func(t *testing.T) {
			oldRun := docker.RunComposeCommand
			oldSilently := docker.RunComposeCommandSilently
			oldWithOutput := docker.RunComposeCommandWithOutput
			t.Cleanup(func() {
				docker.RunComposeCommand = oldRun
				docker.RunComposeCommandSilently = oldSilently
				docker.RunComposeCommandWithOutput = oldWithOutput
				rootCmd.SetArgs(nil)
				output.SetAgent(false)
				docker.ResetEnsuredServices()
				viper.Set("type", nil)
			})

			docker.RunComposeCommand = func(string, ...string) error { return nil }
			docker.RunComposeCommandSilently = func(string, ...string) error { return nil }
			docker.RunComposeCommandWithOutput = func(a ...string) ([]byte, error) {
				if len(a) > 0 && a[0] == "ps" {
					return psRunningRequested(a), nil
				}
				return []byte("[]"), nil
			}
			viper.Set("type", "project")

			var human, stdout, stderr bytes.Buffer
			restoreUtils := utils.SetWriter(&human)
			defer restoreUtils()
			restoreOutput := output.SetWriters(&stdout, &stderr)
			defer restoreOutput()

			rootCmd.SetArgs(append([]string{"--agent"}, args...))
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("rootCmd.Execute() failed: %v", err)
			}

			if human.Len() != 0 {
				t.Errorf("%v printed %q in agent mode, want nothing", args, human.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("%v wrote %q to the payload stream, want nothing", args, stdout.String())
			}
		})
	}
}
```

`psRunningRequested` already exists in `cmd/qa_test.go`; reuse it rather than writing a second mock.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ -run TestAgentModeSilences -v`
Expected: FAIL — `up` writes its banner through `fmt.Print`, which no gate can see. The failure message shows the leaked text.

- [ ] **Step 3: Convert `cmd/up.go`**

Run `grep -n 'fmt\.Print' cmd/up.go` and convert each. Rules:

- A line stating what is about to happen or what happened → `utils.PrintInfo` / `utils.PrintSuccess`.
- A line the user is meant to act on but which is not a failure (a URL, a credential, a next step) → `utils.PrintInfo`.
- A blank `fmt.Println()` used as a spacer → delete it; the helpers own their own spacing and a bare newline in agent mode is a stray byte.
- Keep the text byte-identical otherwise. `PrintSuccess` prepends `✔ ` and a colour, so a line that already contains its own glyph loses it.

- [ ] **Step 4: Run the test**

Run: `go test ./cmd/ -run TestAgentModeSilences -v`
Expected: PASS for `up`.

- [ ] **Step 5: Convert the remaining eight files**

Same rules, one file at a time: `cmd/deploy.go`, `cmd/test_init.go`, `internal/pipeline/run.go`, `internal/certificates/certs.go`, `cmd/run.go`, `cmd/ci_init.go`, `cmd/init.go`, `internal/docker/customimage.go`.

`cmd/run.go` needs care: `run` is a passthrough command, so a print that renders the child process's own output must stay a raw write to stdout. Only Orobox's own framing around it converts.

- [ ] **Step 6: Verify the passthrough commands still pass output through**

`logs`, `shell`, `console` and `db` reach `docker.RunComposeCommand`, which writes the child's
stdout straight to `os.Stdout` and is deliberately untouched by this work. Their own framing goes
through the `utils` helpers and is therefore already silenced. Confirm both halves by reading each
command's `Run` function — no raw write may remain around the passthrough, and nothing may wrap the
child's stream.

Run: `grep -n 'fmt\.Print\|utils\.Print' cmd/logs.go cmd/shell.go cmd/console.go cmd/db.go`
Expected: only `utils.Print*` calls. `cmd/db.go` has uncommitted changes in the working tree — read
them before judging what belongs there.

- [ ] **Step 7: Verify no decorative prints remain**

Run: `grep -rn --include='*.go' 'fmt\.Print' cmd internal | grep -v _test.go`
Expected: only `internal/utils/ui.go` (the helpers and the prompts) and `internal/docker/compose.go` (the error passthrough, which Task 11 handles).

- [ ] **Step 8: Run the whole suite**

Run: `go test ./... && golangci-lint run ./...`
Expected: PASS, no findings.

- [ ] **Step 9: Commit checkpoint**

Stop and hand the tree to the repository owner.

---

### Task 11: Route the compose failure dumps to stderr

**Files:**
- Modify: `internal/docker/compose.go` (the failure branches of `RunComposeCommandSilently` and `RunSetupComposeCommandSilently`)
- Test: `internal/docker/compose_test.go`

**Interfaces:**
- Consumes: `output.Agent()`, `output.Err(string)`.
- Produces: nothing new.

These 11 `fmt.Print` calls are the one group that must keep printing in agent mode. They dump the failed command's own stderr and stdout, which is the only diagnostic a caller gets when a container cannot start. They move from stdout to stderr so an agent can separate them from payload.

- [ ] **Step 1: Write the failing test**

Append to `internal/docker/compose_test.go`:

```go
func TestSilentRunnerSendsAFailureDumpToStderrInAgentMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	restore := output.SetWriters(&stdout, &stderr)
	defer restore()

	output.SetAgent(true)
	defer output.SetAgent(false)

	// A command that cannot exist, so the runner takes its failure branch.
	err := RunComposeCommandSilently("", "this-subcommand-does-not-exist")
	if err == nil {
		t.Fatal("RunComposeCommandSilently() succeeded, want a failure")
	}

	if stdout.Len() != 0 {
		t.Errorf("the failure dump went to the payload stream: %q", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Error("the failure dump reached neither stream; the diagnostic is lost")
	}
}
```

This test shells out to `docker compose`. If the package's existing tests already skip without Docker, follow that pattern; if they do not, guard it with a `docker` lookup and `t.Skip`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/docker/ -run TestSilentRunnerSends -v`
Expected: FAIL — the dump lands in `stdout` (the process's real stdout, so the buffer stays empty and the assertion on `stderr` fails).

- [ ] **Step 3: Implement**

In each failure branch, replace the pair of `fmt.Print` calls:

```go
		// A failed command's own output is the only diagnostic there is, so agent mode keeps it —
		// on stderr, where a caller can tell it apart from the payload.
		if output.Agent() {
			if stderr.Len() > 0 {
				output.Err(stderr.String())
			}
			if stdout.Len() > 0 {
				output.Err(stdout.String())
			}
		} else {
			if stderr.Len() > 0 {
				fmt.Print(stderr.String())
			}
			if stdout.Len() > 0 {
				fmt.Print(stdout.String())
			}
		}
```

The registry-authentication hint below it already goes through `utils.PrintWarning`/`PrintInfo`, so Task 2 has silenced it in agent mode. That is correct: it is advice for a human, and the underlying `unauthorized` line is in the dump above.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/docker/ -run TestSilentRunnerSends -v`
Expected: PASS

- [ ] **Step 5: Run the whole suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit checkpoint**

Stop and hand the tree to the repository owner.

---

### Task 12: Documentation

**Files:**
- Modify: `docs/configuration.md`
- Modify: the QA documentation page (find it with `grep -rln 'orobox qa' docs/`)
- Modify: `README.md` if it lists the global flags
- Modify: `docs/superpowers/specs/2026-09-17-agent-output-mode-design.md` (the JUnit file/line correction from Task 8)

**Interfaces:** none.

`docs/configuration.md` already has uncommitted changes in the working tree from earlier work. Read it before editing and keep those changes intact.

- [ ] **Step 1: Document the flag**

Add an `--agent` section covering: what it suppresses, that errors go to stderr prefixed `error: `, that `--debug` wins, that it is a flag with no environment variable and no auto-detection, and that prompts take their defaults.

- [ ] **Step 2: Document the qa output grammar**

Give the line grammar, a worked example of the `fixed N files` line plus findings, the 50-finding cap and how to get the rest (`--report=gitlab`), and the exit-code rule: fixes alone exit 0, remaining findings or a tool that could not run exit 1.

- [ ] **Step 3: Correct the spec**

Replace the sentence claiming PHPUnit reports a failure's file and line in the failure body with what Task 8 established: the `<testcase>` element carries `file` and `line` attributes, which is what the implementation reads.

- [ ] **Step 4: Verify the docs build**

Run: `grep -rn 'agent' mkdocs.yml docs/configuration.md | head`
Confirm the new section is reachable from the navigation if the file is listed there.

- [ ] **Step 5: Commit checkpoint**

Stop and hand the tree to the repository owner.

---

### Task 13: End-to-end coverage

**Files:**
- Modify: `e2e/e2e_test.go` (near `assertQa`, around line 194)
- Modify: `e2e/support.go` if a new assertion helper is needed

**Interfaces:**
- Consumes: `Box.TryRun`, `RunResult{Stdout, Stderr, ExitCode}`.
- Produces: `assertQaAgent(t *testing.T, box *Box, c Case)`.

`e2e/support.go:154` defines `errorMarker = "✘"`, the glyph `utils.PrintError` writes, and the suite's `failed` helper greps for it. Agent mode writes `error: ` to stderr and no glyph at all, so the existing helper reports an agent-mode failure as a success. The new assertion must not use it.

- [ ] **Step 1: Write the assertion**

```go
// assertQaAgent drives `orobox qa --agent` and grades it on the contract that mode promises: the
// payload on stdout, one finding per line, diagnostics on stderr, and nothing else anywhere.
//
// It runs after assertQa, which has already established that the tools work in this box. What is
// under test here is the formatting, not the analysis.
func assertQaAgent(t *testing.T, box *Box, c Case) {
	t.Helper()

	res := box.TryRun("qa", "--agent")

	for _, banner := range []string{"Running QA tools", "--- Running ", "✔", "✘", "⚠", "ℹ"} {
		if strings.Contains(res.Stdout, banner) {
			t.Errorf("qa --agent leaked %q into stdout for %s %s:\n%s", banner, c.Oro, c.Type, res.Stdout)
		}
	}

	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if line == "" || strings.HasPrefix(line, "fixed ") || strings.HasPrefix(line, "... ") {
			continue
		}
		// path[:line] severity tool [check] message
		if fields := strings.Fields(line); len(fields) < 3 {
			t.Errorf("qa --agent printed a line that does not parse for %s %s: %q", c.Oro, c.Type, line)
		}
	}

	if res.ExitCode != 0 && strings.TrimSpace(res.Stdout) == "" && strings.TrimSpace(res.Stderr) == "" {
		t.Errorf("qa --agent failed for %s %s with no output at all", c.Oro, c.Type)
	}
}
```

- [ ] **Step 2: Add the clean-tree assertion**

After `assertQaAgent`, run the narrowest tool the box can pass — `box.TryRun("qa", "--agent", "--php-cs-fixer")` immediately after a successful fixing run — and assert `res.Stdout == ""` with `res.ExitCode == 0`. This is the zero-bytes promise from the spec, and it is the assertion most likely to catch a stray `fmt.Println` anywhere in the stack.

- [ ] **Step 3: Call it from the matrix**

Add the call to the step sequence in `TestMatrix`, immediately after the existing `assertQa` step, so it inherits the same installed tools.

- [ ] **Step 4: Run the e2e suite**

Run: `make e2e`
Expected: PASS. This takes a long time and needs Docker — the Makefile allows six hours. If the environment cannot run it, say so explicitly rather than reporting the task as verified.

- [ ] **Step 5: Commit checkpoint**

Stop and hand the tree to the repository owner.

---

### Task 14: Verify the fix-mode classification

**Files:**
- Create: `internal/report/testdata/php-cs-fixer-fixmode.json`
- Create: `internal/report/testdata/rector-fixmode.json`
- Create: `internal/report/testdata/twig-cs-fixer-fixmode.json`
- Modify: `internal/report/testdata/NOTES.md`
- Modify: `cmd/qa_agent.go` (`appliedFixReporters`)
- Test: `cmd/qa_agent_test.go`

**Interfaces:**
- Consumes: `appliedFixReporters` from Task 7.
- Produces: a verified `appliedFixReporters` table and three new fixtures.

This is the open question from the spec. Until it is answered, `twig-cs-fixer` is treated as reporting what remains, which is the safe default: a wrong guess produces visible noise instead of silent omission.

- [ ] **Step 1: Capture the real output**

In a working box with the QA tools installed, against a scratch directory of deliberately broken files (the existing fixtures used `var/qa-probe/` — `NOTES.md` describes the setup), run each fixer in **fix** mode with its report flag and keep the JSON:

```bash
# inside the application container, from the application root
vendor-bin/qa/vendor/bin/php-cs-fixer fix --format=gitlab <path> > php-cs-fixer-fixmode.json
vendor-bin/qa/vendor/bin/rector process <path> --output-format=json > rector-fixmode.json
vendor-bin/qa/vendor/bin/twig-cs-fixer lint --fix --report=gitlab <path> > twig-cs-fixer-fixmode.json
```

Confirm the exact binary paths against `qatools.BinaryPaths` and the exact argument order against what `qatools.Tools` builds — the commands above are the shape, not a guarantee.

- [ ] **Step 2: Decide each tool from its output**

For each file, answer one question: does the document describe files the tool **changed**, or violations it **left**? Re-run the tool a second time over the now-fixed tree — a tool reporting what it changed produces an empty document on the second run; one reporting what remains produces the same document twice.

- [ ] **Step 3: Record what you found**

Extend `NOTES.md` with a fix-mode section: the date, the tool versions, the command for each, and the answer to Step 2's question per tool. `NOTES.md` already carries a "Findings that contradict the original design" section — anything surprising belongs there, in the same voice.

- [ ] **Step 4: Write the test**

Add to `cmd/qa_agent_test.go` a case per tool, reading the new fixture and asserting the classification the capture established. For a tool that reports what it changed:

```go
func TestQaAgentResultCountsTwigCsFixerAsFixed(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "internal", "report", "testdata", "twig-cs-fixer-fixmode.json"))
	if err != nil {
		t.Fatalf("could not read the fixture: %v", err)
	}

	got, err := qaAgentResultFrom(
		[]report.ToolReport{{Tool: "twig-cs-fixer", Data: data}},
		map[string]int{"twig-cs-fixer": 0},
		report.PathPrefix{ContainerRoot: "/var/www/oro"},
		qatools.ModeFix,
	)
	if err != nil {
		t.Fatalf("qaAgentResultFrom() failed: %v", err)
	}

	if !strings.HasPrefix(got.Stdout, "fixed ") {
		t.Errorf("Stdout = %q, want it to start with the fixed summary", got.Stdout)
	}
	if got.Failed {
		t.Error("Failed = true, want false: fixes alone do not fail the run")
	}
}
```

Invert the assertions for a tool that reports what remains.

- [ ] **Step 5: Update the table**

Add `twig-cs-fixer` to `appliedFixReporters` only if Step 2 showed it reports what it changed. If it reports what remains, leave the map alone and replace its doc comment's "deliberately absent, unverified" paragraph with the verified reason.

- [ ] **Step 6: Run the tests**

Run: `go test ./cmd/ ./internal/report/...`
Expected: PASS

- [ ] **Step 7: Update the spec's table**

Replace the `twig-cs-fixer` row's **unverified** marker in the spec with the answer.

- [ ] **Step 8: Commit checkpoint**

Stop and hand the tree to the repository owner.
