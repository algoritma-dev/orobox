package docker

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// stubDbReadyPolling makes the poll loop deterministic: no real clock, a small attempt budget.
func stubDbReadyPolling(t *testing.T, attempts int) {
	t.Helper()

	oldAttempts, oldInterval, oldSleep, oldRun := dbReadyAttempts, dbReadyInterval, sleepFunc, RunComposeCommandWithOutput
	t.Cleanup(func() {
		dbReadyAttempts, dbReadyInterval, sleepFunc, RunComposeCommandWithOutput = oldAttempts, oldInterval, oldSleep, oldRun
	})

	dbReadyAttempts = attempts
	dbReadyInterval = time.Millisecond
	sleepFunc = func(time.Duration) {}
}

// Postgres reports "accepting connections" only once it is actually listening, so the wait has
// to keep polling: `docker compose up -d` returns as soon as the container is running, which is
// several seconds earlier.
func TestWaitForDatabaseReadyRetriesUntilAccepting(t *testing.T) {
	stubDbReadyPolling(t, 10)

	var calls [][]string
	RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		calls = append(calls, args)
		if len(calls) < 3 {
			return []byte("no response"), errors.New("exit status 2")
		}
		return []byte("accepting connections"), nil
	}

	if err := WaitForDatabaseReady(false); err != nil {
		t.Fatalf("WaitForDatabaseReady returned %v after the server came up", err)
	}
	if len(calls) != 3 {
		t.Errorf("expected 3 polls, got %d: %v", len(calls), calls)
	}

	got := strings.Join(calls[0], " ")
	for _, want := range []string{"exec", "-T", "db", "pg_isready"} {
		if !strings.Contains(got, want) {
			t.Errorf("poll is missing %q, got: %q", want, got)
		}
	}
}

// A server that never comes up must surface as an error rather than as a caller that goes on
// to psql against nothing and reports a confusing failure of its own.
func TestWaitForDatabaseReadyGivesUp(t *testing.T) {
	stubDbReadyPolling(t, 4)

	polls := 0
	RunComposeCommandWithOutput = func(...string) ([]byte, error) {
		polls++
		return nil, errors.New("exit status 2")
	}

	err := WaitForDatabaseReady(false)
	if err == nil {
		t.Fatal("WaitForDatabaseReady returned nil for a server that never accepted connections")
	}
	if polls != 4 {
		t.Errorf("expected the full attempt budget of 4 polls, got %d", polls)
	}
	if !strings.Contains(err.Error(), "db") {
		t.Errorf("error should name the service that stayed down, got: %v", err)
	}
}

// The test environment is a different container and a different database, and test-init is the
// command that hit the race, so the wait has to target it.
func TestWaitForDatabaseReadyTargetsTestService(t *testing.T) {
	stubDbReadyPolling(t, 2)

	var args []string
	RunComposeCommandWithOutput = func(a ...string) ([]byte, error) {
		args = a
		return nil, nil
	}

	if err := WaitForDatabaseReady(true); err != nil {
		t.Fatalf("WaitForDatabaseReady returned %v", err)
	}

	got := strings.Join(args, " ")
	if !strings.Contains(got, "db-test") {
		t.Errorf("expected the poll to target db-test, got: %q", got)
	}
	if !strings.Contains(got, "oro_db_test") {
		t.Errorf("expected the poll to name the test database, got: %q", got)
	}
}

// An already-accepting server must not cost a single sleep: the wait runs before every command
// that talks to the database, so the common case has to be free.
func TestWaitForDatabaseReadyDoesNotSleepWhenReady(t *testing.T) {
	stubDbReadyPolling(t, 10)

	slept := 0
	sleepFunc = func(time.Duration) { slept++ }
	RunComposeCommandWithOutput = func(...string) ([]byte, error) { return nil, nil }

	if err := WaitForDatabaseReady(false); err != nil {
		t.Fatalf("WaitForDatabaseReady returned %v", err)
	}
	if slept != 0 {
		t.Errorf("expected no sleep for a ready server, slept %d times", slept)
	}
}
