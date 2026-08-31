package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/algoritma-dev/orobox/internal/docker"

	"github.com/spf13/viper"
)

// TestInitRunArgsNeverStartsDependencies guards the fix for the install crash where the
// long-running stack (web/consumer/cron), pulled in by the `application` service's
// depends_on, restart-looped against an empty database and kept rewriting var/cache/dev.
// That raced oro:install's cache wipe and left a half-written entity-config cache, which
// crashed oro:entity-extend:cache:clear with a null cache key.
func TestInitRunArgsNeverStartsDependencies(t *testing.T) {
	args := initRunArgs("application", "test", "-f", "composer.json")

	if !containsArg(args, "--no-deps") {
		t.Errorf("initRunArgs must pass --no-deps so init never starts web/consumer/cron, got: %v", args)
	}

	// --no-deps is a `run` option, so it has to precede the service name.
	noDeps, service := indexOfArg(args, "--no-deps"), indexOfArg(args, "application")
	if noDeps > service {
		t.Errorf("--no-deps must come before the service name, got: %v", args)
	}

	// The one-off container still has to clean up after itself and stay non-interactive.
	for _, want := range []string{"run", "--rm", "-T"} {
		if !containsArg(args, want) {
			t.Errorf("initRunArgs missing %q, got: %v", want, args)
		}
	}

	// Extra arguments are preserved, in order, after the options.
	if got := strings.Join(args[len(args)-4:], " "); got != "application test -f composer.json" {
		t.Errorf("initRunArgs should append extra args verbatim, got trailing %q", got)
	}

	// With no extra arguments it is just the option prefix, ready for credential args.
	if got := strings.Join(initRunArgs(), " "); got != "run --rm -T --no-deps" {
		t.Errorf("initRunArgs() prefix = %q", got)
	}
}

func containsArg(args []string, want string) bool {
	return indexOfArg(args, want) >= 0
}

func indexOfArg(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

// A failed installation must make `orobox init` exit non-zero.
//
// The exit code is the only signal a CI job, a Makefile or the e2e harness can act on: while
// init printed "OroCommerce download/install failed" and still returned 0, every caller saw a
// successful bootstrap and carried on against a project that was never installed.
func TestInitCommandFailsWhenInstallationFails(t *testing.T) {
	restore := stubInitEnvironment(t)
	defer restore()

	performInstallation = func() bool { return false }

	rootCmd.SetArgs([]string{"init", "-t", "project", "-v", "6.1"})
	if err := rootCmd.Execute(); err == nil {
		t.Error("expected a non-nil error so Execute exits 1, got nil")
	}
}

// The mirror case: a successful installation must keep exiting 0.
func TestInitCommandSucceedsWhenInstallationSucceeds(t *testing.T) {
	restore := stubInitEnvironment(t)
	defer restore()

	performInstallation = func() bool { return true }

	rootCmd.SetArgs([]string{"init", "-t", "project", "-v", "6.1"})
	if err := rootCmd.Execute(); err != nil {
		t.Errorf("expected no error for a successful install, got %v", err)
	}
}

// stubInitEnvironment runs `orobox init` in a throwaway directory and replaces the two things
// it would otherwise reach for: the compose runners and performInstallation itself. init no
// longer creates or changes into a directory of its own — `orobox create` does that — so the
// working directory is what points it at the throwaway tree. It returns the restore func the
// caller defers.
func stubInitEnvironment(t *testing.T) func() {
	t.Helper()

	tmpDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	oldPerform := performInstallation
	oldInstallType := installType
	oldStdin := stdin
	oldRun := docker.RunComposeCommand
	oldRunSilently := docker.RunComposeCommandSilently

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	installType = ""
	// generateConfig still prompts: -t and -v cover the type and the version, so the answers
	// are the version selection, the host and the root taken as defaults, then "n" to SSL so
	// the run never reaches mkcert. The remaining service questions hit EOF and take theirs.
	stdin = strings.NewReader("\n\n\nn\n")
	docker.RunComposeCommand = func(string, ...string) error { return nil }
	docker.RunComposeCommandSilently = func(string, ...string) error { return nil }

	return func() {
		performInstallation = oldPerform
		installType = oldInstallType
		stdin = oldStdin
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandSilently = oldRunSilently
		rootCmd.SetArgs(nil)
		if err := os.Chdir(origWd); err != nil {
			t.Fatal(err)
		}
		viper.Reset()
	}
}

func TestGenerateConfig(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "orobox-init-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Simulate interactive input:
	// 1. Installation type (selection 1 = bundle)
	// 2. Bundle class: Algoritma\Bundle\TestBundle\TestBundle
	// 3. OroCommerce version (selection 1 = 7.0)
	// 4. Host: test.local
	// 5. Root: public
	// 6. SSL: n
	// 7. Redis: y
	// 8. RedisInsight: y
	// 9. Mailpit: y
	// 10. RabbitMQ: y
	// 11. Elasticsearch: y
	// 12. Kibana: y
	// 13. Adminer: y
	input := "1\nAlgoritma\\Bundle\\TestBundle\\TestBundle\n1\ntest.local\npublic\nn\ny\ny\ny\ny\ny\ny\ny\n"

	oldType := installType
	installType = ""
	defer func() { installType = oldType }()

	oldStdin := stdin
	stdin = strings.NewReader(input)
	defer func() { stdin = oldStdin }()

	generateConfig()

	// Check if .orobox.yaml was created
	if _, err := os.Stat(".orobox.yaml"); os.IsNotExist(err) {
		t.Fatalf(".orobox.yaml was not created")
	}

	// Verify content if needed
	data, err := os.ReadFile(".orobox.yaml")
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "oro_version: \"7.0\"") {
		t.Errorf("Expected oro_version 7.0 in config, got:\n%s", content)
	}
	if !strings.Contains(content, "host: test.local") {
		t.Errorf("Expected host test.local in config, got:\n%s", content)
	}
}

func TestGenerateConfigProject(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "orobox-init-project-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Project install skips the bundle class/namespace prompt entirely.
	// 1. Installation type (selection 2 = project)
	// 2. OroCommerce version (selection 1 = 7.0)
	// 3. Host: proj.local
	// 4. Root: public
	// 5. SSL: n
	// 6. Redis: n
	// 7. Mailpit: y
	// 8. RabbitMQ: n
	// 9. Elasticsearch: n
	// 10. Adminer: y
	input := "2\n1\nproj.local\npublic\nn\nn\ny\nn\nn\ny\n"

	oldType := installType
	installType = ""
	defer func() { installType = oldType }()

	oldStdin := stdin
	stdin = strings.NewReader(input)
	defer func() { stdin = oldStdin }()

	generateConfig()

	data, err := os.ReadFile(".orobox.yaml")
	if err != nil {
		t.Fatalf(".orobox.yaml was not created: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "type: project") {
		t.Errorf("Expected type project in config, got:\n%s", content)
	}
	if !strings.Contains(content, "host: proj.local") {
		t.Errorf("Expected host proj.local in config, got:\n%s", content)
	}
	// No bundle namespace/class should have been collected (fields stay empty).
	if !strings.Contains(content, `namespace: ""`) || !strings.Contains(content, `class: ""`) {
		t.Errorf("Project config should have empty bundle namespace/class, got:\n%s", content)
	}
}

func TestGenerateConfigDemo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "orobox-init-demo-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Demo installs skip the bundle class/namespace prompt, exactly like project.
	// 1. Installation type (selection 3 = demo)
	// 2. OroCommerce version (selection 1 = 7.0)
	// 3. Host: demo.local
	// 4. Root: public
	// 5. SSL: n
	// 6. Redis: n
	// 7. Mailpit: y
	// 8. RabbitMQ: n
	// 9. Elasticsearch: n
	// 10. Adminer: y
	input := "3\n1\ndemo.local\npublic\nn\nn\ny\nn\nn\ny\n"

	oldType := installType
	installType = ""
	defer func() { installType = oldType }()

	oldStdin := stdin
	stdin = strings.NewReader(input)
	defer func() { stdin = oldStdin }()

	generateConfig()

	data, err := os.ReadFile(".orobox.yaml")
	if err != nil {
		t.Fatalf(".orobox.yaml was not created: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "type: demo") {
		t.Errorf("Expected type demo in config, got:\n%s", content)
	}
	if !strings.Contains(content, "host: demo.local") {
		t.Errorf("Expected host demo.local in config, got:\n%s", content)
	}
	if !strings.Contains(content, `namespace: ""`) || !strings.Contains(content, `class: ""`) {
		t.Errorf("Demo config should have empty bundle namespace/class, got:\n%s", content)
	}
}

func TestValidateConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "orobox-validate-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Test missing file
	if validateConfig() {
		t.Errorf("validateConfig should return false for missing file")
	}

	// Test invalid file
	os.WriteFile(".orobox.yaml", []byte("invalid yaml"), 0644)
	if validateConfig() {
		t.Errorf("validateConfig should return false for invalid yaml")
	}

	// Test valid file
	validYaml := `
type: bundle
namespace: MyNamespace
oro_version: "6.1"
domains:
  - host: localhost
`
	os.WriteFile(".orobox.yaml", []byte(validYaml), 0644)
	if !validateConfig() {
		t.Errorf("validateConfig should return true for valid config")
	}
}

func TestBundlePathFlagRemoved(t *testing.T) {
	if f := initCmd.Flags().Lookup("bundle-path"); f != nil {
		t.Error("init should no longer register the --bundle-path flag (folder creation moved to 'create')")
	}
}

func TestForceInstallFlagRegistered(t *testing.T) {
	flag := initCmd.Flags().Lookup("force-install")
	if flag == nil {
		t.Fatal("init command should register the --force-install flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--force-install default should be false, got %q", flag.DefValue)
	}
}

func TestInitTypeFlagOffersDemo(t *testing.T) {
	flag := initCmd.Flags().Lookup("type")
	if flag == nil {
		t.Fatal("init command should register the --type flag")
	}
	for _, want := range []string{"bundle", "project", "demo"} {
		if !strings.Contains(flag.Usage, want) {
			t.Errorf("--type usage should mention %q, got %q", want, flag.Usage)
		}
	}
}

// generateConfig runs before every other step of `init`, so it is the first place a stdin that
// never reaches EOF can hang the whole command. An inherited pipe nothing writes to and
// nothing closes is exactly that: AskSelection would sit in ReadString for the life of the
// process. The wizard must instead answer from the defaults and return.
func TestGenerateConfigDoesNotBlockOnOpenPipe(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Nothing ever writes to w, and it stays open for the whole test.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	defer r.Close()
	defer w.Close()

	oldStdin := stdin
	stdin = r
	defer func() { stdin = oldStdin }()

	oldType, oldVersion := installType, oroVersion
	installType, oroVersion = "project", "7.0"
	defer func() { installType, oroVersion = oldType, oldVersion }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		generateConfig()
	}()

	select {
	case <-done:
		if err := os.Chdir(origWd); err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		// The working directory is deliberately left in tmpDir. A blocked generateConfig
		// outlives this test — the goroutine is still parked in ReadString — and writes
		// .orobox.yaml whenever it is finally released. Restoring the working directory
		// first would drop that file into the repository and break later tests with state
		// that has nothing to do with them.
		t.Fatal("generateConfig blocked on a stdin pipe that never reaches EOF")
	}

	// Read through tmpDir: the working directory has been restored by now.
	data, err := os.ReadFile(filepath.Join(tmpDir, ".orobox.yaml"))
	if err != nil {
		t.Fatalf(".orobox.yaml was not created: %v", err)
	}
	if content := string(data); !strings.Contains(content, "type: project") {
		t.Errorf("expected type project in config, got:\n%s", content)
	}
}
