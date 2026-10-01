package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/scaffold"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/viper"
)

// runExtend runs `orobox extend ...` against a throwaway project and returns what went to the
// payload stream and what the human-facing printer wrote. The real templates are used: the command
// is thin, and the interesting thing to check is what it prints for what the scaffolder did.
func runExtend(t *testing.T, root string, args ...string) (payload, human string, err error) {
	t.Helper()

	configPath := filepath.Join(root, ".orobox.yaml")
	if _, statErr := os.Stat(configPath); statErr != nil {
		if err := os.WriteFile(configPath, []byte("type: project\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	oldTemplates := scaffold.Templates
	scaffold.Templates = os.DirFS("..")
	t.Cleanup(func() {
		scaffold.Templates = oldTemplates
		// initConfig records the minimal test config as invalid (no oro_version) and never clears
		// the package-level error, which would stop the next test's command.
		ConfigError = nil
		rootCmd.SetArgs(nil)
		extendLocal = false
		if flag := extendComposeCmd.Flags().Lookup("local"); flag != nil {
			_ = flag.Value.Set("false")
			flag.Changed = false
		}
		extendForce = false
		if flag := extendAddCmd.Flags().Lookup("force"); flag != nil {
			_ = flag.Value.Set("false")
			flag.Changed = false
		}
		resetConfigFlag(t)
		viper.Reset()
		resetGlobalFlags(t)
		output.SetAgent(false)
	})

	var out, errs, printed bytes.Buffer
	restoreOutput := output.SetWriters(&out, &errs)
	defer restoreOutput()
	restoreUtils := utils.SetWriter(&printed)
	defer restoreUtils()

	// initConfig points viper at the file itself (SetConfigName clears any earlier SetConfigFile),
	// so --config is how a test aims the command at its project.
	rootCmd.SetArgs(append([]string{"extend", "--config", configPath}, args...))
	err = rootCmd.Execute()
	return out.String(), printed.String(), err
}

func TestExtendComposePrintsOneReceiptPerFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/vendor/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	payload, _, err := runExtend(t, root, "compose", "--local")
	if err != nil {
		t.Fatalf("extend compose --local: %v", err)
	}

	want := "created .orobox.compose.local.yaml\nupdated .gitignore\n"
	if payload != want {
		t.Errorf("payload = %q, want %q", payload, want)
	}
	if _, err := os.Stat(filepath.Join(root, ".orobox.compose.local.yaml")); err != nil {
		t.Errorf("local override was not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".orobox.compose.yaml")); err == nil {
		t.Error("--local also created the team override")
	}
}

func TestExtendImageSetsTheConfigKey(t *testing.T) {
	root := t.TempDir()

	payload, _, err := runExtend(t, root, "image")
	if err != nil {
		t.Fatalf("extend image: %v", err)
	}

	want := "created docker/Dockerfile\nupdated .orobox.yaml\n"
	if payload != want {
		t.Errorf("payload = %q, want %q", payload, want)
	}
	cfg, err := os.ReadFile(filepath.Join(root, ".orobox.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "dockerfile: docker/Dockerfile") {
		t.Errorf("image.dockerfile not set:\n%s", cfg)
	}
}

// A config whose image.dockerfile points at a file that does not exist is refused by every other
// command, so extend has to run in spite of it: creating that file is how the config gets fixed.
func TestExtendImageCreatesTheMissingConfiguredDockerfile(t *testing.T) {
	root := t.TempDir()
	cfg := "type: project\nimage:\n  dockerfile: build/Dockerfile\n"
	if err := os.WriteFile(filepath.Join(root, ".orobox.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	// initConfig records the missing Dockerfile in the package-level ConfigError, and the command
	// must not be stopped by it.
	payload, _, err := runExtend(t, root, "image")
	if err != nil {
		t.Fatalf("extend image: %v", err)
	}
	if want := "created build/Dockerfile\nskipped .orobox.yaml\n"; payload != want {
		t.Errorf("payload = %q, want %q", payload, want)
	}
}

func TestExtendImageWithoutConfigFails(t *testing.T) {
	root := t.TempDir()
	// runExtend writes a config when none exists, so this case drives the command directly.
	oldTemplates := scaffold.Templates
	scaffold.Templates = os.DirFS("..")
	t.Cleanup(func() {
		scaffold.Templates = oldTemplates
		ConfigError = nil
		rootCmd.SetArgs(nil)
		resetConfigFlag(t)
		viper.Reset()
		resetGlobalFlags(t)
	})

	var out, errs, printed bytes.Buffer
	defer output.SetWriters(&out, &errs)()
	defer utils.SetWriter(&printed)()

	rootCmd.SetArgs([]string{"extend", "--config", filepath.Join(root, ".orobox.yaml"), "image"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("extend image succeeded without a .orobox.yaml")
	}
	if !strings.Contains(printed.String(), "orobox init") {
		t.Errorf("error does not point at `orobox init`: %q", printed.String())
	}
	if out.Len() != 0 {
		t.Errorf("payload = %q, want nothing", out.String())
	}
}

// In agent mode only the receipt lines may remain: the hint printed after them is chrome.
func TestExtendAgentModeKeepsOnlyTheReceipts(t *testing.T) {
	root := t.TempDir()

	payload, human, err := runExtendAgent(t, root, "compose")
	if err != nil {
		t.Fatalf("extend compose: %v", err)
	}
	if want := "created .orobox.compose.yaml\n"; payload != want {
		t.Errorf("payload = %q, want %q", payload, want)
	}
	if human != "" {
		t.Errorf("human output = %q, want nothing in agent mode", human)
	}
}

// resetConfigFlag clears --config: cobra keeps the parsed value, and a stale path would aim the
// next test's initConfig at a directory that no longer exists.
func resetConfigFlag(t *testing.T) {
	t.Helper()
	cfgFile = ""
	if flag := rootCmd.PersistentFlags().Lookup("config"); flag != nil {
		_ = flag.Value.Set("")
		flag.Changed = false
	}
}

func runExtendAgent(t *testing.T, root string, args ...string) (string, string, error) {
	t.Helper()
	return runExtend(t, root, append([]string{"--agent"}, args...)...)
}

func TestExtendAddListsTheRecipes(t *testing.T) {
	root := t.TempDir()

	payload, _, err := runExtend(t, root, "add")
	if err != nil {
		t.Fatalf("extend add: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(payload, "\n"), "\n")
	var names []string
	for _, line := range lines {
		name, description, ok := strings.Cut(line, " — ")
		if !ok || description == "" {
			t.Errorf("line %q is not `name — description`", line)
		}
		names = append(names, name)
	}
	if got, want := strings.Join(names, ","), "blackfire,selenium,sftp,varnish"; got != want {
		t.Errorf("recipes = %s, want %s", got, want)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 1 {
		t.Errorf("listing wrote files: %v", entries)
	}
}

func TestExtendAddPrintsReceiptsThenNotes(t *testing.T) {
	root := t.TempDir()

	payload, human, err := runExtend(t, root, "add", "selenium", "sftp")
	if err != nil {
		t.Fatalf("extend add: %v", err)
	}
	want := "created .orobox.compose.yaml\nupdated .orobox.compose.yaml\ncreated docker/sftp/upload/.gitkeep\n"
	if payload != want {
		t.Errorf("payload = %q, want %q", payload, want)
	}
	selenium, sftp := strings.Index(human, "localhost:7900"), strings.Index(human, "sftp -P 2222")
	if selenium < 0 || sftp < 0 || selenium > sftp {
		t.Errorf("notes missing or out of order:\n%s", human)
	}
}

func TestExtendAddForceReplacesTheService(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".orobox.compose.yaml"), []byte("services:\n  selenium:\n    image: mine:1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, human, err := runExtend(t, root, "add", "selenium"); err == nil || !strings.Contains(human, "selenium") {
		t.Fatalf("extend add over an existing service: err %v, output %q", err, human)
	}

	payload, _, err := runExtend(t, root, "add", "--force", "selenium")
	if err != nil {
		t.Fatalf("extend add --force: %v", err)
	}
	if want := "updated .orobox.compose.yaml\n"; payload != want {
		t.Errorf("payload = %q, want %q", payload, want)
	}
	got, err := os.ReadFile(filepath.Join(root, ".orobox.compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "selenium/standalone-chrome:") {
		t.Errorf("service not replaced:\n%s", got)
	}
}

// An unknown name is caught before the first recipe is applied, so a typo in a list does not leave
// half of it added.
func TestExtendAddUnknownRecipeWritesNothing(t *testing.T) {
	root := t.TempDir()

	payload, human, err := runExtend(t, root, "add", "selenium", "minio")
	if err == nil {
		t.Fatal("extend add accepted an unknown recipe")
	}
	for _, want := range []string{"minio", "blackfire, selenium, sftp, varnish"} {
		if !strings.Contains(human, want) {
			t.Errorf("error does not mention %q: %q", want, human)
		}
	}
	if payload != "" {
		t.Errorf("payload = %q, want nothing", payload)
	}
	if _, err := os.Stat(filepath.Join(root, ".orobox.compose.yaml")); err == nil {
		t.Error("the known recipe was added before the unknown one was refused")
	}
}

func TestExtendAddAgentModeKeepsOnlyTheReceipts(t *testing.T) {
	root := t.TempDir()

	payload, human, err := runExtendAgent(t, root, "add", "varnish")
	if err != nil {
		t.Fatalf("extend add: %v", err)
	}
	if want := "created .orobox.compose.yaml\ncreated docker/varnish/default.vcl\n"; payload != want {
		t.Errorf("payload = %q, want %q", payload, want)
	}
	if human != "" {
		t.Errorf("human output = %q, want nothing in agent mode", human)
	}
}
