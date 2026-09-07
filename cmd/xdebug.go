// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
)

var (
	xdebugConsumer bool
	xdebugCron     bool
	xdebugJSON     bool
)

var xdebugCmd = &cobra.Command{
	Use:   "xdebug [on|off|status]",
	Short: "Enable, disable or show Xdebug status",
	Args:  cobra.ExactArgs(1),
	// A patch or reload that fails is a runtime problem, not a usage problem: printing the
	// flag list after it buries the actual error (and the compose output above it) under
	// help text that has nothing to do with the failure.
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, args []string) error {
		docker.EnsureDockerCompose()
		action := args[0]
		if action == "status" {
			if xdebugJSON {
				return showXdebugStatusJSON()
			}
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
			err = applyXdebugHotfix(enable, "consumer", false, true)
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
	xdebugCmd.Flags().BoolVar(&xdebugJSON, "json", false, "With 'status', print machine-readable JSON instead of text")
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
		// `docker compose exec` needs the service up. The most common reason this fails is a
		// stack that is not running (or a container that died), so point there: the bare
		// "exit status 1" on its own leaves nothing to act on.
		return fmt.Errorf("failed to patch %s: %w (if %s is not running, start the stack with 'orobox up')", service, err, service)
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

// xdebugStatusServices are the containers whose Xdebug state is reported by `xdebug status`,
// in both its text and --json forms.
var xdebugStatusServices = []struct{ service, label string }{
	{"application", "Application"},
	{"php-fpm-app", "PHP-FPM"},
	{"cron", "Cron"},
	{"consumer", "Consumer"},
}

func showXdebugStatus() {
	utils.StartLoader("Checking Xdebug status...")
	defer utils.StopLoader()

	for _, s := range xdebugStatusServices {
		enabled, err := xdebugEnabled(s.service)
		if err != nil {
			utils.PrintWarning(fmt.Sprintf("%s: could not check status", s.label))
			continue
		}
		if enabled {
			utils.PrintSuccess(fmt.Sprintf("%s: Xdebug is ENABLED", s.label))
		} else {
			utils.PrintWarning(fmt.Sprintf("%s: Xdebug is DISABLED", s.label))
		}
	}
}

// showXdebugStatusJSON prints {"application":bool,"php-fpm-app":bool,"consumer":bool,"cron":bool}
// so a caller like orobox-tray can drive a checkbox off a real state instead of guessing.
// Unlike the text form, a service that cannot be checked fails the whole call: a caller
// consuming this as data has no "could not check status" middle ground to render.
func showXdebugStatusJSON() error {
	result := make(map[string]bool, len(xdebugStatusServices))
	for _, s := range xdebugStatusServices {
		enabled, err := xdebugEnabled(s.service)
		if err != nil {
			return fmt.Errorf("checking %s: %w", s.service, err)
		}
		result[s.service] = enabled
	}

	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// xdebugEnabled reports whether Xdebug's ini file is present (not disabled) in service.
func xdebugEnabled(service string) (bool, error) {
	execArgs := []string{"exec", "-u", "root"}
	if !isTTY() {
		execArgs = append(execArgs, "-T")
	}
	execArgs = append(execArgs, service, "bash", "-c", "if [ -f /usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini ]; then echo 'on'; else echo 'off'; fi")

	output, err := docker.RunComposeCommandWithOutput(execArgs...)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(output)) == "on", nil
}
