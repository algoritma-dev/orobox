package docker

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// stubServiceHealthPolling makes the health wait deterministic: no real clock, a small budget.
func stubServiceHealthPolling(t *testing.T, attempts int) {
	t.Helper()

	oldAttempts, oldInterval, oldSleep := serviceHealthAttempts, serviceHealthInterval, sleepFunc
	oldRun, oldRunSilently := RunComposeCommandWithOutput, RunComposeCommandSilently
	t.Cleanup(func() {
		serviceHealthAttempts, serviceHealthInterval, sleepFunc = oldAttempts, oldInterval, oldSleep
		RunComposeCommandWithOutput, RunComposeCommandSilently = oldRun, oldRunSilently
		ResetEnsuredServices()
	})

	serviceHealthAttempts = attempts
	serviceHealthInterval = time.Millisecond
	sleepFunc = func(time.Duration) {}
	RunComposeCommandSilently = func(string, ...string) error { return nil }
	ResetEnsuredServices()
}

// psReturning builds a `docker compose ps` mock that answers with the given statuses, advancing
// through them one call at a time and repeating the last.
func psReturning(t *testing.T, rounds ...[]ServiceStatus) *int {
	t.Helper()

	call := 0
	RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		if len(args) == 0 || args[0] != "ps" {
			return []byte(""), nil
		}
		round := rounds[min(call, len(rounds)-1)]
		call++
		body, err := json.Marshal(round)
		if err != nil {
			t.Fatal(err)
		}
		return body, nil
	}
	return &call
}

// A container reporting Health "starting" is running but has not passed a single probe yet:
// Postgres is not accepting, RabbitMQ is not listening. Treating it as ensured is what let
// every caller exec straight into a service that was not up.
func TestEnsureServicesRunningWaitsOutStarting(t *testing.T) {
	stubServiceHealthPolling(t, 10)
	calls := psReturning(t,
		[]ServiceStatus{{Service: "db", State: "running", Health: "starting"}},
		[]ServiceStatus{{Service: "db", State: "running", Health: "starting"}},
		[]ServiceStatus{{Service: "db", State: "running", Health: "healthy"}},
	)

	if err := EnsureServicesRunning([]string{"db"}); err != nil {
		t.Fatalf("EnsureServicesRunning returned %v once db turned healthy", err)
	}
	if *calls < 3 {
		t.Errorf("only %d ps calls: the wait returned before db reported healthy", *calls)
	}
}

// A service with no healthcheck cannot ever report health, so it must not be waited on.
// `application` is one, and it is in almost every call.
func TestEnsureServicesRunningAcceptsAServiceWithoutAHealthcheck(t *testing.T) {
	stubServiceHealthPolling(t, 10)
	calls := psReturning(t, []ServiceStatus{{Service: "application", State: "running", Health: ""}})

	if err := EnsureServicesRunning([]string{"application"}); err != nil {
		t.Fatalf("EnsureServicesRunning returned %v for a service that reports no health", err)
	}
	if *calls != 1 {
		t.Errorf("expected a single ps call, got %d: a service without a healthcheck was waited on", *calls)
	}
}

// An already-healthy service costs one ps call and no sleep: this runs before most commands.
func TestEnsureServicesRunningIsFreeWhenAlreadyHealthy(t *testing.T) {
	stubServiceHealthPolling(t, 10)
	slept := 0
	sleepFunc = func(time.Duration) { slept++ }
	calls := psReturning(t, []ServiceStatus{{Service: "db", State: "running", Health: "healthy"}})

	if err := EnsureServicesRunning([]string{"db"}); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || slept != 0 {
		t.Errorf("healthy service cost %d ps calls and %d sleeps, want 1 and 0", *calls, slept)
	}
}

// A service that never passes a probe has to fail the caller. Reporting success and letting the
// caller exec into it is how the failure used to surface as a confusing error from psql instead.
func TestEnsureServicesRunningGivesUpOnAServiceThatNeverGetsHealthy(t *testing.T) {
	stubServiceHealthPolling(t, 4)
	psReturning(t, []ServiceStatus{{Service: "db", State: "running", Health: "starting"}})

	err := EnsureServicesRunning([]string{"db"})
	if err == nil {
		t.Fatal("EnsureServicesRunning returned nil for a service stuck in starting")
	}
	if !strings.Contains(err.Error(), "db") {
		t.Errorf("error should name the service that never got healthy, got: %v", err)
	}
}

// An unhealthy service is a failure, not something to keep waiting on: the probe ran and said no.
func TestEnsureServicesRunningFailsFastOnUnhealthy(t *testing.T) {
	stubServiceHealthPolling(t, 20)
	calls := psReturning(t,
		[]ServiceStatus{{Service: "db", State: "running", Health: "starting"}},
		[]ServiceStatus{{Service: "db", State: "running", Health: "unhealthy"}},
	)

	if err := EnsureServicesRunning([]string{"db"}); err == nil {
		t.Fatal("EnsureServicesRunning returned nil for an unhealthy service")
	}
	if *calls > 3 {
		t.Errorf("kept polling an unhealthy service: %d ps calls", *calls)
	}
}

// A service marked ensured is not re-checked, so the wait is paid once per process and not
// before every command that names the same service.
func TestEnsureServicesRunningRemembersHealthyServices(t *testing.T) {
	stubServiceHealthPolling(t, 10)
	calls := psReturning(t, []ServiceStatus{{Service: "db", State: "running", Health: "healthy"}})

	if err := EnsureServicesRunning([]string{"db"}); err != nil {
		t.Fatal(err)
	}
	before := *calls
	if err := EnsureServicesRunning([]string{"db"}); err != nil {
		t.Fatal(err)
	}
	if *calls != before {
		t.Errorf("db was re-checked: %d ps calls, want %d", *calls, before)
	}
}
