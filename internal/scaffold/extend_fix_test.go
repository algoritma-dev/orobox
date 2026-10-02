package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The default Dockerfile gets a directory of its own: its directory is the build context, which
// is hashed on every container start, and docker/ also holds recipe data and fixtures that
// change at run time.
func TestExtendImageDefaultsToItsOwnBuildContext(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", extendConfigWithComment)

	if _, err := ExtendImage(cfgIn(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docker", "image", "Dockerfile")); err != nil {
		t.Errorf("Dockerfile not created at docker/image/Dockerfile: %v", err)
	}
	if !strings.Contains(readProjectFile(t, dir, ".orobox.yaml"), "dockerfile: docker/image/Dockerfile") {
		t.Errorf("image.dockerfile not set:\n%s", readProjectFile(t, dir, ".orobox.yaml"))
	}
}

// The config edit is the step that can still fail (here: image is an alias), so it runs before
// the Dockerfile is written; a failure must not leave a Dockerfile nothing points at.
func TestExtendImageWritesNothingWhenTheConfigEditFails(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", "x: &img\n  apk: [git]\nimage: *img\n")

	if _, err := ExtendImage(cfgIn(dir)); err == nil {
		t.Fatal("want an error editing through an alias")
	}
	if _, err := os.Stat(filepath.Join(dir, "docker", "image", "Dockerfile")); !os.IsNotExist(err) {
		t.Errorf("an orphan Dockerfile was left behind: %v", err)
	}
}

// A Dockerfile already at the default path that is not an Orobox layer (a production image,
// say) must not be adopted: the next `orobox up` would refuse it or build the wrong thing.
func TestExtendImageRefusesAForeignDockerfile(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", extendConfigWithComment)
	writeProjectFile(t, dir, "docker/image/Dockerfile", "FROM php:8.4-fpm-alpine\n")

	_, err := ExtendImage(cfgIn(dir))
	if err == nil || !strings.Contains(err.Error(), "OROBOX_BASE_IMAGE") {
		t.Fatalf("want a refusal naming OROBOX_BASE_IMAGE, got %v", err)
	}
	if strings.Contains(readProjectFile(t, dir, ".orobox.yaml"), "dockerfile") {
		t.Error("the config was pointed at the foreign Dockerfile")
	}
}

func TestExtendImageAdoptsAnExistingOroboxDockerfile(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", extendConfigWithComment)
	writeProjectFile(t, dir, "docker/image/Dockerfile", "ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}\n")

	receipts, err := ExtendImage(cfgIn(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got := receiptLines(receipts); got != "skipped docker/image/Dockerfile|updated .orobox.yaml" {
		t.Errorf("receipts = %q", got)
	}
}

// Every extend subcommand works against the project's config: without one there is no
// project, and the files would be written somewhere Orobox never looks.
func TestExtendRequiresAConfig(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	if _, err := ExtendCompose(cfgIn(dir), false); err == nil || !strings.Contains(err.Error(), "orobox init") {
		t.Errorf("extend compose without a config: want an error pointing to orobox init, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ComposeOverrideFile)); !os.IsNotExist(err) {
		t.Error("the override was written without a config")
	}
	if _, _, err := AddRecipe(cfgIn(dir), realRecipe(t, "varnish"), false); err == nil {
		t.Error("extend add without a config: want an error")
	}
}

// With --config naming another file, that file is the one edited and reported.
func TestExtendImageEditsTheConfigFileItIsGiven(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, "orobox.dev.yaml", extendConfigWithComment)

	receipts, err := ExtendImage(filepath.Join(dir, "orobox.dev.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := receiptLines(receipts); got != "created docker/image/Dockerfile|updated orobox.dev.yaml" {
		t.Errorf("receipts = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".orobox.yaml")); !os.IsNotExist(err) {
		t.Error("a second .orobox.yaml was created")
	}
}

// A dangling symlink at the target counts as an existing file: writing through it would create
// the file wherever the link points, possibly outside the project.
func TestWriteOnceTreatsADanglingSymlinkAsExisting(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "elsewhere")
	writeProjectFile(t, dir, ".orobox.yaml", extendConfigWithComment)
	if err := os.MkdirAll(filepath.Join(dir, "docker", "image"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "docker", "image", "Dockerfile")); err != nil {
		t.Fatal(err)
	}

	if _, err := ExtendImage(cfgIn(dir)); err == nil {
		t.Error("a dangling symlink at the default path must be refused, not adopted")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Errorf("a file was written through the dangling symlink: %v", err)
	}
	if strings.Contains(readProjectFile(t, dir, ".orobox.yaml"), "dockerfile") {
		t.Error("the config was pointed at the dangling symlink")
	}
}

// A directory symlinked out of the project must not be written into.
func TestWriteRefusesADirectoryLinkedOutsideTheProject(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	outside := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", extendConfigWithComment)
	if err := os.Symlink(outside, filepath.Join(dir, "docker")); err != nil {
		t.Fatal(err)
	}

	if _, err := ExtendImage(cfgIn(dir)); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("want a refusal to write outside the project, got %v", err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("files were written outside the project: %v", entries)
	}
}

func receiptLines(rs []Receipt) string {
	var lines []string
	for _, r := range rs {
		lines = append(lines, r.String())
	}
	return strings.Join(lines, "|")
}

// A planned write that would be refused (here: docker/ links outside the project) must be refused
// before anything is written, so the recipe is not left half applied.
func TestAddRecipeRefusesBeforeWritingAnything(t *testing.T) {
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", "type: project\noro_version: \"7.0\"\n")
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "docker")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddRecipe(cfgIn(dir), realRecipe(t, "varnish"), false); err == nil {
		t.Fatal("want a refusal")
	}
	if _, err := os.Stat(filepath.Join(dir, ComposeOverrideFile)); !os.IsNotExist(err) {
		t.Error(".orobox.compose.yaml was written although the recipe was refused")
	}
}

// A file that is itself a symlink to somewhere outside the project is not written through.
func TestAddRecipeRefusesAnEnvFileLinkedOutsideTheProject(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(t.TempDir(), "shared.env")
	if err := os.WriteFile(shared, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, dir, ".orobox.yaml", "type: project\noro_version: \"7.0\"\n")
	if err := os.Symlink(shared, filepath.Join(dir, ".env")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddRecipe(cfgIn(dir), realRecipe(t, "blackfire"), false); err == nil {
		t.Fatal("want a refusal to write through the symlink")
	}
	if got, _ := os.ReadFile(shared); string(got) != "A=1\n" {
		t.Errorf("the shared file outside the project was modified:\n%s", got)
	}
}

// Something at the default path that is not a readable regular file is not adopted.
func TestExtendImageRefusesANonRegularFileAtTheDefaultPath(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", extendConfigWithComment)
	if err := os.MkdirAll(filepath.Join(dir, "docker", "image", "Dockerfile"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtendImage(cfgIn(dir)); err == nil {
		t.Fatal("want a refusal")
	}
	if strings.Contains(readProjectFile(t, dir, ".orobox.yaml"), "dockerfile") {
		t.Error("the config was pointed at a directory")
	}
}

// A config the final write would refuse is found before the Dockerfile is written, so the
// refusal leaves no Dockerfile the config does not name.
func TestExtendImageRefusesAConfigLinkedOutsideBeforeWriting(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	shared := filepath.Join(t.TempDir(), "orobox.yaml")
	if err := os.WriteFile(shared, []byte(extendConfigWithComment), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, cfgIn(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtendImage(cfgIn(dir)); err == nil {
		t.Fatal("want a refusal to write through the symlink")
	}
	if _, err := os.Stat(filepath.Join(dir, "docker", "image", "Dockerfile")); err == nil {
		t.Error("the Dockerfile was written before the config was refused")
	}
}

// The same for the .gitignore of `extend compose --local`: no local override without its entry.
func TestExtendComposeLocalRefusesAGitignoreLinkedOutsideBeforeWriting(t *testing.T) {
	useRealTemplates(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, ".orobox.yaml", extendConfigWithComment)
	shared := filepath.Join(t.TempDir(), "gitignore")
	if err := os.WriteFile(shared, []byte("/vendor/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(dir, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtendCompose(cfgIn(dir), true); err == nil {
		t.Fatal("want a refusal to write through the symlink")
	}
	if _, err := os.Stat(filepath.Join(dir, ComposeLocalOverrideFile)); err == nil {
		t.Error("the local override was written before the .gitignore was refused")
	}
}

// A relative project root is compared as the absolute path it stands for.
func TestCheckInsideAcceptsARelativeRoot(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkInside(".", filepath.Join(dir, "sub")); err != nil {
		t.Errorf("relative root, absolute dir: %v", err)
	}
	if err := checkInside(dir, "sub"); err != nil {
		t.Errorf("absolute root, relative dir: %v", err)
	}
}
