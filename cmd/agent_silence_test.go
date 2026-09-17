package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/utils"
	"github.com/spf13/viper"
)

// captureStdout redirects the process's own stdout for the duration of fn and returns what was
// written to it.
//
// Redirecting utils' writer is not enough: a raw fmt.Print goes to os.Stdout directly, which is
// exactly the kind of call this test exists to catch, so the file descriptor itself has to be
// captured.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("could not create the pipe: %v", err)
	}
	prev := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	defer func() {
		os.Stdout = prev
		_ = w.Close()
	}()

	fn()

	os.Stdout = prev
	_ = w.Close()
	return <-done
}

// TestAgentModeSilencesTheSilentCommands is the net under the conversion of the raw fmt.Print
// calls: a command that still writes its own chrome fails here rather than in a caller's context
// window.
func TestAgentModeSilencesTheSilentCommands(t *testing.T) {
	for _, args := range [][]string{{"up"}, {"down"}} {
		t.Run(args[0], func(t *testing.T) {
			oldRun := docker.RunComposeCommand
			oldSilently := docker.RunComposeCommandSilently
			oldWithOutput := docker.RunComposeCommandWithOutput
			t.Cleanup(func() {
				docker.RunComposeCommand = oldRun
				docker.RunComposeCommandSilently = oldSilently
				docker.RunComposeCommandWithOutput = oldWithOutput
				rootCmd.SetArgs(nil)
				// Cobra keeps --agent on the command after Execute, so without this every later
				// test in the package would run in agent mode.
				resetGlobalFlags(t)
				output.SetAgent(false)
				docker.ResetEnsuredServices()
				viper.Set("type", nil)
			})

			docker.RunComposeCommand = func(string, ...string) error { return nil }
			docker.RunComposeCommandSilently = func(string, ...string) error { return nil }
			docker.RunComposeCommandWithOutput = func(a ...string) ([]byte, error) {
				if len(a) > 0 && a[0] == "ps" {
					return psRunningRequested(a), nil
				}
				return []byte("[]"), nil
			}
			viper.Set("type", "project")

			var human, stdout, stderr bytes.Buffer
			restoreUtils := utils.SetWriter(&human)
			defer restoreUtils()
			restoreOutput := output.SetWriters(&stdout, &stderr)
			defer restoreOutput()

			rootCmd.SetArgs(append([]string{"--agent"}, args...))
			raw := captureStdout(t, func() {
				if err := rootCmd.Execute(); err != nil {
					t.Errorf("rootCmd.Execute() failed: %v", err)
				}
			})

			if raw != "" {
				t.Errorf("%v wrote %q straight to stdout in agent mode, want nothing", args, raw)
			}
			if human.Len() != 0 {
				t.Errorf("%v printed %q in agent mode, want nothing", args, human.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("%v wrote %q to the payload stream, want nothing", args, stdout.String())
			}
		})
	}
}
