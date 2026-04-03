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

		if runTest {
			docker.SetIncludeTestFiles(true)
		}
		docker.EnsureDockerCompose()

		commandName := args[0]
		var executionList []*config.CommandConfig
		visited := make(map[string]bool)
		path := make(map[string]bool)

		if err := resolveDependencies(commandName, commands, &executionList, visited, path); err != nil {
			utils.PrintError(err.Error())

			if len(commands) > 0 {
				fmt.Println("\nAvailable commands:")
				for _, cmd := range commands {
					fmt.Printf("  %-12s %s\n", cmd.Name, cmd.Description)
				}
			}
			return
		}

		for i, cmdToRun := range executionList {
			service := "application"
			if runService != "" {
				service = runService
			} else if cmdToRun.Service != "" {
				service = cmdToRun.Service
			}

			if i == len(executionList)-1 {
				utils.StartLoader(fmt.Sprintf("Running: %s", cmdToRun.Command))
				executeCustomCommand(service, cmdToRun.Command, runTest)
				utils.StopLoader()
			} else {
				utils.StartLoader(fmt.Sprintf("Running dependency: %s", cmdToRun.Name))
				if err := executeCommandWait(service, cmdToRun.Command, runTest); err != nil {
					utils.PrintError(fmt.Sprintf("Dependency '%s' failed: %v", cmdToRun.Name, err))
					os.Exit(1)
				}
				utils.StopLoader()
				utils.PrintInfo(fmt.Sprintf("Running dependency: %s", cmdToRun.Name))
				utils.PrintInfo(fmt.Sprintln("Done"))
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&runService, "service", "s", "", "Service to run the command in")
	runCmd.Flags().BoolVarP(&runTest, "test", "t", false, "Run in test environment (uses application service with test override)")

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

func executeCustomCommand(service, customCommand string, isTest bool) {
	composeCmd := docker.GetComposeCommand()
	binary, err := exec.LookPath(composeCmd[0])
	if err != nil {
		utils.PrintError(fmt.Sprintf("Docker compose not found: %v", err))
		return
	}

	baseArgs := docker.GetBaseComposeArgs()
	args := append(composeCmd, baseArgs...)

	args = append(args, "exec")

	// Check if we have a TTY
	if !isTTY() {
		args = append(args, "-T")
	}

	// Set ORO_ENV=test if explicitly requested via --test flag
	if isTest && service == "application" {
		args = append(args, "-e", "ORO_ENV=test")
	}

	// We use "sh -c" to allow multiple commands and pipes if specified in the config.
	args = append(args, service, "sh", "-c", customCommand)
	env := os.Environ()

	err = syscall.Exec(binary, args, env)
	if err != nil {
		utils.PrintError(fmt.Sprintf("Failed to execute command: %v", err))
	}
}

func executeCommandWait(service, customCommand string, isTest bool) error {
	composeCmd := docker.GetComposeCommand()
	binary, err := exec.LookPath(composeCmd[0])
	if err != nil {
		return fmt.Errorf("docker compose not found: %v", err)
	}

	baseArgs := docker.GetBaseComposeArgs()
	args := append(composeCmd, baseArgs...)
	args = append(args, "exec")

	// Check if we have a TTY
	if !isTTY() {
		args = append(args, "-T")
	}

	// Set ORO_ENV=test if explicitly requested via --test flag
	if isTest && service == "application" {
		args = append(args, "-e", "ORO_ENV=test")
	}

	// We use "sh -c" to allow multiple commands and pipes if specified in the config.
	args = append(args, service, "sh", "-c", customCommand)

	cmd := exec.Command(binary, args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

func resolveDependencies(name string, commands []config.CommandConfig, executionList *[]*config.CommandConfig, visited map[string]bool, path map[string]bool) error {
	if path[name] {
		return fmt.Errorf("circular dependency detected for command '%s'", name)
	}

	if visited[name] {
		return nil
	}

	var foundCommand *config.CommandConfig
	for i := range commands {
		if commands[i].Name == name {
			foundCommand = &commands[i]
			break
		}
	}

	if foundCommand == nil {
		return fmt.Errorf("command '%s' not found in .orobox.yaml", name)
	}

	path[name] = true
	for _, depName := range foundCommand.Depends {
		if err := resolveDependencies(depName, commands, executionList, visited, path); err != nil {
			return err
		}
	}
	delete(path, name)

	visited[name] = true
	*executionList = append(*executionList, foundCommand)
	return nil
}
