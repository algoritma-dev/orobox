package cmd

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
)

func TestDbCommand(t *testing.T) {
	oldRunSilently := docker.RunComposeCommandSilently
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	oldDbExec := dbExec
	defer func() {
		docker.RunComposeCommandSilently = oldRunSilently
		docker.RunComposeCommandWithOutput = oldRunWithOutput
		dbExec = oldDbExec
	}()

	var calls [][]string
	docker.RunComposeCommandSilently = func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		if args[0] == "ps" {
			return psRunningRequested(args), nil
		}
		return []byte(""), nil
	}
	dbExec = func(_ io.Reader, _ io.Writer, args ...string) error {
		calls = append(calls, args)
		return nil
	}

	// Create a dummy backup file
	tmpFile, err := os.CreateTemp("", "backup*.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	rootCmd.SetArgs([]string{"db", "restore", tmpFile.Name()})
	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	// Expected calls:
	// 1. ps (to check services)
	// 2. psql (restore) - executed via exec.Command in db.go, so not captured by RunComposeCommandSilently
	// 3. update configuration URLs (via exec.Command)
	// 4. rm -rf var/cache/dev (via RunComposeCommandSilently)
	// 5. oro:platform:update (via RunComposeCommandSilently)

	foundCacheClear := false
	foundPlatformUpdate := false
	foundDBClear := false
	foundExtension := false
	for _, call := range calls {
		if contains(call, "DROP DATABASE") && contains(call, "postgres") {
			foundDBClear = true
		}
		if contains(call, "CREATE EXTENSION") && contains(call, "uuid-ossp") {
			foundExtension = true
		}
		if contains(call, "rm") && contains(call, "var/cache/dev") {
			foundCacheClear = true
		}
		if contains(call, "oro:platform:update") {
			foundPlatformUpdate = true
		}
	}

	if !foundDBClear {
		t.Errorf("Expected database clear command (DROP DATABASE) not found in calls")
	}

	if !foundExtension {
		t.Errorf("Expected extension creation command (CREATE EXTENSION) not found in calls")
	}

	if !foundCacheClear {
		t.Errorf("Expected cache clear command not found in calls")
	}
	if !foundPlatformUpdate {
		t.Errorf("Expected platform update command not found in calls")
	}
}

// TestRestoreCacheClearDetachesBeforeDeleting guards the restore failure where clearing the cache
// ran as a bare `rm -rf var/cache/dev` against a live stack: web, consumer and cron kept warming
// the cache while rm walked it, so the command died with
// "rm: can't remove 'var/cache/dev/oro_data/doctrine_metadata': Directory not empty" and the
// restore continued on a half-cleared cache.
func TestRestoreCacheClearDetachesBeforeDeleting(t *testing.T) {
	if !strings.Contains(cacheClearScript, "mv ") {
		t.Fatal("cache clear must rename var/cache/dev before removing it")
	}
	if strings.Index(cacheClearScript, "mv ") > strings.Index(cacheClearScript, "rm -rf") {
		t.Fatal("the rename has to come before the removal, or the removal still races the writers")
	}
}

// TestDbRestorePausesTheKernelsAroundTheCacheClear covers the race the pause exists for: while
// var/cache/dev is detached, web, the consumer and cron rebuild it concurrently with
// oro:platform:update, and Oro's extend-class writer fails on the directory a second creator
// already made ("Could not create cache directory .../oro_entities/Extend/Entity").
func TestDbRestorePausesTheKernelsAroundTheCacheClear(t *testing.T) {
	oldRunSilently := docker.RunComposeCommandSilently
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	oldDbExec := dbExec
	defer func() {
		docker.RunComposeCommandSilently = oldRunSilently
		docker.RunComposeCommandWithOutput = oldRunWithOutput
		dbExec = oldDbExec
	}()
	docker.ResetEnsuredServices()

	var calls [][]string
	docker.RunComposeCommandSilently = func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		if args[0] == "ps" {
			// Only some of the kernels are up, so the restore may only touch those.
			var statuses []docker.ServiceStatus
			for _, name := range args[1:] {
				if name == "web" || name == "cron" || name == "db" || name == "application" {
					statuses = append(statuses, docker.ServiceStatus{Service: name, State: "running", Health: "healthy"})
				}
			}
			body, err := json.Marshal(statuses)
			if err != nil {
				t.Fatal(err)
			}
			return body, nil
		}
		return []byte(""), nil
	}
	dbExec = func(_ io.Reader, _ io.Writer, args ...string) error {
		calls = append(calls, args)
		return nil
	}

	tmpFile, err := os.CreateTemp("", "backup*.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	rootCmd.SetArgs([]string{"db", "restore", tmpFile.Name()})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	indexOf := func(match func([]string) bool) int {
		for i, call := range calls {
			if match(call) {
				return i
			}
		}
		return -1
	}

	stop := indexOf(func(c []string) bool { return len(c) > 0 && c[0] == "stop" })
	start := indexOf(func(c []string) bool { return len(c) > 0 && c[0] == "start" })
	cacheClear := indexOf(func(c []string) bool { return contains(c, "var/cache/dev") })
	update := indexOf(func(c []string) bool { return contains(c, "oro:platform:update") })

	if stop < 0 || start < 0 {
		t.Fatalf("restore did not pause and resume the kernels: %v", calls)
	}
	if !(stop < cacheClear && cacheClear < update && update < start) {
		t.Errorf("the pause does not bracket the cache clear and the platform update: stop=%d clear=%d update=%d start=%d\n%v",
			stop, cacheClear, update, start, calls)
	}

	// Symmetric, and limited to what was actually up: a consumer a developer deliberately keeps
	// down must not be started by a restore.
	want := "web cron"
	if got := strings.Join(calls[stop][1:], " "); got != want {
		t.Errorf("stopped %q, want %q", got, want)
	}
	if got := strings.Join(calls[start][1:], " "); got != want {
		t.Errorf("started %q, want %q", got, want)
	}
}
