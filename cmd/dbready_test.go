package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
)

// The give-up path — a server that never accepts — is covered in the docker package by
// TestWaitForDatabaseReadyGivesUp; driving it from here would mean either a 90 second wait or
// exporting the poll budget purely for a test.

// recordCompose captures every compose invocation in one ordered log, across both runners, so a
// test can assert what ran before what.
func recordCompose(t *testing.T) *[]string {
	t.Helper()

	oldRun := docker.RunComposeCommand
	oldRunSilently := docker.RunComposeCommandSilently
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	oldDbExec := dbExec
	t.Cleanup(func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandSilently = oldRunSilently
		docker.RunComposeCommandWithOutput = oldRunWithOutput
		dbExec = oldDbExec
		rootCmd.SetArgs(nil)
		docker.ResetEnsuredServices()
	})

	var log []string
	record := func(_ string, args ...string) error {
		log = append(log, strings.Join(args, " "))
		return nil
	}
	docker.RunComposeCommand = record
	docker.RunComposeCommandSilently = record
	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		log = append(log, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "ps" {
			return psRunningRequested(args), nil
		}
		return []byte("0"), nil
	}
	dbExec = func(_ io.Reader, _ io.Writer, args ...string) error {
		log = append(log, strings.Join(args, " "))
		return nil
	}
	docker.ResetEnsuredServices()

	return &log
}

// indexOfCall returns the position of the first logged call containing every fragment, or -1.
func indexOfCall(log []string, fragments ...string) int {
	for i, call := range log {
		match := true
		for _, f := range fragments {
			if !strings.Contains(call, f) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// `docker compose up -d` returns once the container is running, several seconds before Postgres
// listens. test-init is where that cost a CI run: the DROP DATABASE right after the start failed
// with "connection to server on socket /var/run/postgresql/.s.PGSQL.5432 failed: No such file or
// directory", and the whole test environment was left un-provisioned.
func TestTestInitWaitsForPostgresBeforeAnyPsql(t *testing.T) {
	inTempDir(t)
	log := recordCompose(t)
	docker.SetDatabaseInitializedCache(true, false)

	if err := runCommand(t, "test-init"); err != nil {
		t.Fatalf("test-init returned %v", err)
	}

	ready := indexOfCall(*log, "pg_isready", "db-test")
	if ready < 0 {
		t.Fatalf("test-init never waited for db-test to accept connections; calls: %v", *log)
	}

	psql := indexOfCall(*log, "DROP DATABASE")
	if psql < 0 {
		t.Fatalf("test-init never dropped the test database; calls: %v", *log)
	}
	if ready > psql {
		t.Errorf("test-init ran psql (call %d) before waiting for Postgres (call %d); calls: %v", psql, ready, *log)
	}
}

// db restore clears the dev database with psql immediately after starting it, so it races the
// same window.
func TestDbRestoreWaitsForPostgresBeforeAnyPsql(t *testing.T) {
	inTempDir(t)
	log := recordCompose(t)

	dump := filepath.Join(t.TempDir(), "backup.sql")
	if err := os.WriteFile(dump, []byte("-- dump\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runCommand(t, "db", "restore", dump); err != nil {
		t.Fatalf("db restore returned %v", err)
	}

	ready := indexOfCall(*log, "pg_isready", "db")
	if ready < 0 {
		t.Fatalf("db restore never waited for the database to accept connections; calls: %v", *log)
	}

	psql := indexOfCall(*log, "DROP DATABASE")
	if psql < 0 {
		t.Fatalf("db restore never cleared the database; calls: %v", *log)
	}
	if ready > psql {
		t.Errorf("db restore ran psql (call %d) before waiting for Postgres (call %d); calls: %v", psql, ready, *log)
	}
}
