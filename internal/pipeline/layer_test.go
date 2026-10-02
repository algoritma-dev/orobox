package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/spf13/viper"
)

const layerTestPublishedTag = "algoritmadev/orobox:6.1-project-latest"

// loadConfLikeCmd writes .orobox.yaml into dir and loads it the way cmd/deploy.go, cmd/qa.go and
// cmd/test.go do: viper reads the file and the OroConfig comes out of viper.Unmarshal. That path
// splits dotted keys and lowercases them, which is exactly what ApplyProjectLayer has to survive.
func loadConfLikeCmd(t *testing.T, dir, yaml string) *config.OroConfig {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)

	path := filepath.Join(dir, ".orobox.yaml")
	if err := os.WriteFile(path, []byte("type: project\noro_version: \"6.1\"\n"+yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	var conf config.OroConfig
	if err := viper.Unmarshal(&conf); err != nil {
		t.Fatal(err)
	}
	return &conf
}

func writeLayerFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func layerTestPlan(t *testing.T, conf *config.OroConfig, dir string) *Plan {
	t.Helper()
	p := NewChecks(conf, ChecksOptions{ProjectDir: dir, RunQA: true})
	if p.Image != layerTestPublishedTag {
		t.Fatalf("precondition: plan image = %q, want the published tag", p.Image)
	}
	return p
}

func TestApplyProjectLayerNone(t *testing.T) {
	dir := t.TempDir()
	conf := loadConfLikeCmd(t, dir, "")
	p := layerTestPlan(t, conf, dir)

	if err := ApplyProjectLayer(p, conf, dir); err != nil {
		t.Fatalf("ApplyProjectLayer: %v", err)
	}
	if p.Layer != nil {
		t.Errorf("Layer = %+v, want nil without image or php_ini", p.Layer)
	}
	if p.PhpIni != "" {
		t.Errorf("PhpIni = %q, want empty", p.PhpIni)
	}
	if p.Image != layerTestPublishedTag {
		t.Errorf("Image = %q, want it unchanged", p.Image)
	}
}

func TestApplyProjectLayerDeclarative(t *testing.T) {
	dir := t.TempDir()
	conf := loadConfLikeCmd(t, dir, "image:\n  php_extensions: [redis]\n")
	p := layerTestPlan(t, conf, dir)

	if err := ApplyProjectLayer(p, conf, dir); err != nil {
		t.Fatalf("ApplyProjectLayer: %v", err)
	}
	if p.Layer == nil {
		t.Fatal("Layer = nil, want a layer for image.php_extensions")
	}
	if !strings.Contains(string(p.Layer.Dockerfile), "RUN install-php-extensions redis") {
		t.Errorf("Dockerfile does not install the extension:\n%s", p.Layer.Dockerfile)
	}
	if !strings.Contains(string(p.Layer.Dockerfile), "FROM ${"+config.DockerfileBaseImageArg+"}") {
		t.Errorf("Dockerfile does not build on the base image argument:\n%s", p.Layer.Dockerfile)
	}
	if p.Layer.BaseImage != layerTestPublishedTag {
		t.Errorf("BaseImage = %q, want the published tag %q", p.Layer.BaseImage, layerTestPublishedTag)
	}
	if p.Layer.ContextDir != "" {
		t.Errorf("ContextDir = %q, want empty: image.* keys alone need no build context", p.Layer.ContextDir)
	}
	if p.PhpIni != "" {
		t.Errorf("PhpIni = %q, want empty", p.PhpIni)
	}
}

const extendingDockerfile = "ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}\nRUN echo project\n"

func TestApplyProjectLayerProjectDockerfile(t *testing.T) {
	dir := t.TempDir()
	writeLayerFile(t, filepath.Join(dir, "docker", "Dockerfile"), extendingDockerfile)
	conf := loadConfLikeCmd(t, dir, "image:\n  dockerfile: docker/Dockerfile\n  apk: [git]\n")
	p := layerTestPlan(t, conf, dir)

	if err := ApplyProjectLayer(p, conf, dir); err != nil {
		t.Fatalf("ApplyProjectLayer: %v", err)
	}
	if p.Layer == nil {
		t.Fatal("Layer = nil, want a layer for image.dockerfile")
	}
	if want := filepath.Join(dir, "docker"); p.Layer.ContextDir != want {
		t.Errorf("ContextDir = %q, want the Dockerfile's directory %q", p.Layer.ContextDir, want)
	}
	want := docker.RenderLayerDockerfile([]byte(extendingDockerfile), conf.ImageSettings())
	if string(p.Layer.Dockerfile) != string(want) {
		t.Errorf("Dockerfile =\n%s\nwant the dev renderer's output\n%s", p.Layer.Dockerfile, want)
	}
	if p.Layer.BaseImage != layerTestPublishedTag {
		t.Errorf("BaseImage = %q, want %q", p.Layer.BaseImage, layerTestPublishedTag)
	}
}

func TestApplyProjectLayerRejectsDockerfileNotExtendingTheBase(t *testing.T) {
	dir := t.TempDir()
	writeLayerFile(t, filepath.Join(dir, "docker", "Dockerfile"), "FROM alpine:3.20\nRUN echo nope\n")
	conf := loadConfLikeCmd(t, dir, "image:\n  dockerfile: docker/Dockerfile\n")
	p := layerTestPlan(t, conf, dir)

	err := ApplyProjectLayer(p, conf, dir)
	if err == nil {
		t.Fatal("ApplyProjectLayer accepted a Dockerfile whose final FROM is not the base argument")
	}
	if !strings.Contains(err.Error(), config.DockerfileBaseImageArg) {
		t.Errorf("error %q does not name the base image argument", err)
	}
}

func TestApplyProjectLayerMissingDockerfile(t *testing.T) {
	dir := t.TempDir()
	conf := loadConfLikeCmd(t, dir, "image:\n  dockerfile: docker/Dockerfile\n")
	p := layerTestPlan(t, conf, dir)

	err := ApplyProjectLayer(p, conf, dir)
	if err == nil || !strings.Contains(err.Error(), "docker/Dockerfile") {
		t.Fatalf("err = %v, want a read error naming the configured Dockerfile", err)
	}
}

func TestApplyProjectLayerPhpIni(t *testing.T) {
	t.Run("map form", func(t *testing.T) {
		dir := t.TempDir()
		// Dotted and mixed-case directive names: viper.Unmarshal would have turned the first
		// into a nested map and lowercased the second, so this only passes if the values come
		// from the file itself.
		conf := loadConfLikeCmd(t, dir, "php_ini:\n  xdebug.log_level: 0\n  Memory_Limit: 1G\n  opcache.enable: true\n")
		p := layerTestPlan(t, conf, dir)

		if err := ApplyProjectLayer(p, conf, dir); err != nil {
			t.Fatalf("ApplyProjectLayer: %v", err)
		}
		want, err := docker.RenderPhpIni(map[string]any{
			"xdebug.log_level": 0,
			"Memory_Limit":     "1G",
			"opcache.enable":   true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if p.PhpIni != want {
			t.Errorf("PhpIni =\n%s\nwant\n%s", p.PhpIni, want)
		}
		for _, line := range []string{"xdebug.log_level = 0\n", "Memory_Limit = 1G\n"} {
			if !strings.Contains(p.PhpIni, line) {
				t.Errorf("PhpIni lacks %q verbatim:\n%s", line, p.PhpIni)
			}
		}
		if p.Layer != nil {
			t.Errorf("Layer = %+v, want nil: php_ini alone builds nothing", p.Layer)
		}
		if p.Image != layerTestPublishedTag {
			t.Errorf("Image = %q, want it unchanged", p.Image)
		}
	})

	t.Run("file form", func(t *testing.T) {
		dir := t.TempDir()
		const content = "; project settings\nmemory_limit = 2G\n"
		writeLayerFile(t, filepath.Join(dir, "docker", "php.ini"), content)
		conf := loadConfLikeCmd(t, dir, "php_ini: docker/php.ini\n")
		p := layerTestPlan(t, conf, dir)

		if err := ApplyProjectLayer(p, conf, dir); err != nil {
			t.Fatalf("ApplyProjectLayer: %v", err)
		}
		if p.PhpIni != content {
			t.Errorf("PhpIni = %q, want the file's contents %q", p.PhpIni, content)
		}
	})
}

func TestApplyProjectLayerDeprecatedAlias(t *testing.T) {
	dir := t.TempDir()
	writeLayerFile(t, filepath.Join(dir, "docker", "Dockerfile"), extendingDockerfile)

	aliasConf := loadConfLikeCmd(t, dir, "dockerfile: docker/Dockerfile\n")
	alias := layerTestPlan(t, aliasConf, dir)
	if err := ApplyProjectLayer(alias, aliasConf, dir); err != nil {
		t.Fatalf("ApplyProjectLayer (top-level dockerfile): %v", err)
	}

	imageConf := loadConfLikeCmd(t, dir, "image:\n  dockerfile: docker/Dockerfile\n")
	image := layerTestPlan(t, imageConf, dir)
	if err := ApplyProjectLayer(image, imageConf, dir); err != nil {
		t.Fatalf("ApplyProjectLayer (image.dockerfile): %v", err)
	}

	if alias.Layer == nil || image.Layer == nil {
		t.Fatalf("Layer: alias %+v, image %+v; want both set", alias.Layer, image.Layer)
	}
	if alias.Layer.ContextDir != image.Layer.ContextDir ||
		string(alias.Layer.Dockerfile) != string(image.Layer.Dockerfile) ||
		alias.Layer.BaseImage != image.Layer.BaseImage {
		t.Errorf("top-level dockerfile gave %+v, image.dockerfile gave %+v; want the same layer", alias.Layer, image.Layer)
	}
}

func TestLayerSpecBuildOpts(t *testing.T) {
	layer := &LayerSpec{BaseImage: layerTestPublishedTag}
	opts := layer.buildOpts("")

	if opts.Dockerfile != ".orobox.layer.Dockerfile" {
		t.Errorf("Dockerfile = %q, want the rendered file's name in the context", opts.Dockerfile)
	}
	if len(opts.BuildArgs) != 1 ||
		opts.BuildArgs[0].Name != config.DockerfileBaseImageArg ||
		opts.BuildArgs[0].Value != layerTestPublishedTag {
		t.Errorf("BuildArgs = %+v, want only %s=%s", opts.BuildArgs, config.DockerfileBaseImageArg, layerTestPublishedTag)
	}
}

// --no-cache must rebuild the layer too: the run ID goes in a build argument declared right after
// the final FROM, so every RUN of the final stage misses the cache.
func TestLayerSpecNoCacheBustsTheLayer(t *testing.T) {
	l := &LayerSpec{
		Dockerfile: []byte("ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}\nUSER root\nRUN apk add --no-cache git\n"),
		BaseImage:  layerTestPublishedTag,
	}
	if got := l.dockerfileFor(""); got != string(l.Dockerfile) {
		t.Errorf("without a cache bust the Dockerfile must be unchanged, got:\n%s", got)
	}
	got := l.dockerfileFor("run-42")
	if !strings.Contains(got, "FROM ${OROBOX_BASE_IMAGE}\nARG OROBOX_CACHE_BUST\nUSER root") {
		t.Errorf("cache-bust ARG not placed after the final FROM:\n%s", got)
	}
	opts := l.buildOpts("run-42")
	found := false
	for _, a := range opts.BuildArgs {
		if a.Name == "OROBOX_CACHE_BUST" && a.Value == "run-42" {
			found = true
		}
	}
	if !found {
		t.Errorf("build args %v lack OROBOX_CACHE_BUST=run-42", opts.BuildArgs)
	}
}

// The build context is uploaded with the patterns of its .dockerignore excluded, as docker build
// would, and with the project's own excludes when the context is the project root.
func TestLayerContextExcludes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".dockerignore"), []byte("# comment\n\nnode_modules\n!keep.txt\n/var/cache\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := layerContextExcludes(dir, "", false)
	for _, want := range []string{"node_modules", "var/cache"} {
		if !containsString(got, want) {
			t.Errorf("excludes %v lack %q", got, want)
		}
	}
	for _, unwanted := range []string{"# comment", ""} {
		if containsString(got, unwanted) {
			t.Errorf("excludes %v must not contain %q", got, unwanted)
		}
	}
	// A negation re-includes files, so it has to reach the exclude list, in order.
	if !containsString(got, "!keep.txt") {
		t.Errorf("excludes %v lost the negation !keep.txt", got)
	}
	if root := layerContextExcludes(dir, "", true); len(root) <= len(got) {
		t.Errorf("a project-root context must add the project excludes: %v", root)
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// As Docker does, a Dockerfile-specific ignore file replaces the context's .dockerignore.
func TestLayerContextExcludesPrefersTheDockerfileSpecificFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".dockerignore"), []byte("generic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile.dockerignore"), []byte("specific\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := layerContextExcludes(dir, "Dockerfile", false)
	if !containsString(got, "specific") || containsString(got, "generic") {
		t.Errorf("excludes = %v, want only the Dockerfile-specific patterns", got)
	}
}
