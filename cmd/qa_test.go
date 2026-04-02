package cmd

import (
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
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
