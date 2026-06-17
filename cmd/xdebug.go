// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"strings"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
)

var (
	xdebugConsumer bool
	xdebugCron     bool
)

var xdebugCmd = &cobra.Command{
	Use:   "xdebug [on|off|status]",
	Short: "Enable, disable or show Xdebug status",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		docker.EnsureDockerCompose()
		action := args[0]
		if action == "status" {
			showXdebugStatus()
			return nil
		}

		if action != "on" && action != "off" {
			utils.PrintError("Action must be 'on', 'off' or 'status'")
			return fmt.Errorf("invalid xdebug action: %s", action)
		}

		enable := action == "on"

		var err error
		if xdebugCron {
			err = applyXdebugHotfix(enable, "cron", false, false)
		} else if xdebugConsumer {
			err = applyXdebugHotfix(enable, "consumer", false, false)
		} else {
			if err = applyXdebugHotfix(enable, "application", false, false); err == nil {
				err = applyXdebugHotfix(enable, "php-fpm-app", true, false)
			}
		}
		if err != nil {
			utils.PrintError(fmt.Sprintf("Xdebug %s failed: %v", action, err))
			return err
		}

		utils.PrintSuccess(fmt.Sprintf("Xdebug %s completed successfully!", action))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(xdebugCmd)
	xdebugCmd.Flags().BoolVar(&xdebugConsumer, "consumer", false, "Apply to consumer service")
	xdebugCmd.Flags().BoolVar(&xdebugCron, "cron", false, "Apply to cron service")
}

func applyXdebugHotfix(enable bool, service string, reloadPhpFpm bool, restartService bool) error {
	source := "/usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini"
	target := "/usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini.disabled"

	if enable {
		source = "/usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini.disabled"
		target = "/usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini"
	}

	// Move file if it exists
	execArgs := []string{"exec", "-u", "root"}
	execArgs = append(execArgs, "-T")
	execArgs = append(execArgs, service, "bash", "-c", fmt.Sprintf("if [ -f %s ]; then mv %s %s; fi", source, source, target))
	err := docker.RunComposeCommandSilently("Applying Xdebug patch...", execArgs...)
	if err != nil {
		return fmt.Errorf("failed to patch %s: %w", service, err)
	}

	if reloadPhpFpm {
		// Signal FPM to reload configuration
		reloadArgs := []string{"exec", "-u", "root"}
		if !isTTY() {
			reloadArgs = append(reloadArgs, "-T")
		}
		reloadArgs = append(reloadArgs, service, "kill", "-USR2", "1")
		if err := docker.RunComposeCommandSilently("Reloading PHP-FPM...", reloadArgs...); err != nil {
			return fmt.Errorf("failed to reload %s: %w", service, err)
		}
	}

	if restartService {
		if err := docker.RunComposeCommandSilently(fmt.Sprintf("Restarting %s...", service), "restart", service); err != nil {
			return fmt.Errorf("failed to restart %s: %w", service, err)
		}
	}

	return nil
}

func showXdebugStatus() {
	utils.StartLoader("Checking Xdebug status...")
	defer utils.StopLoader()

	checkXdebugStatus("application", "Application")
	checkXdebugStatus("php-fpm-app", "PHP-FPM")
	checkXdebugStatus("cron", "Cron")
	checkXdebugStatus("consumer", "Consumer")
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
