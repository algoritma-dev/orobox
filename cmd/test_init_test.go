package cmd

import (
	"strings"
	"testing"
)

// TestTestInitRunArgsNeverStartsDependencies mirrors the guard in init_test.go: the one-off
// containers test-init runs must not drag in the `application` service's depends_on chain
// (web, consumer, cron, php-fpm-app, ws and the dev db). Beyond being wasteful for a test
// install that only talks to db-test, consumer and cron restart-loop without an installed
// database and each retry rewrites var/cache, which is what used to corrupt oro:install.
func TestTestInitRunArgsNeverStartsDependencies(t *testing.T) {
	args := testInitRunArgs("php", "bin/console", "oro:install", "--env=test")

	if !containsArg(args, "--no-deps") {
		t.Errorf("testInitRunArgs must pass --no-deps, got: %v", args)
	}

	// --no-deps is a `run` option, so it has to precede the service name.
	if indexOfArg(args, "--no-deps") > indexOfArg(args, "application") {
		t.Errorf("--no-deps must come before the service name, got: %v", args)
	}

	for _, want := range []string{"run", "--rm", "-T", "application"} {
		if !containsArg(args, want) {
			t.Errorf("testInitRunArgs missing %q, got: %v", want, args)
		}
	}

	// The command is appended verbatim after the service name.
	if got := strings.Join(args, " "); got != "run --rm -T --no-deps application php bin/console oro:install --env=test" {
		t.Errorf("unexpected args: %q", got)
	}
}
