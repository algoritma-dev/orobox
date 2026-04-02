// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
)

var (
	xdebugDev  bool
	xdebugTest bool
)

var xdebugCmd = &cobra.Command{
	Use:   "xdebug [on|off|status]",
	Short: "Enable, disable or show Xdebug status in development and test environments",
	Args:  cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		docker.EnsureDockerCompose()
		action := args[0]
		if action == "status" {
			showXdebugStatus()
			return
		}

		if action != "on" && action != "off" {
			utils.PrintError("Action must be 'on', 'off' or 'status'")
			os.Exit(1)
		}

		enable := action == "on"

		// Default to both if none specified
		if !xdebugDev && !xdebugTest {
			xdebugDev = true
			xdebugTest = true
		}

		// 1. Hot-patch running containers
		if xdebugDev {
			applyXdebugHotfix(enable, "php-fpm-app", true)
			applyXdebugHotfix(enable, "application", false)
			applyXdebugHotfix(enable, "consumer", false)
			applyXdebugHotfix(enable, "cron", false)
			applyXdebugHotfix(enable, "ws", false)
		}

		if xdebugTest {
			docker.SetIncludeTestFiles(true)
			applyXdebugHotfix(enable, "application", false)
		}

		utils.PrintSuccess(fmt.Sprintf("Xdebug %s completed successfully!", action))
	},
}

func init() {
	rootCmd.AddCommand(xdebugCmd)
	xdebugCmd.Flags().BoolVar(&xdebugDev, "dev", false, "Apply to development environment")
	xdebugCmd.Flags().BoolVar(&xdebugTest, "test", false, "Apply to test environment")
}

func applyXdebugHotfix(enable bool, service string, reloadFpm bool) {
	source := "/usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini"
	target := "/usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini.disabled"

	if enable {
		source = "/usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini.disabled"
		target = "/usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini"
	}

	// Move file if it exists
	execArgs := []string{"exec", "-u", "root"}
	if !isTTY() {
		execArgs = append(execArgs, "-T")
	}
	execArgs = append(execArgs, service, "bash", "-c", fmt.Sprintf("if [ -f %s ]; then mv %s %s; fi", source, source, target))
	err := docker.RunComposeCommand("", execArgs...)
	if err != nil {
		utils.PrintWarning(fmt.Sprintf("Failed to patch %s: %v", service, err))
		return
	}

	if reloadFpm {
		// Signal FPM to reload configuration
		reloadArgs := []string{"exec", "-u", "root"}
		if !isTTY() {
			reloadArgs = append(reloadArgs, "-T")
		}
		reloadArgs = append(reloadArgs, service, "kill", "-USR2", "1")
		_ = docker.RunComposeCommand("", reloadArgs...)
	}
}

func showXdebugStatus() {
	utils.PrintInfo("Checking Xdebug status...")

	showAll := !xdebugDev && !xdebugTest

	// 2. Dev environment status
	if showAll || xdebugDev {
		checkXdebugStatus("php-fpm-app", "Development (php-fpm-app)")
	}

	// 3. Test environment status
	if showAll || xdebugTest {
		docker.SetIncludeTestFiles(true)
		checkXdebugStatus("application", "Test (application with test override)")
	}
}

func checkXdebugStatus(service, label string) {
	execArgs := []string{"exec", "-u", "root"}
	if !isTTY() {
		execArgs = append(execArgs, "-T")
	}
	// Check if the file is present (not disabled)
	execArgs = append(execArgs, service, "bash", "-c", "if [ -f /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini ]; then echo 'on'; else echo 'off'; fi")

	output, err := docker.RunComposeCommandWithOutput(execArgs...)
	if err != nil {
		utils.PrintWarning(fmt.Sprintf("%s: could not check status", label))
		return
	}

	status := strings.TrimSpace(string(output))
	if status == "on" {
		utils.PrintSuccess(fmt.Sprintf("%s: Xdebug is ENABLED", label))
	} else {
		utils.PrintWarning(fmt.Sprintf("%s: Xdebug is DISABLED", label))
	}
}
