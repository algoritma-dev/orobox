package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
)

// The exit code is the only thing a CI job, a Makefile or the e2e harness can act on. Every
// command below used to print its failure and return 0, so a caller saw a successful run
// against an environment that was never brought up, a database that was never restored or a
// QA tool set that was never installed.
//
// The cases assert the error reaches Execute, which is what cmd.Execute turns into exit 1.

// composeFailure installs compose mocks that fail whenever the args of a call contain every
// token in match, and succeed otherwise. It restores the real functions on cleanup.
func composeFailure(t *testing.T, match ...string) {
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

	fails := func(args []string) bool {
		if len(match) == 0 {
			return false
		}
		for _, want := range match {
			if !contains(args, want) {
				return false
			}
		}
		return true
	}
	mock := func(_ string, args ...string) error {
		if fails(args) {
			return errors.New("simulated compose failure")
		}
		return nil
	}
	docker.RunComposeCommand = mock
	docker.RunComposeCommandSilently = mock
	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			return psRunningRequested(args), nil
		}
		return []byte("0"), nil
	}
	dbExec = func(io.Reader, io.Writer, ...string) error { return nil }
	docker.ResetEnsuredServices()
}

// inTempDir runs the case from a throwaway working directory. `qa-init` writes its
// configuration stubs into the checkout it is pointed at, so without this a successful run
// would drop phpstan.neon, rector.php and friends into the package directory.
func inTempDir(t *testing.T) {
	t.Helper()

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWd); err != nil {
			t.Fatal(err)
		}
	})
}

// runCommand executes argv against rootCmd with its output captured, and returns the error
// Execute would exit on.
func runCommand(t *testing.T, argv ...string) error {
	t.Helper()

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(argv)
	return rootCmd.Execute()
}

func TestComposeFailuresExitNonZero(t *testing.T) {
	cases := []struct {
		name string
		// failOn is the set of tokens that identifies the compose call to fail.
		failOn []string
		argv   []string
	}{
		{"up", []string{"up", "-d"}, []string{"up"}},
		{"down", []string{"down", "--remove-orphans"}, []string{"down"}},
		{"clear", []string{"down", "-v"}, []string{"clear"}},
		{"qa-init", []string{"mkdir -p /var/www/oro/vendor-bin/qa"}, []string{"qa-init"}},
		{"test-init drop database", []string{"DROP DATABASE IF EXISTS oro_db_test WITH (FORCE);"}, []string{"test-init"}},
		{"test-init oro:install", []string{"oro:install"}, []string{"test-init"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inTempDir(t)
			composeFailure(t, c.failOn...)
			if err := runCommand(t, c.argv...); err == nil {
				t.Errorf("orobox %v returned nil after a failed %v; the process would exit 0", c.argv, c.failOn)
			}
		})
	}
}

// A compose stack that comes up must keep exiting 0: the point of the change is the failure
// path, not a command that now reports every run as broken.
func TestComposeSuccessExitsZero(t *testing.T) {
	// These also prove the failure cases above are caused by the injected failure and not by
	// something unrelated in the command: with nothing injected, every one of them exits 0.
	for _, argv := range [][]string{{"up"}, {"down"}, {"clear"}, {"qa-init"}, {"test-init"}} {
		t.Run(argv[0], func(t *testing.T) {
			inTempDir(t)
			composeFailure(t) // no match: every call succeeds
			if err := runCommand(t, argv...); err != nil {
				t.Errorf("orobox %v returned %v on a successful run", argv, err)
			}
		})
	}
}

func TestDbBackupFailureExitsNonZero(t *testing.T) {
	composeFailure(t)
	dbExec = func(io.Reader, io.Writer, ...string) error { return errors.New("pg_dump exploded") }

	dump := filepath.Join(t.TempDir(), "backup.sql")
	if err := runCommand(t, "db", "backup", dump); err == nil {
		t.Error("db backup returned nil after pg_dump failed; the process would exit 0")
	}
}

func TestDbRestoreMissingFileExitsNonZero(t *testing.T) {
	composeFailure(t)

	missing := filepath.Join(t.TempDir(), "nope.sql")
	if err := runCommand(t, "db", "restore", missing); err == nil {
		t.Error("db restore returned nil for a backup file that does not exist")
	}
}

func TestDbRestoreFailureExitsNonZero(t *testing.T) {
	composeFailure(t)
	dbExec = func(io.Reader, io.Writer, ...string) error { return errors.New("psql exploded") }

	dump := filepath.Join(t.TempDir(), "backup.sql")
	if err := os.WriteFile(dump, []byte("-- dump\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runCommand(t, "db", "restore", dump); err == nil {
		t.Error("db restore returned nil after psql failed; the process would exit 0")
	}
}

// The schema migration is part of the restore: reporting "Restore completed successfully!"
// after oro:platform:update failed left a database the application cannot boot against
// behind a zero exit code.
func TestDbRestorePlatformUpdateFailureExitsNonZero(t *testing.T) {
	composeFailure(t, "oro:platform:update")

	dump := filepath.Join(t.TempDir(), "backup.sql")
	if err := os.WriteFile(dump, []byte("-- dump\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runCommand(t, "db", "restore", dump); err == nil {
		t.Error("db restore returned nil after oro:platform:update failed")
	}
}
