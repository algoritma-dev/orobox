package scaffold

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/composeoverride"
	"github.com/algoritma-dev/orobox/internal/docker"

	yamlv3 "gopkg.in/yaml.v3"
)

const extendConfigWithComment = "# keep me\ntype: project\noro_version: \"7.0\"\n"

func writeProjectFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readProjectFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestExtendImageCreates(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", extendConfigWithComment)

	receipts, err := ExtendImage(dir)
	if err != nil {
		t.Fatalf("ExtendImage: %v", err)
	}

	want := []Receipt{
		{Path: "docker/Dockerfile", Action: ActionCreated},
		{Path: ".orobox.yaml", Action: ActionUpdated},
	}
	if !reflect.DeepEqual(receipts, want) {
		t.Errorf("receipts = %v, want %v", receipts, want)
	}

	dockerfile := readProjectFile(t, dir, "docker/Dockerfile")
	if !strings.HasPrefix(dockerfile, "ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}") {
		t.Errorf("Dockerfile does not start with the required header:\n%s", dockerfile)
	}

	cfg := readProjectFile(t, dir, ".orobox.yaml")
	if !strings.Contains(cfg, "# keep me") {
		t.Errorf(".orobox.yaml lost its comment:\n%s", cfg)
	}
	var parsed struct {
		Image struct {
			Dockerfile string `yaml:"dockerfile"`
		} `yaml:"image"`
	}
	if err := yamlv3.Unmarshal([]byte(cfg), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Image.Dockerfile != "docker/Dockerfile" {
		t.Errorf("image.dockerfile = %q, want docker/Dockerfile\n%s", parsed.Image.Dockerfile, cfg)
	}
}

func TestExtendImageNeverOverwrites(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", extendConfigWithComment)
	writeProjectFile(t, dir, "docker/Dockerfile", "mine\n")

	receipts, err := ExtendImage(dir)
	if err != nil {
		t.Fatalf("ExtendImage: %v", err)
	}

	want := []Receipt{
		{Path: "docker/Dockerfile", Action: ActionSkipped},
		{Path: ".orobox.yaml", Action: ActionUpdated},
	}
	if !reflect.DeepEqual(receipts, want) {
		t.Errorf("receipts = %v, want %v", receipts, want)
	}
	if got := readProjectFile(t, dir, "docker/Dockerfile"); got != "mine\n" {
		t.Errorf("existing Dockerfile was modified: %q", got)
	}
	if !strings.Contains(readProjectFile(t, dir, ".orobox.yaml"), "dockerfile: docker/Dockerfile") {
		t.Error("image.dockerfile was not set")
	}
}

func TestExtendImageRendersValidLayer(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", extendConfigWithComment)

	if _, err := ExtendImage(dir); err != nil {
		t.Fatalf("ExtendImage: %v", err)
	}

	content := []byte(readProjectFile(t, dir, "docker/Dockerfile"))
	if err := docker.CheckExtendsBaseImage("docker/Dockerfile", content); err != nil {
		t.Errorf("created Dockerfile is refused: %v", err)
	}
}

func TestExtendImageRequiresConfig(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()

	_, err := ExtendImage(dir)
	if err == nil || !strings.Contains(err.Error(), "orobox init") {
		t.Fatalf("err = %v, want one pointing at `orobox init`", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "docker", "Dockerfile")); statErr == nil {
		t.Error("a Dockerfile was created without a config")
	}
}

func TestExtendImageKeepsConfiguredDockerfile(t *testing.T) {
	useRealTemplates(t)

	tests := []struct {
		name   string
		config string
		path   string
	}{
		{"image key", "image:\n  dockerfile: build/Custom.Dockerfile\n", "build/Custom.Dockerfile"},
		{"deprecated key", "dockerfile: ./build/Old.Dockerfile\n", "build/Old.Dockerfile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectFile(t, dir, ".orobox.yaml", tt.config)

			receipts, err := ExtendImage(dir)
			if err != nil {
				t.Fatalf("ExtendImage: %v", err)
			}

			want := []Receipt{
				{Path: tt.path, Action: ActionCreated},
				{Path: ".orobox.yaml", Action: ActionSkipped},
			}
			if !reflect.DeepEqual(receipts, want) {
				t.Errorf("receipts = %v, want %v", receipts, want)
			}
			if got := readProjectFile(t, dir, ".orobox.yaml"); got != tt.config {
				t.Errorf(".orobox.yaml changed:\n%s", got)
			}
			if _, err := os.Stat(filepath.Join(dir, "docker", "Dockerfile")); err == nil {
				t.Error("a second Dockerfile was created at the default path")
			}
			content := []byte(readProjectFile(t, dir, tt.path))
			if err := docker.CheckExtendsBaseImage(tt.path, content); err != nil {
				t.Errorf("created Dockerfile is refused: %v", err)
			}
		})
	}
}

func TestExtendImageRefusesEscapingPath(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", "image:\n  dockerfile: ../outside/Dockerfile\n")

	if _, err := ExtendImage(dir); err == nil {
		t.Fatal("ExtendImage accepted a Dockerfile path outside the project")
	}
}

func TestExtendImageInvalidYAMLWritesNothing(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", "type: [unclosed\n")

	if _, err := ExtendImage(dir); err == nil {
		t.Fatal("ExtendImage accepted an invalid .orobox.yaml")
	}
	if _, err := os.Stat(filepath.Join(dir, "docker", "Dockerfile")); err == nil {
		t.Error("a Dockerfile was created next to an unreadable config")
	}
}

func TestExtendComposeCreates(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()

	receipts, err := ExtendCompose(dir, false)
	if err != nil {
		t.Fatalf("ExtendCompose: %v", err)
	}
	want := []Receipt{{Path: ".orobox.compose.yaml", Action: ActionCreated}}
	if !reflect.DeepEqual(receipts, want) {
		t.Errorf("receipts = %v, want %v", receipts, want)
	}

	// Never overwrites: a second run reports the file as skipped and leaves it alone.
	writeProjectFile(t, dir, ".orobox.compose.yaml", "services:\n  mine: {}\n")
	receipts, err = ExtendCompose(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	want = []Receipt{{Path: ".orobox.compose.yaml", Action: ActionSkipped}}
	if !reflect.DeepEqual(receipts, want) {
		t.Errorf("receipts = %v, want %v", receipts, want)
	}
	if got := readProjectFile(t, dir, ".orobox.compose.yaml"); got != "services:\n  mine: {}\n" {
		t.Errorf("existing override was modified: %q", got)
	}
}

func TestExtendComposeTemplateParses(t *testing.T) {
	useRealTemplates(t)

	for _, local := range []bool{false, true} {
		name := ".orobox.compose.yaml"
		if local {
			name = ".orobox.compose.local.yaml"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := ExtendCompose(dir, local); err != nil {
				t.Fatalf("ExtendCompose: %v", err)
			}
			content := []byte(readProjectFile(t, dir, name))

			var doc map[string]any
			if err := yamlv3.Unmarshal(content, &doc); err != nil {
				t.Fatalf("template is not valid YAML: %v", err)
			}
			if _, ok := doc["services"]; !ok {
				t.Errorf("template has no services mapping:\n%s", content)
			}
			if _, err := composeoverride.Resolve(content, dir, "/home/test"); err != nil {
				t.Errorf("Resolve refuses the template: %v", err)
			}
		})
	}
}

// The commented examples are what users uncomment, so each one must be a working override once
// the comment markers are gone. A typo in an example would otherwise ship unnoticed.
func TestExtendComposeExamplesAreValidWhenUncommented(t *testing.T) {
	useRealTemplates(t)

	for _, path := range []string{"templates/extend/compose.yaml.tmpl", "templates/extend/compose.local.yaml.tmpl"} {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
			if err != nil {
				t.Fatal(err)
			}
			var uncommented []string
			for _, line := range strings.Split(string(raw), "\n") {
				switch {
				case strings.HasPrefix(line, "# services:"), strings.HasPrefix(line, "# volumes:"),
					strings.HasPrefix(line, "#   ") && !strings.HasPrefix(line, "#   #"):
					uncommented = append(uncommented, strings.TrimPrefix(strings.TrimPrefix(line, "#"), " "))
				}
			}
			content := []byte(strings.Join(uncommented, "\n"))
			var doc map[string]any
			if err := yamlv3.Unmarshal(content, &doc); err != nil {
				t.Fatalf("uncommented examples are not valid YAML: %v\n%s", err, content)
			}
			if _, err := composeoverride.Resolve(content, "/project", "/home/test"); err != nil {
				t.Errorf("Resolve refuses the uncommented examples: %v\n%s", err, content)
			}
		})
	}
}

func TestExtendComposeLocalGitignore(t *testing.T) {
	useRealTemplates(t)

	t.Run("appends once", func(t *testing.T) {
		dir := t.TempDir()
		writeProjectFile(t, dir, ".gitignore", "/vendor/\n")

		receipts, err := ExtendCompose(dir, true)
		if err != nil {
			t.Fatalf("ExtendCompose: %v", err)
		}
		want := []Receipt{
			{Path: ".orobox.compose.local.yaml", Action: ActionCreated},
			{Path: ".gitignore", Action: ActionUpdated},
		}
		if !reflect.DeepEqual(receipts, want) {
			t.Errorf("receipts = %v, want %v", receipts, want)
		}

		receipts, err = ExtendCompose(dir, true)
		if err != nil {
			t.Fatal(err)
		}
		want = []Receipt{
			{Path: ".orobox.compose.local.yaml", Action: ActionSkipped},
			{Path: ".gitignore", Action: ActionSkipped},
		}
		if !reflect.DeepEqual(receipts, want) {
			t.Errorf("second run receipts = %v, want %v", receipts, want)
		}

		got := readProjectFile(t, dir, ".gitignore")
		if got != "/vendor/\n/.orobox.compose.local.yaml\n" {
			t.Errorf(".gitignore = %q", got)
		}
	})

	t.Run("adds the missing trailing newline", func(t *testing.T) {
		dir := t.TempDir()
		writeProjectFile(t, dir, ".gitignore", "/vendor/")

		if _, err := ExtendCompose(dir, true); err != nil {
			t.Fatal(err)
		}
		if got := readProjectFile(t, dir, ".gitignore"); got != "/vendor/\n/.orobox.compose.local.yaml\n" {
			t.Errorf(".gitignore = %q", got)
		}
	})

	t.Run("recognizes the entry without the slash", func(t *testing.T) {
		dir := t.TempDir()
		writeProjectFile(t, dir, ".gitignore", "vendor\n.orobox.compose.local.yaml\n")

		if _, err := ExtendCompose(dir, true); err != nil {
			t.Fatal(err)
		}
		if got := readProjectFile(t, dir, ".gitignore"); got != "vendor\n.orobox.compose.local.yaml\n" {
			t.Errorf(".gitignore was modified: %q", got)
		}
	})

	t.Run("no gitignore is not created", func(t *testing.T) {
		dir := t.TempDir()

		receipts, err := ExtendCompose(dir, true)
		if err != nil {
			t.Fatal(err)
		}
		want := []Receipt{{Path: ".orobox.compose.local.yaml", Action: ActionCreated}}
		if !reflect.DeepEqual(receipts, want) {
			t.Errorf("receipts = %v, want %v", receipts, want)
		}
		if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err == nil {
			t.Error(".gitignore was created")
		}
	})

	t.Run("without --local the gitignore is left alone", func(t *testing.T) {
		dir := t.TempDir()
		writeProjectFile(t, dir, ".gitignore", "/vendor/\n")

		if _, err := ExtendCompose(dir, false); err != nil {
			t.Fatal(err)
		}
		if got := readProjectFile(t, dir, ".gitignore"); got != "/vendor/\n" {
			t.Errorf(".gitignore was modified: %q", got)
		}
	})
}

// The file names are repeated here because scaffold must not depend on how docker discovers
// them; this keeps the two from drifting apart.
func TestExtendFileNamesMatchTheOverrideDiscovery(t *testing.T) {
	var names []string
	for _, f := range docker.OverrideFiles {
		names = append(names, f.Source)
	}
	want := []string{ComposeOverrideFile, ComposeLocalOverrideFile}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("docker.OverrideFiles sources = %v, scaffold uses %v", names, want)
	}
}
