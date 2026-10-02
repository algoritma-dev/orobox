package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/utils"
	"github.com/spf13/cobra"
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

			var stdout, stderr bytes.Buffer
			t.Cleanup(utils.SetWriter(&stdout))
			t.Cleanup(utils.SetErrWriter(&stderr))

			warnDeprecatedConfig(rootCmd)

			got := strings.Contains(stderr.String(), "image.dockerfile")
			if got != c.want {
				t.Errorf("warning printed = %v, want %v (stderr %q)", got, c.want, stderr.String())
			}
			// stdout may be a pipe or a completion script being sourced; the warning must not
			// land in it.
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", stdout.String())
			}
		})
	}
}

// Shell completion and help print something the shell or the user consumes verbatim, so the
// deprecation warning is not added to them at all, not even on stderr.
func TestWarnDeprecatedConfigSkipsCompletionAndHelp(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("dockerfile", "docker/Dockerfile")
	prevErr := ConfigError
	ConfigError = nil
	t.Cleanup(func() { ConfigError = prevErr })

	for _, path := range []string{"completion", "completion bash", "__complete", "__completeNoDesc", "help"} {
		t.Run(path, func(t *testing.T) {
			// cobra adds the completion and help commands lazily, on the first Execute, and the
			// hidden __complete commands only inside Execute itself, so those two are stood in for
			// by a command of the same name.
			rootCmd.InitDefaultCompletionCmd()
			rootCmd.InitDefaultHelpCmd()
			c := &cobra.Command{Use: path}
			if !strings.HasPrefix(path, "__") {
				var err error
				if c, _, err = rootCmd.Find(splitArgs(path)); err != nil {
					t.Fatalf("find %q: %v", path, err)
				}
			}
			var stdout, stderr bytes.Buffer
			t.Cleanup(utils.SetWriter(&stdout))
			t.Cleanup(utils.SetErrWriter(&stderr))

			warnDeprecatedConfig(c)

			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("%s printed stdout %q, stderr %q; want nothing", path, stdout.String(), stderr.String())
			}
		})
	}
}

// loadTestConfig writes yaml as the config file of a throwaway project and runs initConfig on it,
// the way the root command does before any subcommand.
func loadTestConfig(t *testing.T, yaml string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, ".orobox.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	prevErr := ConfigError
	t.Cleanup(func() {
		ConfigError = prevErr
		configErrorIsFilesOnly = false
		resetConfigFlag(t)
		viper.Reset()
		resetGlobalFlags(t)
	})
	viper.Reset()
	cfgFile = path
	initConfig()
	return path
}

const rootTestConfig = "type: project\noro_version: \"6.1\"\ndomains:\n  - host: example.com\n"

// A YAML syntax error used to be swallowed with the rest of ReadInConfig's errors, leaving every
// command running on an empty config.
func TestInitConfigReportsYAMLSyntaxErrors(t *testing.T) {
	path := loadTestConfig(t, rootTestConfig+"services: [redis\n", nil)
	if ConfigError == nil {
		t.Fatal("expected a YAML syntax error to set ConfigError")
	}
	if !strings.Contains(ConfigError.Error(), path) {
		t.Errorf("ConfigError %q should name %s", ConfigError, path)
	}
}

func TestInitConfigWithoutConfigFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	prevErr := ConfigError
	t.Cleanup(func() {
		ConfigError = prevErr
		resetConfigFlag(t)
		viper.Reset()
	})
	viper.Reset()
	cfgFile = ""
	initConfig()
	if ConfigError != nil {
		t.Errorf("no .orobox.yaml must not be an error, got %v", ConfigError)
	}

	// An explicit --config to a file that does not exist yet is the same case: `init --config`
	// is how such a file gets created.
	viper.Reset()
	cfgFile = filepath.Join(dir, "missing.yaml")
	initConfig()
	if ConfigError != nil {
		t.Errorf("a missing --config file must not be an error, got %v", ConfigError)
	}
}

func TestInitConfigAcceptsLegacyNginxPorts(t *testing.T) {
	loadTestConfig(t, rootTestConfig+"nginx_http_port: 8090\n", nil)
	if ConfigError != nil {
		t.Fatalf("nginx_http_port must still load, got %v", ConfigError)
	}
	if httpPort, _ := docker.GetNginxPorts(); httpPort != "8090" {
		t.Errorf("GetNginxPorts() http = %q, want 8090", httpPort)
	}
}

// `down` and `clear` only tear the stack down, so a php_ini file that has gone missing must not
// lock the user out of them; every other command still refuses the config.
func TestConfigGateToleratesMissingFilesForTeardown(t *testing.T) {
	t.Cleanup(docker.ResetPhpIniProblemReported)
	loadTestConfig(t, rootTestConfig+"php_ini: conf/missing.ini\n", nil)
	if ConfigError == nil {
		t.Fatal("expected the missing php_ini file to be reported")
	}
	resetGlobalFlags(t)
	output.SetAgent(false)

	for _, tc := range []struct {
		path    string
		blocked bool
	}{{"down", false}, {"clear", false}, {"clean", false}, {"up", true}, {"test-init", true}} {
		t.Run(tc.path, func(t *testing.T) {
			c, _, err := rootCmd.Find(splitArgs(tc.path))
			if err != nil {
				t.Fatalf("find %q: %v", tc.path, err)
			}
			var stdout, stderr bytes.Buffer
			t.Cleanup(utils.SetWriter(&stdout))
			t.Cleanup(utils.SetErrWriter(&stderr))

			err = configGate(c)
			if tc.blocked {
				if err == nil {
					t.Fatal("expected the config error to stop the command")
				}
				return
			}
			if err != nil {
				t.Fatalf("expected %s to run, got %v", tc.path, err)
			}
			if !strings.Contains(stderr.String(), "php_ini") {
				t.Errorf("expected a warning naming php_ini on stderr, got %q", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", stdout.String())
			}
		})
	}
}

// A config that is broken in itself, not just pointing at a missing file, still stops teardown:
// the stack it describes cannot even be rendered.
func TestConfigGateStillRefusesInvalidConfigForTeardown(t *testing.T) {
	loadTestConfig(t, "type: project\n", nil)
	if ConfigError == nil {
		t.Fatal("expected a config without oro_version to be refused")
	}
	c, _, err := rootCmd.Find([]string{"down"})
	if err != nil {
		t.Fatal(err)
	}
	if err := configGate(c); err == nil {
		t.Error("expected down to be stopped by an invalid config")
	}
}
