package cmd

import (
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
