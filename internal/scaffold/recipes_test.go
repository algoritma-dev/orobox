package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/composeoverride"
	"github.com/algoritma-dev/orobox/internal/config"

	yamlv3 "gopkg.in/yaml.v3"
)

// realRecipes loads the recipes shipped in the repository, the same files the binary embeds.
func realRecipes(t *testing.T) []Recipe {
	t.Helper()
	recipes, err := LoadRecipes(os.DirFS("../../templates"))
	if err != nil {
		t.Fatalf("LoadRecipes: %v", err)
	}
	return recipes
}

func realRecipe(t *testing.T, name string) Recipe {
	t.Helper()
	for _, r := range realRecipes(t) {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no recipe %q", name)
	return Recipe{}
}

// recipeServices decodes the services of a recipe's compose fragment far enough to look at them.
func recipeServices(t *testing.T, r Recipe) map[string]struct {
	Image string `yaml:"image"`
} {
	t.Helper()
	var compose struct {
		Services map[string]struct {
			Image string `yaml:"image"`
		} `yaml:"services"`
	}
	if err := r.Compose.Decode(&compose); err != nil {
		t.Fatalf("%s: decode compose: %v", r.Name, err)
	}
	return compose.Services
}

// imageTag returns the tag of an image reference, ignoring a pinned digest. A registry host with a
// port (`host:5000/repo`) has a colon too, which is why only the last path segment is looked at.
func imageTag(ref string) string {
	if at := strings.Index(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	last := ref[strings.LastIndex(ref, "/")+1:]
	if colon := strings.LastIndex(last, ":"); colon >= 0 {
		return last[colon+1:]
	}
	return ""
}

func TestRecipesLoadAndPinned(t *testing.T) {
	recipes := realRecipes(t)

	var names []string
	for _, r := range recipes {
		names = append(names, r.Name)
	}
	if want := []string{"blackfire", "selenium", "sftp", "varnish"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("recipes = %v, want %v", names, want)
	}

	for _, r := range recipes {
		if strings.TrimSpace(r.Description) == "" {
			t.Errorf("%s: empty description", r.Name)
		}
		if strings.TrimSpace(r.Notes) == "" {
			t.Errorf("%s: empty notes", r.Name)
		}
		services := recipeServices(t, r)
		if len(services) == 0 {
			t.Errorf("%s: compose fragment declares no service", r.Name)
		}
		for name, svc := range services {
			if tag := imageTag(svc.Image); tag == "" || tag == "latest" {
				t.Errorf("%s: service %s image %q is not pinned to a tag", r.Name, name, svc.Image)
			}
		}
	}
}

// Every fragment has to be a valid override on top of a stack that has a `web` service, which is
// what varnish sits in front of. Compose itself is the judge when it is installed.
func TestRecipesComposeValid(t *testing.T) {
	_, dockerErr := exec.LookPath("docker")

	for _, r := range realRecipes(t) {
		t.Run(r.Name, func(t *testing.T) {
			dir := t.TempDir()
			fragment, err := yamlv3.Marshal(r.Compose)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := composeoverride.Resolve(fragment, dir, "/home/test")
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if dockerErr != nil {
				t.Skip("docker is not on PATH")
			}

			stub := filepath.Join(dir, "stub.yaml")
			override := filepath.Join(dir, "override.yaml")
			writeProjectFile(t, dir, "stub.yaml", "services:\n  web:\n    image: nginx:1.29-alpine\n")
			writeProjectFile(t, dir, "override.yaml", string(resolved))

			cmd := exec.Command("docker", "compose", "--project-directory", dir, "-p", "orobox-recipe-test",
				"-f", stub, "-f", override, "config", "-q")
			// The recipes interpolate their credentials from the project .env; unset, Compose warns
			// about each one, which is noise here.
			cmd.Env = append(os.Environ(), "BLACKFIRE_SERVER_ID=x", "BLACKFIRE_SERVER_TOKEN=x")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("docker compose config: %v\n%s\n--- override ---\n%s", err, out, resolved)
			}
		})
	}
}

func TestAddRecipeRefusesExistingService(t *testing.T) {
	sftp := realRecipe(t, "sftp")
	dir := t.TempDir()
	existing := "# team services\nservices:\n  sftp:\n    image: mine:1\n  other:\n    image: keep:1 # stays\n"
	writeProjectFile(t, dir, ".orobox.compose.yaml", existing)

	_, err := AddRecipe(dir, sftp, false)
	if err == nil || !strings.Contains(err.Error(), "sftp") {
		t.Fatalf("err = %v, want a refusal naming sftp", err)
	}
	if got := readProjectFile(t, dir, ".orobox.compose.yaml"); got != existing {
		t.Errorf("refused recipe changed the override:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "docker", "sftp")); !os.IsNotExist(err) {
		t.Errorf("refused recipe still copied its files (stat err %v)", err)
	}

	if _, err := AddRecipe(dir, sftp, true); err != nil {
		t.Fatalf("AddRecipe --force: %v", err)
	}
	got := readProjectFile(t, dir, ".orobox.compose.yaml")
	var compose struct {
		Services map[string]struct {
			Image string `yaml:"image"`
		} `yaml:"services"`
	}
	if err := yamlv3.Unmarshal([]byte(got), &compose); err != nil {
		t.Fatal(err)
	}
	if img := compose.Services["sftp"].Image; !strings.HasPrefix(img, "atmoz/sftp:") {
		t.Errorf("sftp image = %q, want the recipe's\n%s", img, got)
	}
	if img := compose.Services["other"].Image; img != "keep:1" {
		t.Errorf("other image = %q, want it untouched\n%s", img, got)
	}
	for _, comment := range []string{"# team services", "# stays"} {
		if !strings.Contains(got, comment) {
			t.Errorf("override lost %q:\n%s", comment, got)
		}
	}
}

func TestAddRecipeBlackfire(t *testing.T) {
	blackfire := realRecipe(t, "blackfire")
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", "# keep me\ntype: project\noro_version: \"7.0\"\nimage:\n  php_extensions: [redis]\nphp_ini:\n  memory_limit: 2G\n")
	writeProjectFile(t, dir, ".env", "# mine\nBLACKFIRE_SERVER_ID=abc\n")

	receipts, err := AddRecipe(dir, blackfire, false)
	if err != nil {
		t.Fatalf("AddRecipe: %v", err)
	}
	want := []Receipt{
		{Path: ".orobox.compose.yaml", Action: ActionCreated},
		{Path: ".orobox.yaml", Action: ActionUpdated},
		{Path: ".env", Action: ActionUpdated},
	}
	if !reflect.DeepEqual(receipts, want) {
		t.Errorf("receipts = %v, want %v", receipts, want)
	}

	assertBlackfireConfig := func() {
		t.Helper()
		raw := readProjectFile(t, dir, ".orobox.yaml")
		if !strings.Contains(raw, "# keep me") {
			t.Errorf(".orobox.yaml lost its comment:\n%s", raw)
		}
		cfg, err := config.ParseConfig([]byte(raw))
		if err != nil {
			t.Fatalf("merged .orobox.yaml does not parse: %v\n%s", err, raw)
		}
		if got := cfg.Image.PhpExtensions; !reflect.DeepEqual(got, []string{"redis", "blackfire"}) {
			t.Errorf("image.php_extensions = %v, want [redis blackfire]", got)
		}
		ini, err := cfg.PhpIniSettings()
		if err != nil {
			t.Fatal(err)
		}
		wantIni := map[string]any{"memory_limit": "2G", "blackfire.agent_socket": "tcp://blackfire:8307"}
		if !reflect.DeepEqual(ini.Values, wantIni) {
			t.Errorf("php_ini = %v, want %v", ini.Values, wantIni)
		}

		env := readProjectFile(t, dir, ".env")
		if !strings.HasPrefix(env, "# mine\nBLACKFIRE_SERVER_ID=abc\n") {
			t.Errorf(".env lost the project's own lines:\n%s", env)
		}
		var keys []string
		for _, line := range strings.Split(env, "\n") {
			if k, _, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
				keys = append(keys, k)
			}
		}
		if want := []string{"BLACKFIRE_SERVER_ID", "BLACKFIRE_SERVER_TOKEN"}; !reflect.DeepEqual(keys, want) {
			t.Errorf(".env keys = %v, want %v\n%s", keys, want, env)
		}
		if !strings.Contains(env, "\nBLACKFIRE_SERVER_TOKEN=\n") {
			t.Errorf(".env has no empty BLACKFIRE_SERVER_TOKEN:\n%s", env)
		}
	}
	assertBlackfireConfig()

	// Adding it again changes nothing: the forced service is the same one, lists are
	// deduplicated, and keys the project already has win.
	receipts, err = AddRecipe(dir, blackfire, true)
	if err != nil {
		t.Fatalf("AddRecipe again: %v", err)
	}
	want = []Receipt{
		{Path: ".orobox.compose.yaml", Action: ActionSkipped},
		{Path: ".orobox.yaml", Action: ActionSkipped},
		{Path: ".env", Action: ActionSkipped},
	}
	if !reflect.DeepEqual(receipts, want) {
		t.Errorf("second run receipts = %v, want %v", receipts, want)
	}
	assertBlackfireConfig()
}

func TestAddRecipeCreatesTheEnvFile(t *testing.T) {
	blackfire := realRecipe(t, "blackfire")
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", "type: project\noro_version: \"7.0\"\n")

	receipts, err := AddRecipe(dir, blackfire, false)
	if err != nil {
		t.Fatalf("AddRecipe: %v", err)
	}
	if got := receipts[len(receipts)-1]; got != (Receipt{Path: ".env", Action: ActionCreated}) {
		t.Errorf("last receipt = %v, want created .env", got)
	}
	env := readProjectFile(t, dir, ".env")
	for _, line := range []string{"BLACKFIRE_SERVER_ID=\n", "BLACKFIRE_SERVER_TOKEN=\n"} {
		if !strings.Contains(env, line) {
			t.Errorf(".env misses %q:\n%s", line, env)
		}
	}
}

func TestAddRecipePhpIniFileConflict(t *testing.T) {
	blackfire := realRecipe(t, "blackfire")
	dir := t.TempDir()
	cfg := "type: project\noro_version: \"7.0\"\nphp_ini: docker/php.ini\n"
	writeProjectFile(t, dir, ".orobox.yaml", cfg)

	_, err := AddRecipe(dir, blackfire, false)
	if err == nil || !strings.Contains(err.Error(), "blackfire.agent_socket") {
		t.Fatalf("err = %v, want one naming blackfire.agent_socket", err)
	}
	if !strings.Contains(err.Error(), "docker/php.ini") {
		t.Errorf("err = %v, want it to name the project's ini file", err)
	}
	assertNothingWritten(t, dir, cfg)
}

// extend is exempt from the global config validation, so the recipe has to check the config it is
// about to edit itself, and refuse before writing anything.
func TestAddRecipeInvalidConfigWritesNothing(t *testing.T) {
	blackfire := realRecipe(t, "blackfire")
	for name, cfg := range map[string]string{
		"unknown key": "type: project\noro_version: \"7.0\"\nnot_a_key: 1\n",
		"bad yaml":    "type: [project\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectFile(t, dir, ".orobox.yaml", cfg)
			if _, err := AddRecipe(dir, blackfire, false); err == nil {
				t.Fatal("AddRecipe accepted an invalid .orobox.yaml")
			}
			assertNothingWritten(t, dir, cfg)
		})
	}

	t.Run("missing config", func(t *testing.T) {
		dir := t.TempDir()
		_, err := AddRecipe(dir, blackfire, false)
		if err == nil || !strings.Contains(err.Error(), "orobox init") {
			t.Fatalf("err = %v, want one pointing at orobox init", err)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("files written without a config: %v", entries)
		}
	})
}

// A recipe without a config section does not need a .orobox.yaml at all.
func TestAddRecipeWithoutConfigSection(t *testing.T) {
	dir := t.TempDir()
	receipts, err := AddRecipe(dir, realRecipe(t, "selenium"), false)
	if err != nil {
		t.Fatalf("AddRecipe: %v", err)
	}
	if want := []Receipt{{Path: ".orobox.compose.yaml", Action: ActionCreated}}; !reflect.DeepEqual(receipts, want) {
		t.Errorf("receipts = %v, want %v", receipts, want)
	}
}

func TestAddRecipeCopiesFiles(t *testing.T) {
	varnish := realRecipe(t, "varnish")
	dir := t.TempDir()

	receipts, err := AddRecipe(dir, varnish, false)
	if err != nil {
		t.Fatalf("AddRecipe: %v", err)
	}
	want := []Receipt{
		{Path: ".orobox.compose.yaml", Action: ActionCreated},
		{Path: "docker/varnish/default.vcl", Action: ActionCreated},
	}
	if !reflect.DeepEqual(receipts, want) {
		t.Errorf("receipts = %v, want %v", receipts, want)
	}
	shipped, err := os.ReadFile("../../templates/recipes/varnish/files/default.vcl")
	if err != nil {
		t.Fatal(err)
	}
	if got := readProjectFile(t, dir, "docker/varnish/default.vcl"); got != string(shipped) {
		t.Errorf("default.vcl differs from the shipped one:\n%s", got)
	}

	writeProjectFile(t, dir, "docker/varnish/default.vcl", "vcl 4.1; # mine\n")
	receipts, err = AddRecipe(dir, varnish, true)
	if err != nil {
		t.Fatalf("AddRecipe again: %v", err)
	}
	if got := receipts[len(receipts)-1]; got != (Receipt{Path: "docker/varnish/default.vcl", Action: ActionSkipped}) {
		t.Errorf("last receipt = %v, want skipped docker/varnish/default.vcl", got)
	}
	if got := readProjectFile(t, dir, "docker/varnish/default.vcl"); got != "vcl 4.1; # mine\n" {
		t.Errorf("an existing VCL was overwritten:\n%s", got)
	}
}

// The sftp recipe ships an empty upload directory; the placeholder that keeps it in git is copied
// with the directory structure intact.
func TestAddRecipeCopiesNestedFiles(t *testing.T) {
	dir := t.TempDir()
	receipts, err := AddRecipe(dir, realRecipe(t, "sftp"), false)
	if err != nil {
		t.Fatalf("AddRecipe: %v", err)
	}
	var paths []string
	for _, r := range receipts {
		paths = append(paths, r.Path)
	}
	sort.Strings(paths)
	if want := []string{".orobox.compose.yaml", "docker/sftp/upload/.gitkeep"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestLoadRecipesRejectsMalformedRecipe(t *testing.T) {
	for name, content := range map[string]string{
		"no description": "compose:\n  services:\n    a:\n      image: a:1\n",
		"no compose":     "description: d\nnotes: n\n",
		"unknown key":    "description: d\ncompose:\n  services:\n    a:\n      image: a:1\nservices: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeProjectFile(t, root, "recipes/broken/recipe.yaml", content)
			if _, err := LoadRecipes(os.DirFS(root)); err == nil || !strings.Contains(err.Error(), "broken") {
				t.Errorf("err = %v, want one naming the recipe", err)
			}
		})
	}
}

// assertNothingWritten checks a refused recipe left the project exactly as it was: only the
// config the test wrote, unchanged.
func assertNothingWritten(t *testing.T, dir, cfg string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ".orobox.yaml" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("project holds %v, want only .orobox.yaml", names)
	}
	if got := readProjectFile(t, dir, ".orobox.yaml"); got != cfg {
		t.Errorf(".orobox.yaml changed:\n%s", got)
	}
}
