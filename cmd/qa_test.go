package cmd

import (
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/spf13/viper"
)

func TestQaCommand(t *testing.T) {
	oldRun := docker.RunComposeCommand
	defer func() { docker.RunComposeCommand = oldRun }()

	var calls [][]string
	docker.RunComposeCommand = func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
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
			6,
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
			2,
			[]string{"phpstan", "rector"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls = nil
			// Reset global flags before each run
			qaPhpstan = false
			qaRector = false
			qaPhpCsFixer = false
			qaTwigCsFixer = false
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
