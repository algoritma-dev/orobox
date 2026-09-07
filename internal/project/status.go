package project

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// KernelServices are the long-running services that boot an Oro kernel of their own against
// the same database: web, php-fpm-app, ws, consumer and cron. This is the single definition
// shared by the CLI and the tray, so "environment is healthy" cannot drift between the two.
var KernelServices = []string{"web", "php-fpm-app", "ws", "consumer", "cron"}

// ServiceStatus mirrors the fields of `docker compose ps --format json` this package needs.
type ServiceStatus struct {
	Service  string `json:"Service"`
	State    string `json:"State"`
	Health   string `json:"Health"`
	ExitCode int    `json:"ExitCode"`
}

// ParseServiceStatuses decodes the output of `docker compose ps --format json`, keyed by
// service name. Compose has emitted both shapes across its 2.x line: a JSON array, and one
// object per line. Empty or malformed output yields an empty map rather than an error, since
// callers treat "no status" as "not running" either way.
func ParseServiceStatuses(output []byte) map[string]ServiceStatus {
	statuses := make(map[string]ServiceStatus)

	var list []ServiceStatus
	if err := json.Unmarshal(output, &list); err == nil {
		for _, s := range list {
			statuses[s.Service] = s
		}
		return statuses
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		var s ServiceStatus
		if err := json.Unmarshal([]byte(line), &s); err == nil {
			statuses[s.Service] = s
		}
	}
	return statuses
}

// StateFromStatuses maps kernelServices' statuses to a State:
//   - none running                                  -> StateDown
//   - all running, none unhealthy                   -> StateUp
//   - some running, some not                         -> StatePartial
//   - a kernel service exited non-zero, or unhealthy -> StateError
func StateFromStatuses(statuses map[string]ServiceStatus, kernelServices []string) State {
	runningCount := 0
	for _, name := range kernelServices {
		s, ok := statuses[name]
		if !ok {
			continue
		}
		if s.State == "exited" && s.ExitCode != 0 {
			return StateError
		}
		if s.Health == "unhealthy" {
			return StateError
		}
		if s.State == "running" {
			runningCount++
		}
	}

	switch {
	case runningCount == 0:
		return StateDown
	case runningCount == len(kernelServices):
		return StateUp
	default:
		return StatePartial
	}
}

// runPS runs `docker compose <args> ps --format json` under a hard 5s timeout. It is a
// variable so tests can replace it without a real docker binary.
var runPS = func(args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	return cmd.Output()
}

// Status queries docker compose for p's current state. A failed query (docker absent, daemon
// down, timeout) reports StateError, same as a kernel service that is actually unhealthy —
// callers distinguish global Docker unavailability separately, this is per-project only.
//
// A stack whose kernel services are all healthy is queried once more, for whether oro:install
// has ever run: `up` only starts containers, it never installs (see internal/project package
// doc), so "up" alone would send a caller straight to a 500 on a stack init has never touched.
func (p Project) Status() State {
	args := append(p.ComposeArgs(false), "ps", "--format", "json")
	output, err := runPS(args)
	if err != nil {
		return StateError
	}

	state := StateFromStatuses(ParseServiceStatuses(output), KernelServices)
	if state != StateUp {
		return state
	}

	installed, err := p.IsInstalled()
	if err != nil {
		return StateError
	}
	if !installed {
		return StateUpNotInstalled
	}
	return StateUp
}
