package docker

import (
	"fmt"
	"time"

	"github.com/algoritma-dev/orobox/internal/utils"
)

// The poll budget: 180 attempts half a second apart, so a cold Postgres has 90 seconds to come
// up. It is generous on purpose — a first start on a loaded CI runner has to initdb, run
// init-db.sql and then start accepting — and it costs nothing when the server is already there,
// because the first successful poll returns immediately.
//
// They are variables, together with the sleep, so a test can drive the loop without a clock.
var (
	dbReadyAttempts = 180
	dbReadyInterval = 500 * time.Millisecond
	sleepFunc       = time.Sleep
)

// WaitForDatabaseReady blocks until the Postgres service accepts connections.
//
// `docker compose up -d` returns once the container is running, which is several seconds before
// Postgres is listening: on a first start it still has to initialize the data directory and run
// init-db.sql. Every caller that follows the start with a psql — test-init dropping the test
// database, db restore clearing the dev one, the installed-state probe — used to race that
// window and fail with "connection to server on socket /var/run/postgresql/.s.PGSQL.5432
// failed: No such file or directory".
//
// The condition polled is the same one the compose healthcheck uses, pg_isready, rather than the
// healthcheck's own status: those services declare a 60s start_period with the default 30s
// probe interval, so their health stays "starting" long after the server is usable and waiting
// on it would add half a minute to every command.
//
// pg_isready reports on the server, not on a specific database, so this returns as soon as the
// server is up whether or not the database has been created yet — which is what the callers
// need, since some of them are about to create or drop it.
func WaitForDatabaseReady(test bool) error {
	dbUser, _, dbName, container := GetDatabaseCredentialsFor(test)
	args := []string{"exec", "-T", container, "pg_isready", "-U", dbUser, "-d", dbName}

	var lastErr error
	for attempt := 0; attempt < dbReadyAttempts; attempt++ {
		if attempt == 1 {
			// Only announce once it is clear there is something to wait for: the common case
			// is a server that is already accepting and answers on the first poll.
			utils.StartLoader(fmt.Sprintf("Waiting for %s to accept connections...", container))
			defer utils.StopLoader()
		}
		if attempt > 0 {
			sleepFunc(dbReadyInterval)
		}

		_, err := RunComposeCommandWithOutput(args...)
		if err == nil {
			return nil
		}
		lastErr = err
	}

	budget := time.Duration(dbReadyAttempts) * dbReadyInterval
	return fmt.Errorf("%s did not start accepting connections within %s: %w", container, budget, lastErr)
}
