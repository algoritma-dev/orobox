package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/qatools"

	"github.com/spf13/viper"
)

// resetQaFlags puts the command's package-level flags back to their zero value. runQaCommand turns
// --phpstan on by itself in a baseline run, so leaving them set would decide the next test's tool
// list.
func resetQaFlags() {
	qaPhpstan = false
	qaRector = false
	qaPhpCSFixer = false
	qaTwigCSFixer = false
	qaEslint = false
	qaStylelint = false
	qaEngine = ""
	qaReport = ""
	qaGenerateBaseline = false
}

func TestResolveBaselineRefusesWhatItCannotDeliver(t *testing.T) {
	viper.Set("type", "project")
	t.Cleanup(func() { viper.Set("type", nil) })

	tests := []struct {
		name    string
		engine  string
		format  qatools.Report
		setFlag func()
		wantErr string
	}{
		{"dagger engine", engineDagger, qatools.ReportNone, nil, "--engine=compose"},
		{"a report", engineCompose, qatools.ReportGitLab, nil, "--report cannot be combined"},
		{"another tool", engineCompose, qatools.ReportNone, func() { qaRector = true }, "drop --rector"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetQaFlags()
			t.Cleanup(resetQaFlags)
			if tt.setFlag != nil {
				tt.setFlag()
			}

			_, err := resolveBaseline(tt.engine, tt.format)
			if err == nil {
				t.Fatalf("resolveBaseline(%q, %v) accepted the combination", tt.engine, tt.format)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not mention %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestResolveBaselineWritesNextToTheProjectConfig(t *testing.T) {
	viper.Set("type", "project")
	t.Cleanup(func() { viper.Set("type", nil) })
	resetQaFlags()
	t.Cleanup(resetQaFlags)

	got, err := resolveBaseline(engineCompose, qatools.ReportNone)
	if err != nil {
		t.Fatalf("resolveBaseline: %v", err)
	}
	if want := config.GetSourceRootContainerPath() + "/" + qatools.BaselineFile; got != want {
		t.Errorf("resolveBaseline = %q, want %q", got, want)
	}
}

func TestFinishBaselineWiresTheIncludeIntoTheProjectConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, qatools.BaselineFile), []byte("parameters:\n    ignoreErrors: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "phpstan.neon")
	if err := os.WriteFile(configPath, []byte("parameters:\n    level: 6\n"), 0644); err != nil {
		t.Fatal(err)
	}

	finishBaseline(dir)

	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), qatools.BaselineFile) {
		t.Errorf("phpstan.neon does not include the baseline: %q", updated)
	}
	if !strings.Contains(string(updated), "level: 6") {
		t.Errorf("phpstan.neon lost its own configuration: %q", updated)
	}
}

func TestFinishBaselineWritesAProjectConfigWhenThereIsNone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, qatools.BaselineFile), []byte("parameters:\n    ignoreErrors: []\n"), 0644); err != nil {
		t.Fatal(err)
	}

	finishBaseline(dir)

	written, err := os.ReadFile(filepath.Join(dir, "phpstan.neon"))
	if err != nil {
		t.Fatalf("no phpstan.neon was written: %v", err)
	}
	if !strings.Contains(string(written), qatools.BaselineFile) {
		t.Errorf("the written config does not include the baseline: %q", written)
	}
}

func TestQaGenerateBaselineRunsPhpstanAlone(t *testing.T) {
	oldRun := docker.RunComposeCommand
	oldRunSilently := docker.RunComposeCommandSilently
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	t.Cleanup(func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandSilently = oldRunSilently
		docker.RunComposeCommandWithOutput = oldRunWithOutput
	})

	var calls [][]string
	mockRun := func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	docker.RunComposeCommand = mockRun
	docker.RunComposeCommandSilently = mockRun
	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			return []byte(`{"Service": "application", "State": "running"}`), nil
		}
		return []byte("[]"), nil
	}

	// The mocked run does not write the file PHPStan would, so the temp project carries it: a
	// baseline that is not on disk afterwards is a failure the command exits on.
	withTempProject(t)
	if err := os.WriteFile(qatools.BaselineFile, []byte("parameters:\n    ignoreErrors: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	viper.Set("type", "project")
	t.Cleanup(func() { viper.Set("type", nil) })

	docker.ResetEnsuredServices()
	resetQaFlags()
	t.Cleanup(resetQaFlags)

	rootCmd.SetArgs([]string{"qa", "--generate-baseline"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %v", len(calls), calls)
	}
	script := strings.Join(calls[0], " ")
	if !strings.Contains(script, "--generate-baseline="+config.GetSourceRootContainerPath()+"/"+qatools.BaselineFile) {
		t.Errorf("the run does not generate the baseline: %s", script)
	}
	for _, other := range []string{"rector", "php-cs-fixer", "eslint", "stylelint"} {
		if strings.Contains(script, other) {
			t.Errorf("%s ran in a baseline run: %s", other, script)
		}
	}

	if _, err := os.Stat("phpstan.neon"); err != nil {
		t.Errorf("the baseline was not wired into a phpstan.neon: %v", err)
	}
}
