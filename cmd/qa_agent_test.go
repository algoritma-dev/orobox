package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/algoritma-dev/orobox/internal/report"
	"github.com/spf13/viper"
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

// agentProject points the configuration at a throwaway project root, so rawReportDir("qa") lands
// in a temporary directory rather than in the repository being worked on.
func agentProject(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, ".orobox.yaml")
	if err := os.WriteFile(configPath, []byte("type: project\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(viper.Reset)

	return root
}

// TestQaAgentRunsTheReportMachineryWithoutLeavingFixMode is the guard on the one non-additive
// change in this feature: agent mode forces a report so it can read the tools, and the mode the
// tools run in must stay the fix mode a local `orobox qa` has always had.
func TestQaAgentRunsTheReportMachineryWithoutLeavingFixMode(t *testing.T) {
	oldRun := docker.RunComposeCommand
	oldRunSilently := docker.RunComposeCommandSilently
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	t.Cleanup(func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandSilently = oldRunSilently
		docker.RunComposeCommandWithOutput = oldRunWithOutput
		output.SetAgent(false)
		docker.ResetEnsuredServices()
	})

	root := agentProject(t)
	rawDir := filepath.Join(root, config.RawReportsRelDir, "qa")

	var script string
	var streamed bool
	docker.RunComposeCommand = func(_ string, _ ...string) error {
		streamed = true
		return nil
	}
	docker.RunComposeCommandSilently = docker.RunComposeCommand
	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			return psRunningRequested(args), nil
		}
		if len(args) > 0 && args[0] == "exec" {
			script = args[len(args)-1]
			// Stand in for the container: every enabled tool writes a clean report.
			if err := os.MkdirAll(rawDir, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, tool := range []string{"phpstan", "php-cs-fixer"} {
				if err := os.WriteFile(filepath.Join(rawDir, tool+".json"), []byte("[]"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			return []byte("--- Running phpstan ---\n[OK] No errors\n"), nil
		}
		return []byte("[]"), nil
	}

	var payload, errs bytes.Buffer
	restore := output.SetWriters(&payload, &errs)
	t.Cleanup(restore)
	output.SetAgent(true)

	runQaOnCompose(qatools.ReportNone, "", nil)

	if script == "" {
		t.Fatal("the agent-mode run never reached the container through the capturing runner")
	}
	if streamed {
		t.Error("the agent-mode run used the streaming runner, so the tools' own output reached stdout")
	}
	if !strings.Contains(script, "--error-format=gitlab") {
		t.Errorf("agent mode did not force the report machinery on:\n%s", script)
	}
	if strings.Contains(script, "--dry-run") {
		t.Errorf("agent mode turned the tools check-only; a local qa run still fixes:\n%s", script)
	}
	if payload.Len() != 0 {
		t.Errorf("a clean agent-mode run wrote %q, want nothing", payload.String())
	}
	if errs.Len() != 0 {
		t.Errorf("a clean agent-mode run wrote %q to stderr, want nothing", errs.String())
	}
	if _, err := os.Stat(rawDir); !os.IsNotExist(err) {
		t.Errorf("the raw report directory survived a run that did not ask for a report: %v", err)
	}
}
