package cmd

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/utils"
	"github.com/spf13/viper"
)

func TestRootCommand(t *testing.T) {
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if rootCmd.Use != "orobox" {
		t.Errorf("Expected use 'oro', got %s", rootCmd.Use)
	}

	if rootCmd.Version != Version {
		t.Errorf("Expected version %s, got %s", Version, rootCmd.Version)
	}
}

func TestVersionFlag(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--version"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	got := buf.String()
	want := "orobox version " + Version + "\n"
	if got != want {
		t.Errorf("Expected %q, got %q", want, got)
	}
}

// resetGlobalFlags clears the persistent flags between Execute calls and restores the viper
// binding --debug is read through.
//
// Two separate hazards. Cobra keeps a flag's parsed value on the command after Execute returns, so
// a subtest that passed --agent leaves it set for the next one. And several tests in this package
// call viper.Reset(), which discards every BindPFlag registration init() made — after one of those
// has run, viper.GetBool("debug") answers false however the flag was passed, so the precedence
// this file asserts could not be observed. The re-bind restores the wiring the binary has; nothing
// outside a test ever calls Reset.
//
// viper.Set is deliberately not used to clear --debug: an explicit Set outranks the bound flag for
// the rest of the process, which would make every later assertion about --debug meaningless.
func resetGlobalFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"agent", "debug"} {
		if err := rootCmd.PersistentFlags().Set(name, "false"); err != nil {
			t.Fatalf("could not reset --%s: %v", name, err)
		}
	}
	if err := viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug")); err != nil {
		t.Fatalf("could not re-bind --debug: %v", err)
	}
}

// The `help` subcommand, not the --help flag: cobra short-circuits the flag before PersistentPreRun
// runs, so a test using it would assert nothing about the wiring.
func TestAgentFlagSetsAgentMode(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"absent", []string{"help"}, false},
		{"present", []string{"--agent", "help"}, true},
		{"debug wins", []string{"--agent", "--debug", "help"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetGlobalFlags(t)
			t.Cleanup(func() {
				output.SetAgent(false)
				rootCmd.SetArgs(nil)
				resetGlobalFlags(t)
			})

			rootCmd.SetArgs(tc.args)
			rootCmd.SetOut(io.Discard)
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("rootCmd.Execute() failed: %v", err)
			}
			if got := output.Agent(); got != tc.want {
				t.Errorf("output.Agent() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAgentFlagIgnoresTheEnvironment(t *testing.T) {
	t.Setenv("ORO_AGENT", "1")
	resetGlobalFlags(t)
	t.Cleanup(func() {
		output.SetAgent(false)
		rootCmd.SetArgs(nil)
		resetGlobalFlags(t)
	})

	rootCmd.SetArgs([]string{"help"})
	rootCmd.SetOut(io.Discard)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}
	if output.Agent() {
		t.Error("ORO_AGENT switched agent mode on; the flag must be the only way in")
	}
}

func TestWarnDeprecatedConfig(t *testing.T) {
	cases := []struct {
		name      string
		agent     bool
		configErr error
		set       func()
		want      bool
	}{
		{"deprecated key", false, nil, func() { viper.Set("dockerfile", "docker/Dockerfile") }, true},
		{"image.dockerfile only", false, nil, func() { viper.Set("image.dockerfile", "docker/Dockerfile") }, false},
		{"no dockerfile", false, nil, func() {}, false},
		{"agent mode keeps stdout clean", true, nil, func() { viper.Set("dockerfile", "docker/Dockerfile") }, false},
		{"invalid config is reported on its own", false, errors.New("invalid"), func() { viper.Set("dockerfile", "docker/Dockerfile") }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			c.set()

			prevErr := ConfigError
			ConfigError = c.configErr
			t.Cleanup(func() { ConfigError = prevErr })

			output.SetAgent(c.agent)
			t.Cleanup(func() { output.SetAgent(false) })

			var buf bytes.Buffer
			t.Cleanup(utils.SetWriter(&buf))

			warnDeprecatedConfig()

			got := strings.Contains(buf.String(), "image.dockerfile")
			if got != c.want {
				t.Errorf("warning printed = %v, want %v (output %q)", got, c.want, buf.String())
			}
		})
	}
}
