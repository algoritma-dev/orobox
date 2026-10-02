package docker

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/composeoverride"
	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/utils"
)

// overrideProject returns a project directory holding the given files, with an internal
// directory next to it, and resets the package state writeComposeOverrides leaves behind so
// one test cannot leak an error or an analysis into the next.
func overrideProject(t *testing.T, files map[string]string) (projectDir, internalDir string) {
	t.Helper()
	projectDir = t.TempDir()
	internalDir = t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(projectDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	resetWarned()
	t.Cleanup(func() {
		overrideErr = nil
		overrideAnalyses = nil
		resetWarned()
	})
	return projectDir, internalDir
}

// lastFlagValues returns the values that follow each -f in args.
func lastFlagValues(args []string) []string {
	var files []string
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-f" {
			files = append(files, args[i+1])
		}
	}
	return files
}

func TestWriteComposeOverridesResolves(t *testing.T) {
	projectDir, internalDir := overrideProject(t, map[string]string{
		".orobox.compose.yaml": "services:\n  application:\n    volumes:\n      - ./x:/y\n",
	})

	changed, err := writeComposeOverrides(internalDir, projectDir)
	if err != nil {
		t.Fatalf("writeComposeOverrides: %v", err)
	}
	if !changed {
		t.Error("expected the first write to report a change")
	}

	got, err := os.ReadFile(filepath.Join(internalDir, "compose.project.resolved.yaml"))
	if err != nil {
		t.Fatalf("resolved copy was not written: %v", err)
	}
	if want := filepath.Join(projectDir, "x") + ":/y"; !strings.Contains(string(got), want) {
		t.Errorf("resolved copy should contain %q, got:\n%s", want, got)
	}

	// Write-only-on-change keeps `orobox up` from reporting a change on every run.
	changed, err = writeComposeOverrides(internalDir, projectDir)
	if err != nil || changed {
		t.Errorf("an unchanged override must be a no-op, got changed=%v err=%v", changed, err)
	}
}

func TestWriteComposeOverridesSkipsEmpty(t *testing.T) {
	projectDir, _ := overrideProject(t, map[string]string{
		".orobox.compose.yaml": "# nothing here yet\n",
	})
	t.Chdir(projectDir)
	t.Setenv("OROBOX_LOCAL_CONFIG", "1")
	internalDir := filepath.Join(projectDir, ".orobox")
	if err := os.MkdirAll(internalDir, 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
		t.Fatalf("writeComposeOverrides: %v", err)
	}
	if _, err := os.Stat(filepath.Join(internalDir, "compose.project.resolved.yaml")); !os.IsNotExist(err) {
		t.Errorf("a comments-only override must leave no resolved copy, stat error: %v", err)
	}
	for _, f := range lastFlagValues(GetBaseComposeArgs()) {
		if strings.Contains(f, "resolved") {
			t.Errorf("GetBaseComposeArgs must not pass an override that does not exist, got %q", f)
		}
	}
}

func TestWriteComposeOverridesRemovesStale(t *testing.T) {
	projectDir, internalDir := overrideProject(t, nil)
	resolved := filepath.Join(internalDir, "compose.project.resolved.yaml")
	if err := os.WriteFile(resolved, []byte("services: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	changed, err := writeComposeOverrides(internalDir, projectDir)
	if err != nil {
		t.Fatalf("writeComposeOverrides: %v", err)
	}
	if !changed {
		t.Error("removing a stale copy changes the stack, so it must be reported")
	}
	if _, err := os.Stat(resolved); !os.IsNotExist(err) {
		t.Errorf("expected the stale resolved copy to be removed, stat error: %v", err)
	}
}

func TestGetBaseComposeArgsOverrideOrder(t *testing.T) {
	projectDir, _ := overrideProject(t, map[string]string{
		".orobox.compose.yaml":       "services:\n  application:\n    environment:\n      A: \"1\"\n",
		".orobox.compose.local.yaml": "services:\n  application:\n    environment:\n      A: \"2\"\n",
	})
	t.Chdir(projectDir)
	t.Setenv("OROBOX_LOCAL_CONFIG", "1")
	if err := os.MkdirAll(".orobox", 0755); err != nil {
		t.Fatal(err)
	}
	// GetBaseComposeArgs reads the internal dir from the environment, so write there.
	internalDir := filepath.Join(projectDir, ".orobox")

	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
		t.Fatalf("writeComposeOverrides: %v", err)
	}

	files := lastFlagValues(GetBaseComposeArgs())
	if len(files) < 2 {
		t.Fatalf("expected at least the two override files, got %v", files)
	}
	n := len(files)
	if want := filepath.Join(".orobox", "compose.project.resolved.yaml"); files[n-2] != want {
		t.Errorf("second-to-last -f = %q, want %q", files[n-2], want)
	}
	if want := filepath.Join(".orobox", "compose.local.resolved.yaml"); files[n-1] != want {
		t.Errorf("last -f = %q, want %q", files[n-1], want)
	}
}

func TestWriteComposeOverridesInvalidYAML(t *testing.T) {
	projectDir, internalDir := overrideProject(t, map[string]string{
		".orobox.compose.yaml": "services: [unclosed\n",
	})
	// A copy from an earlier, valid run must not survive: it would be passed to compose.
	stale := filepath.Join(internalDir, "compose.project.resolved.yaml")
	if err := os.WriteFile(stale, []byte("services: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := writeComposeOverrides(internalDir, projectDir)
	if err == nil {
		t.Fatal("expected an error for invalid YAML")
	}
	if !strings.Contains(err.Error(), ".orobox.compose.yaml") {
		t.Errorf("error should name the source file, got: %v", err)
	}
	if OverrideError() == nil {
		t.Error("OverrideError must expose the failure to the runners")
	}
	if _, serr := os.Stat(stale); !os.IsNotExist(serr) {
		t.Errorf("a stale resolved copy must be removed on error, stat error: %v", serr)
	}

	// No docker is started: the runners return before building any command. Should a gate
	// regress, the run still stays inside this temporary project (OROBOX_LOCAL_CONFIG pins the
	// internal directory to ./.orobox) instead of reaching the real ~/.config/orobox.
	t.Chdir(projectDir)
	t.Setenv("OROBOX_LOCAL_CONFIG", "1")

	runners := map[string]func() error{
		"RunComposeCommandSilently":      func() error { return RunComposeCommandSilently("x", "up", "-d") },
		"RunSetupComposeCommandSilently": func() error { return RunSetupComposeCommandSilently("x", "up", "-d") },
		"RunComposeCommand":              func() error { return RunComposeCommand("x", "up", "-d") },
		"RunComposeCommandWithOutput": func() error {
			_, err := RunComposeCommandWithOutput("ps")
			return err
		},
		"PullProjectImages": func() error {
			_, err := PullProjectImages()
			return err
		},
	}
	for name, run := range runners {
		if rerr := run(); rerr == nil || !strings.Contains(rerr.Error(), ".orobox.compose.yaml") {
			t.Errorf("%s should return the override error naming the file, got %v", name, rerr)
		}
	}

	// Fixing the file clears the error on the next run.
	if err := os.WriteFile(filepath.Join(projectDir, ".orobox.compose.yaml"), []byte("services: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil || OverrideError() != nil {
		t.Errorf("a valid override must clear the error, got %v / %v", err, OverrideError())
	}
}

func TestWriteComposeOverridesOneBadFileDoesNotHideTheOther(t *testing.T) {
	projectDir, internalDir := overrideProject(t, map[string]string{
		".orobox.compose.yaml":       "- not\n- a mapping\n",
		".orobox.compose.local.yaml": "services:\n  application:\n    environment:\n      A: \"2\"\n",
	})

	_, err := writeComposeOverrides(internalDir, projectDir)
	if err == nil || !strings.Contains(err.Error(), ".orobox.compose.yaml") {
		t.Fatalf("expected an error naming the team file, got %v", err)
	}
	if strings.Contains(err.Error(), ".orobox.compose.local.yaml") {
		t.Errorf("the valid local file must not be blamed: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(internalDir, "compose.local.resolved.yaml")); serr != nil {
		t.Errorf("the valid file should still be resolved: %v", serr)
	}
}

func TestOverrideWarnings(t *testing.T) {
	projectDir, internalDir := overrideProject(t, map[string]string{
		".orobox.compose.yaml": "services:\n  application:\n    volumes:\n      - ./missing:/data\n  web:\n    image: x\n",
	})

	var printed bytes.Buffer
	defer utils.SetWriter(&printed)()

	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
		t.Fatalf("writeComposeOverrides: %v", err)
	}

	out := printed.String()
	missing := filepath.Join(projectDir, "missing")
	if want := "bind mount source does not exist: " + missing; !strings.Contains(out, want) {
		t.Errorf("expected a warning containing %q, got:\n%s", want, out)
	}
	if want := "web redefines image; it is detached from oro_version and the custom layer"; !strings.Contains(out, want) {
		t.Errorf("expected a warning containing %q, got:\n%s", want, out)
	}
}

// EnsureDockerCompose runs several times per command and re-analyzes the same files each time;
// the same finding must not be printed again.
func TestOverrideWarningsPrintOncePerProcess(t *testing.T) {
	projectDir, internalDir := overrideProject(t, map[string]string{
		".orobox.compose.yaml": "services:\n  application:\n    volumes:\n      - ./missing:/data\n",
	})

	var printed bytes.Buffer
	defer utils.SetWriter(&printed)()

	for i := 0; i < 2; i++ {
		if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
			t.Fatalf("writeComposeOverrides: %v", err)
		}
	}

	missing := filepath.Join(projectDir, "missing")
	if n := strings.Count(printed.String(), "bind mount source does not exist: "+missing); n != 1 {
		t.Errorf("expected the warning once, printed %d times:\n%s", n, printed.String())
	}

	// A different finding is still reported.
	if err := os.WriteFile(filepath.Join(projectDir, ".orobox.compose.local.yaml"),
		[]byte("services:\n  application:\n    volumes:\n      - ./other:/data\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
		t.Fatalf("writeComposeOverrides: %v", err)
	}
	if !strings.Contains(printed.String(), filepath.Join(projectDir, "other")) {
		t.Errorf("a distinct warning must still print:\n%s", printed.String())
	}
}

func TestIgnoredPhpIniWarningPrintsOncePerProcess(t *testing.T) {
	resetWarned()
	defer resetWarned()
	var printed bytes.Buffer
	defer utils.SetWriter(&printed)()

	// A line break has no php.ini spelling, so rendering fails and the settings are dropped.
	ini := config.PhpIni{Values: map[string]any{"memory_limit": "1G\nx"}}
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		syncPhpIni(dir, dir, ini)
	}
	if n := strings.Count(printed.String(), "Ignoring php_ini"); n != 1 {
		t.Errorf("expected one warning, got %d:\n%s", n, printed.String())
	}
}

func TestOverrideAnalysisMergesBothFiles(t *testing.T) {
	projectDir, internalDir := overrideProject(t, map[string]string{
		".orobox.compose.yaml": "services:\n  minio:\n    image: minio\n    labels:\n      dev.orobox.url: http://team\n" +
			"  docs:\n    build: ./docs\n    labels:\n      dev.orobox.url: http://docs\n",
		".orobox.compose.local.yaml": "services:\n  minio:\n    labels:\n      dev.orobox.url: http://local\n",
	})

	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
		t.Fatalf("writeComposeOverrides: %v", err)
	}

	a := OverrideAnalysis()
	if !a.HasBuild {
		t.Error("HasBuild should be true when either file has a build key")
	}
	if len(a.URLs) != 2 || a.URLs[0].Service != "docs" || a.URLs[1].Service != "minio" {
		t.Fatalf("URLs should be sorted by service and deduplicated, got %+v", a.URLs)
	}
	if a.URLs[1].URL != "http://local" {
		t.Errorf("the local file must win for a duplicated service, got %q", a.URLs[1].URL)
	}
}

// writeGenerated puts minimal generated compose files in internalDir, so the override wiring
// knows which services the stack defines in each mode.
func writeGenerated(t *testing.T, internalDir string) {
	t.Helper()
	files := map[string]string{
		"docker-compose.yml":       "services:\n  application:\n    image: app\n  web:\n    image: app\n",
		"docker-compose.setup.yml": "services:\n  install:\n    image: app\n",
		"docker-compose.test.yml":  "services:\n  db-test:\n    image: postgres\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(internalDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// An override that tweaks db-test (defined only for test commands) or a disabled optional
// service must not make compose reject every other command with "has neither an image nor a
// build context". The base copy drops what the stack does not define for normal commands; the
// test copy keeps db-test.
func TestWriteComposeOverridesPrunesServicesTheStackDoesNotDefine(t *testing.T) {
	projectDir, internalDir := overrideProject(t, map[string]string{
		".orobox.compose.yaml": "services:\n" +
			"  application:\n    environment:\n      A: \"1\"\n" +
			"  db-test:\n    environment:\n      B: \"2\"\n" +
			"  redis:\n    environment:\n      C: \"3\"\n" +
			"  minio:\n    image: minio/minio\n",
	})
	writeGenerated(t, internalDir)

	var printed bytes.Buffer
	defer utils.SetWriter(&printed)()

	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
		t.Fatalf("writeComposeOverrides: %v", err)
	}

	base, err := composeoverride.ServiceNames(mustRead(t, filepath.Join(internalDir, "compose.project.resolved.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"application", "minio"}; strings.Join(base, ",") != strings.Join(want, ",") {
		t.Errorf("base copy services = %v, want %v", base, want)
	}

	test, err := composeoverride.ServiceNames(mustRead(t, filepath.Join(internalDir, "compose.project.resolved.test.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"application", "db-test", "minio"}; strings.Join(test, ",") != strings.Join(want, ",") {
		t.Errorf("test copy services = %v, want %v", test, want)
	}

	out := printed.String()
	if !strings.Contains(out, "redis") {
		t.Errorf("a service the stack never defines must be reported, got:\n%s", out)
	}
	if strings.Contains(out, "db-test") {
		t.Errorf("db-test exists for test commands and must not be reported, got:\n%s", out)
	}
}

// Without the generated files (nothing rendered yet) there is no list to prune against, and
// the override is passed through whole rather than guessed at.
func TestWriteComposeOverridesKeepsEverythingWithoutGeneratedFiles(t *testing.T) {
	projectDir, internalDir := overrideProject(t, map[string]string{
		".orobox.compose.yaml": "services:\n  application:\n    environment:\n      A: \"1\"\n",
	})
	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
		t.Fatal(err)
	}
	names, err := composeoverride.ServiceNames(mustRead(t, filepath.Join(internalDir, "compose.project.resolved.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "application" {
		t.Errorf("services = %v, want [application]", names)
	}
}

func TestGetBaseComposeArgsPicksTheTestCopyForTestCommands(t *testing.T) {
	projectDir, _ := overrideProject(t, map[string]string{
		".orobox.compose.yaml": "services:\n  db-test:\n    environment:\n      B: \"2\"\n",
	})
	t.Chdir(projectDir)
	t.Setenv("OROBOX_LOCAL_CONFIG", "1")
	internalDir := filepath.Join(projectDir, ".orobox")
	if err := os.MkdirAll(internalDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeGenerated(t, internalDir)
	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
		t.Fatal(err)
	}

	SetIncludeTestFiles(true)
	defer SetIncludeTestFiles(false)
	files := lastFlagValues(GetBaseComposeArgs())
	if want := filepath.Join(".orobox", "compose.project.resolved.test.yaml"); files[len(files)-1] != want {
		t.Errorf("last -f in test mode = %q, want %q (all: %v)", files[len(files)-1], want, files)
	}

	SetIncludeTestFiles(false)
	for _, f := range lastFlagValues(GetBaseComposeArgs()) {
		if strings.Contains(f, ".test.yaml") {
			t.Errorf("test copy passed outside test commands: %v", f)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// Docker treats a missing source differently by syntax: short syntax creates an empty
// directory, long syntax refuses to start the container. The warning says which.
func TestMissingPathWarningNamesWhatDockerDoes(t *testing.T) {
	projectDir, internalDir := overrideProject(t, map[string]string{
		".orobox.compose.yaml": "services:\n  application:\n    volumes:\n" +
			"      - ./short:/a\n" +
			"      - type: bind\n        source: ./long\n        target: /b\n",
	})
	var printed bytes.Buffer
	defer utils.SetWriter(&printed)()
	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
		t.Fatal(err)
	}
	out := printed.String()
	if want := filepath.Join(projectDir, "short") + " — Docker creates an empty directory there"; !strings.Contains(out, want) {
		t.Errorf("want %q in:\n%s", want, out)
	}
	if want := filepath.Join(projectDir, "long") + " — Docker refuses to start the container"; !strings.Contains(out, want) {
		t.Errorf("want %q in:\n%s", want, out)
	}
}

// The personal file may tweak a service the team file adds (the documented way to move a
// recipe's port): it is pruned against the generated stack plus the team file's services.
func TestLocalOverrideMayTweakATeamService(t *testing.T) {
	projectDir, internalDir := overrideProject(t, map[string]string{
		".orobox.compose.yaml":       "services:\n  varnish:\n    image: varnish:9.1.0\n",
		".orobox.compose.local.yaml": "services:\n  varnish:\n    ports: !override\n      - \"6082:80\"\n",
	})
	writeGenerated(t, internalDir)
	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
		t.Fatal(err)
	}
	local := string(mustRead(t, filepath.Join(internalDir, "compose.local.resolved.yaml")))
	if !strings.Contains(local, "6082:80") {
		t.Errorf("the local tweak of the team's varnish was pruned:\n%s", local)
	}
}

// A local `build:` that only adds args must not get a context on top of the team file's.
func TestLocalOverrideKeepsTheTeamBuildContext(t *testing.T) {
	projectDir, internalDir := overrideProject(t, map[string]string{
		".orobox.compose.yaml":       "services:\n  docs:\n    build:\n      context: ./docs\n",
		".orobox.compose.local.yaml": "services:\n  docs:\n    build:\n      args:\n        A: b\n",
	})
	writeGenerated(t, internalDir)
	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
		t.Fatal(err)
	}
	local := string(mustRead(t, filepath.Join(internalDir, "compose.local.resolved.yaml")))
	if strings.Contains(local, "context:") {
		t.Errorf("a context was inserted into the local build:\n%s", local)
	}
}

// up lists only services it starts: not one pruned from the stack, and not one whose profiles
// come from the other file.
func TestOverrideAnalysisListsOnlyServicesUpStarts(t *testing.T) {
	projectDir, internalDir := overrideProject(t, map[string]string{
		".orobox.compose.yaml": "services:\n" +
			"  tools:\n    image: x\n    labels:\n      dev.orobox.url: http://localhost:1\n" +
			"  redisinsight:\n    labels:\n      dev.orobox.url: http://localhost:2\n" +
			"  minio:\n    image: m\n    labels:\n      dev.orobox.url: http://localhost:3\n",
		".orobox.compose.local.yaml": "services:\n  tools:\n    profiles: [tools]\n",
	})
	writeGenerated(t, internalDir)
	if _, err := writeComposeOverrides(internalDir, projectDir); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, u := range OverrideAnalysis().URLs {
		names = append(names, u.Service)
	}
	if strings.Join(names, ",") != "minio" {
		t.Errorf("URLs for %v, want only minio", names)
	}
}
