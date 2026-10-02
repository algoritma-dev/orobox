package pipeline

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dagger.io/dagger"
	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
)

// layerDockerfileName is the name the rendered Dockerfile is written under inside the build
// context. The rendered file is never the project's own Dockerfile byte for byte once image.*
// keys are added, so it gets a name of its own instead of shadowing the project file, and a
// dotted one so it reads as Orobox's rather than the project's.
const layerDockerfileName = ".orobox.layer.Dockerfile"

// LayerSpec is the project's custom image layer as the pipeline builds it: the same Dockerfile
// the dev stack builds with `docker build`, built by Dagger on top of the published image.
type LayerSpec struct {
	// ContextDir is the host directory the build context is uploaded from: the project
	// Dockerfile's directory, or "" when only image.* keys are set and the build needs no files.
	ContextDir string
	// Dockerfile is the rendered layer Dockerfile, as docker.RenderLayerDockerfile produced it.
	Dockerfile []byte
	// BaseImage is the published tag the layer extends, passed as config.DockerfileBaseImageArg.
	BaseImage string
	// ContextIsProjectRoot is set when the project Dockerfile sits next to .orobox.yaml, so the
	// build context is the whole working tree and needs the project's own excludes.
	ContextIsProjectRoot bool
	// DockerfileName is the project Dockerfile's file name, for its own `<name>.dockerignore`.
	DockerfileName string
	// DockerfileOnly is set when the layer is the project Dockerfile unchanged (no image.* lines
	// appended): locally it is then built with `-f <path>`, which honours `<name>.dockerignore`.
	DockerfileOnly bool
}

// cacheBustArg is the build argument --no-cache sets to the run ID. Declared right after a FROM,
// it changes the cache key of every instruction after it in that stage, so the layer is really
// rebuilt — Dagger, unlike `docker build`, has no option to ignore its cache. It is declared after
// every FROM (an ARG is scoped to its stage), so a multi-stage build is rebuilt whole, like
// `docker build --no-cache`.
const cacheBustArg = "OROBOX_CACHE_BUST"

// dockerfileFor returns the Dockerfile to build: the rendered one, with the cache-busting ARG
// inserted when cacheBust is set.
func (l *LayerSpec) dockerfileFor(cacheBust string) string {
	if cacheBust == "" {
		return string(l.Dockerfile)
	}
	return string(docker.InsertAfterEveryFrom(l.Dockerfile, "ARG "+cacheBustArg))
}

// buildOpts are the DockerBuild options for the layer. Kept apart from the runner so what the
// build is told can be tested without a Dagger engine.
func (l *LayerSpec) buildOpts(cacheBust string) dagger.DirectoryDockerBuildOpts {
	args := []dagger.BuildArg{{Name: config.DockerfileBaseImageArg, Value: l.BaseImage}}
	if cacheBust != "" {
		args = append(args, dagger.BuildArg{Name: cacheBustArg, Value: cacheBust})
	}
	return dagger.DirectoryDockerBuildOpts{Dockerfile: layerDockerfileName, BuildArgs: args}
}

// layerIgnoreFileName is the ignore file Dagger's dockerBuild reads for the rendered Dockerfile:
// `<dockerfile>.dockerignore`, before falling back to the context's .dockerignore. Orobox writes
// it with the patterns it chose, so the engine applies exactly the rule the local build applies.
const layerIgnoreFileName = layerDockerfileName + ".dockerignore"

// layerIgnorePatterns are the ignore patterns `docker build` would apply to this layer locally:
// the Dockerfile's own `<name>.dockerignore` when the layer is that Dockerfile alone (built with
// `-f <path>` there) and the file exists, else the context's .dockerignore (the only one a
// Dockerfile read from stdin honours). Parsed the way Docker parses them, in order, negations
// included.
func layerIgnorePatterns(contextDir, dockerfileName string, dockerfileOnly bool) []string {
	candidates := []string{".dockerignore"}
	if dockerfileOnly && dockerfileName != "" {
		candidates = append([]string{dockerfileName + ".dockerignore"}, candidates...)
	}
	for _, name := range candidates {
		src, err := os.ReadFile(filepath.Join(contextDir, name))
		if err != nil {
			continue
		}
		return parseIgnoreFile(src)
	}
	return nil
}

// parseIgnoreFile reads a .dockerignore as Docker does (moby/patternmatcher's ignorefile): a
// UTF-8 BOM is dropped, a line starting with `#` is a comment, whitespace is trimmed, `!`
// negates the (trimmed) rest, and patterns are cleaned and made relative to the context.
func parseIgnoreFile(src []byte) []string {
	src = bytes.TrimPrefix(src, []byte("\ufeff"))
	var patterns []string
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		negate := false
		if rest, ok := strings.CutPrefix(line, "!"); ok {
			negate, line = true, strings.TrimSpace(rest)
			if line == "" {
				continue
			}
		}
		line = filepath.ToSlash(filepath.Clean(line))
		if line != "/" {
			line = strings.TrimPrefix(line, "/")
		}
		if negate {
			line = "!" + line
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// layerUploadExcludes are the paths left out of the uploaded build context: the ignore patterns,
// then the project's own excludes when the context is the project root (vendor/, var/,
// node_modules/ and the git-ignored paths would otherwise be shipped to the engine on every run).
// The project's excludes come last: a later match wins, so a negation in .dockerignore cannot
// pull a git-ignored file back in — a fresh CI checkout would not have it either.
func layerUploadExcludes(contextDir string, patterns []string, projectRoot bool) []string {
	excludes := append([]string{}, patterns...)
	if projectRoot {
		excludes = append(excludes, HostExcludes(contextDir)...)
	}
	return excludes
}

// ApplyProjectLayer makes the plan run on the image and php.ini settings the dev stack runs on:
// the image.* layer (and the deprecated top-level dockerfile) becomes plan.Layer, and php_ini
// becomes plan.PhpIni. With neither configured the plan is left exactly as it was, so a project
// without customizations pays nothing.
//
// hostDir is the directory holding .orobox.yaml. The project Dockerfile and a php_ini file are
// resolved against it, as in the dev environment, so with deploy.source_dir set the paths still
// mean what they mean locally. The context is always the host working tree, even when the steps
// work on a clone: configuration and image then come from the same revision.
//
// php_ini is read through config.GetPhpIni rather than from conf. The commands fill conf with
// viper.Unmarshal, which has already split `xdebug.log_level` into a nested map and lowercased
// `Memory_Limit` by the time conf exists; only a fresh parse of the file has the directive names
// php.ini needs. The image settings have no such problem — lists and paths survive viper — so
// they come from conf.
//
// The pipeline is always an install of type project; the rendered layer does not depend on the
// type, which is why the dev renderer is reused unchanged.
func ApplyProjectLayer(p *Plan, conf *config.OroConfig, hostDir string) error {
	img := conf.ImageSettings()
	if !img.IsEmpty() {
		layer, err := projectLayer(img, hostDir, p.Image)
		if err != nil {
			return err
		}
		p.Layer = layer
	}

	ini, err := config.GetPhpIni()
	if err != nil {
		return err
	}
	content, err := phpIniContent(ini, hostDir)
	if err != nil {
		return err
	}
	p.PhpIni = content

	return nil
}

// projectLayer renders the layer the dev stack would build for img, on top of baseImage.
func projectLayer(img config.ImageConfig, hostDir, baseImage string) (*LayerSpec, error) {
	layer := &LayerSpec{BaseImage: baseImage}

	// The project Dockerfile is optional, exactly as in EnsureCustomImage: image.* keys alone
	// render a complete Dockerfile that needs no context files.
	var projectDockerfile []byte
	if img.Dockerfile != "" {
		path := filepath.Join(hostDir, filepath.FromSlash(img.Dockerfile))
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("could not read the 'image.dockerfile' configured in .orobox.yaml (%s): %w", img.Dockerfile, err)
		}
		// Refused here, before any engine starts, for the reason it is refused locally: a final
		// stage on another image would make oro_version decide nothing.
		if err := docker.CheckExtendsBaseImage(img.Dockerfile, content); err != nil {
			return nil, err
		}
		projectDockerfile = content
		layer.ContextDir = filepath.Dir(path)
		layer.ContextIsProjectRoot = filepath.Clean(layer.ContextDir) == filepath.Clean(hostDir)
		layer.DockerfileName = filepath.Base(path)
	}

	layer.Dockerfile = docker.RenderLayerDockerfile(projectDockerfile, img)
	layer.DockerfileOnly = projectDockerfile != nil && bytes.Equal(layer.Dockerfile, projectDockerfile)
	return layer, nil
}

// phpIniContent returns the text of zz-project.ini for the resolved php_ini setting: rendered
// from the map form, or the project's own file read as-is — the dev stack bind-mounts that file
// unchanged, so the pipeline must not reformat it either.
func phpIniContent(ini config.PhpIni, hostDir string) (string, error) {
	if ini.File != "" {
		content, err := os.ReadFile(filepath.Join(hostDir, filepath.FromSlash(ini.File)))
		if err != nil {
			return "", fmt.Errorf("could not read the 'php_ini' file configured in .orobox.yaml (%s): %w", ini.File, err)
		}
		return string(content), nil
	}
	if len(ini.Values) == 0 {
		return "", nil
	}
	return docker.RenderPhpIni(ini.Values)
}
