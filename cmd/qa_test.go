package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/spf13/viper"
)

func TestQaCommand(t *testing.T) {
	oldRun := docker.RunComposeCommand
	oldRunSilently := docker.RunComposeCommandSilently
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	defer func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandSilently = oldRunSilently
		docker.RunComposeCommandWithOutput = oldRunWithOutput
	}()

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

	viper.Set("type", "project")
	defer viper.Set("type", nil)

	tests := []struct {
		name          string
		args          []string
		expectedCount int
		expectedTools []string
	}{
		{
			"All tools by default",
			[]string{"qa"},
			1, // Now grouped in a single call
			[]string{"phpstan", "rector", "php-cs-fixer", "twig-cs-fixer", "eslint", "stylelint"},
		},
		{
			"Only PHPStan",
			[]string{"qa", "--phpstan"},
			1,
			[]string{"phpstan"},
		},
		{
			"PHPStan and Rector",
			[]string{"qa", "--phpstan", "--rector"},
			1, // Now grouped in a single call
			[]string{"phpstan", "rector"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls = nil
			docker.ResetEnsuredServices()
			// Reset global flags before each run
			qaPhpstan = false
			qaRector = false
			qaPhpCSFixer = false
			qaTwigCSFixer = false
			qaEslint = false
			qaStylelint = false

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()
			if err != nil {
				t.Fatalf("rootCmd.Execute() failed: %v", err)
			}

			if len(calls) != tt.expectedCount {
				t.Errorf("Expected %d calls, got %d. Calls: %v", tt.expectedCount, len(calls), calls)
			}

			for _, tool := range tt.expectedTools {
				found := false
				for _, call := range calls {
					if contains(call, tool) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected tool %s not found in calls", tool)
				}
			}
		})
	}
}

func TestQaBundleCommand(t *testing.T) {
	oldRun := docker.RunComposeCommand
	oldRunSilently := docker.RunComposeCommandSilently
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	defer func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandSilently = oldRunSilently
		docker.RunComposeCommandWithOutput = oldRunWithOutput
	}()

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

	viper.Set("type", "bundle")
	viper.Set("namespace", "Test\\MyBundle")
	defer viper.Set("type", nil)
	defer viper.Set("namespace", nil)

	docker.ResetEnsuredServices()
	qaPhpstan = false
	qaRector = false
	qaPhpCSFixer = false
	qaTwigCSFixer = false
	qaEslint = true
	qaStylelint = true

	rootCmd.SetArgs([]string{"qa"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	// Grouped in a single call
	expectedCount := 1
	if len(calls) != expectedCount {
		t.Errorf("Expected %d calls, got %d. Calls: %v", expectedCount, len(calls), calls)
	}

	// Verify ESLint call
	foundEslint := false
	for _, call := range calls {
		if contains(call, "eslint") {
			foundEslint = true
			if !contains(call, "/var/www/oro/.eslintrc.yml") {
				t.Errorf("ESLint call missing .eslintrc.yml: %v", call)
			}
			if !contains(call, "/var/www/oro/.eslintignore") {
				t.Errorf("ESLint call missing .eslintignore: %v", call)
			}
		}
	}
	if !foundEslint {
		t.Error("ESLint call not found")
	}

	// Verify Stylelint calls
	foundStylelint := false
	foundStylelintCSS := false
	for _, call := range calls {
		// Both calls contain "stylelint"
		if contains(call, "stylelint") {
			if contains(call, "/var/www/oro/.stylelintrc.yml") {
				foundStylelint = true
				if !contains(call, "/var/www/oro/.stylelintignore") {
					t.Errorf("Stylelint call missing .stylelintignore: %v", call)
				}
			}
			if contains(call, "/var/www/oro/.stylelintrc-css.yml") {
				foundStylelintCSS = true
				if !contains(call, "/var/www/oro/.stylelintignore-css") {
					t.Errorf("Stylelint-css call missing .stylelintignore-css: %v", call)
				}
			}
		}
	}
	if !foundStylelint {
		t.Error("Stylelint (non-CSS) call not found")
	}
	if !foundStylelintCSS {
		t.Error("Stylelint (CSS) call not found")
	}
}

func TestQaComposeReportModeUsesTheAggregatingScript(t *testing.T) {
	oldRun := docker.RunComposeCommand
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	defer func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandWithOutput = oldRunWithOutput
	}()

	var calls [][]string
	docker.RunComposeCommand = func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			return []byte(`{"Service": "application", "State": "running"}`), nil
		}
		return []byte("[]"), nil
	}

	viper.Set("type", "project")
	defer viper.Set("type", nil)

	// The per-tool flags are package-level state an earlier test may have left set, which would
	// narrow this run to whatever it selected.
	qaPhpstan, qaRector, qaPhpCSFixer, qaTwigCSFixer, qaEslint, qaStylelint = false, false, false, false, false, false

	format, err := resolveReport("gitlab")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	previous, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()

	// The report directory has to look like a finished run, or the merge exits the process.
	rawDir := filepath.Join(dir, "var", "orobox", "reports", "raw", "qa")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, qatools.StatusFile), []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "phpstan.json"), []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}

	runQaOnCompose(format)

	var script string
	for _, args := range calls {
		if len(args) > 0 {
			script = args[len(args)-1]
		}
	}
	if !strings.Contains(script, "status=0") {
		t.Errorf("report mode must use the aggregating script:\n%s", script)
	}
	if !strings.Contains(script, "--error-format=gitlab") {
		t.Errorf("report mode must ask the tools for GitLab output:\n%s", script)
	}
	if _, err := os.Stat(filepath.Join(dir, "var", "orobox", "reports", "code-quality.json")); err != nil {
		t.Errorf("the merged report was not written: %v", err)
	}
}

func TestQaPathPrefixForAMonorepo(t *testing.T) {
	viper.Set("type", "project")
	viper.Set("deploy.source_dir", "apps/shop")
	defer func() {
		viper.Set("type", nil)
		viper.Set("deploy.source_dir", nil)
	}()

	prefix := qaPathPrefix(engineDagger)
	if prefix.RepoSubdir != "apps/shop" {
		t.Errorf("RepoSubdir = %q, want the monorepo application directory", prefix.RepoSubdir)
	}

	if got := qaPathPrefix(engineCompose); got.RepoSubdir != "" {
		t.Errorf("the compose engine writes from inside the project, so RepoSubdir must be empty: %q", got.RepoSubdir)
	}
}
