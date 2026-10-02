package cmd

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
)

// A failing `docker compose logs` must make the command exit non-zero, like every other
// command that runs compose.
func TestLogsReturnsTheComposeError(t *testing.T) {
	inTempDir(t)
	old := docker.RunComposeCommand
	t.Cleanup(func() { docker.RunComposeCommand = old; rootCmd.SetArgs(nil) })
	docker.RunComposeCommand = func(string, ...string) error { return errors.New("boom") }

	if err := runCommand(t, "logs", "web"); err == nil {
		t.Error("logs returned nil after compose failed; the process would exit 0")
	}
}

// Not every image ships bash (a scratch or busybox image only has sh, or nothing at all), so the
// shell falls back to sh instead of failing with "bash: not found".
func TestShellArgsFallBackToSh(t *testing.T) {
	args := shellExecArgs("whoami")
	joined := strings.Join(args, " ")
	if !strings.HasSuffix(joined, "exec whoami sh -c command -v bash >/dev/null 2>&1 && exec bash || exec sh") {
		t.Errorf("shell args = %q, want an exec into whoami falling back to sh", joined)
	}
}

// help and completion print nothing that depends on the config, so a broken .orobox.yaml must
// not stop them: a user fixing the file still needs `orobox help`, and a completion script
// regenerated from a project directory must not break.
func TestHelpAndCompletionIgnoreABrokenConfig(t *testing.T) {
	inTempDir(t)
	if err := os.WriteFile(".orobox.yaml", []byte("namespace: [unclosed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	exited := false
	oldExit := exitOnConfigError
	exitOnConfigError = func(int) { exited = true }
	t.Cleanup(func() { ConfigError = nil; rootCmd.SetArgs(nil); exitOnConfigError = oldExit })

	// completion writes straight to os.Stdout, which other tests in this package may have
	// swapped for a closed pipe; what matters here is that the config gate lets it through.
	for _, argv := range [][]string{{"help"}, {"completion", "bash"}} {
		exited = false
		_ = runCommand(t, argv...)
		if exited {
			t.Errorf("orobox %v with a broken config was stopped by the config check", argv)
		}
	}
	// The same broken config must still stop an ordinary command, or the test proves nothing.
	exited = false
	_ = runCommand(t, "logs", "web")
	if !exited {
		t.Error("a broken config did not stop `orobox logs`")
	}
	if ConfigError == nil {
		t.Fatal("the broken config was not detected, so this test proves nothing")
	}
}
