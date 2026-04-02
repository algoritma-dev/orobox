// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	runService string
	runTest    bool
)

var runCmd = &cobra.Command{
	Use:   "run [command]",
	Short: "Run a custom command from .orobox.yaml",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		var commands []config.CommandConfig
		_ = viper.UnmarshalKey("commands", &commands)

		if len(args) == 0 {
			if len(commands) > 0 {
				_ = cmd.Help()
			} else {
				utils.PrintWarning("No custom commands defined in .orobox.yaml")
				_ = cmd.Help()
			}
			return
		}

		docker.EnsureDockerCompose()

		commandName := args[0]
		var foundCommand *config.CommandConfig
		for _, cmd := range commands {
			if cmd.Name == commandName {
				foundCommand = &cmd
				break
			}
		}

		if foundCommand == nil {
			utils.PrintError(fmt.Sprintf("Command '%s' not found in .orobox.yaml", commandName))

			if len(commands) > 0 {
				fmt.Println("\nAvailable commands:")
				for _, cmd := range commands {
					fmt.Printf("  %-12s %s\n", cmd.Name, cmd.Description)
				}
			}
			return
		}

		service := "application"
		if runService != "" {
			service = runService
		} else if runTest {
			service = "application_test"
		} else if foundCommand.Service != "" {
			service = foundCommand.Service
		}

		executeCustomCommand(service, foundCommand.Command)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&runService, "service", "s", "", "Service to run the command in")
	runCmd.Flags().BoolVarP(&runTest, "test", "t", false, "Run in test environment (uses application_test service)")

	// Dynamic help
	runCmd.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		if viper.ConfigFileUsed() == "" {
			initConfig()
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n\nUsage:\n  %s\n", cmd.Short, cmd.UseLine())

		var commands []config.CommandConfig
		_ = viper.UnmarshalKey("commands", &commands)

		if len(commands) > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "\nAvailable Commands:")
			for _, c := range commands {
				fmt.Fprintf(cmd.OutOrStdout(), "  %-12s %s\n", c.Name, c.Description)
			}
		}

		fmt.Fprintln(cmd.OutOrStdout(), "\nFlags:")
		fmt.Fprintf(cmd.OutOrStdout(), "%s", cmd.Flags().FlagUsages())
	})
}

func executeCustomCommand(service, customCommand string) {
	composeCmd := docker.GetComposeCommand()
	binary, err := exec.LookPath(composeCmd[0])
	if err != nil {
		utils.PrintError(fmt.Sprintf("Docker compose not found: %v", err))
		return
	}

	baseArgs := docker.GetBaseComposeArgs()
	args := append(composeCmd, baseArgs...)
	// We use "exec" to run the command in the specified container.
	// We use "sh -c" to allow multiple commands and pipes if specified in the config.
	args = append(args, "exec", service, "sh", "-c", customCommand)
	env := os.Environ()

	err = syscall.Exec(binary, args, env)
	if err != nil {
		utils.PrintError(fmt.Sprintf("Failed to execute command: %v", err))
	}
}
