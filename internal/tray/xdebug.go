package tray

import (
	"encoding/json"
	"os/exec"
)

// runXdebugStatus runs `<oroboxBin> xdebug status --json` in hostPath. A variable so tests can
// replace it without a real orobox binary or Docker stack.
var runXdebugStatus = func(oroboxBin, hostPath string) ([]byte, error) {
	cmd := exec.Command(oroboxBin, "xdebug", "status", "--json")
	cmd.Dir = hostPath
	return cmd.Output()
}

// XdebugStatus reports which services currently have Xdebug enabled, keyed the same way as
// `orobox xdebug status --json`: "application", "php-fpm-app", "consumer", "cron".
func XdebugStatus(oroboxBin, hostPath string) (map[string]bool, error) {
	output, err := runXdebugStatus(oroboxBin, hostPath)
	if err != nil {
		return nil, err
	}
	var result map[string]bool
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// XdebugChecked is what the menu's single Xdebug checkbox reflects: whether the main
// application service has it enabled, since that is what a developer toggles day to day.
func XdebugChecked(status map[string]bool) bool {
	return status["application"]
}
