package pipeline

import (
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
}

// cacheBustArg is the build argument --no-cache sets to the run ID. Declared right after the
// final FROM, it changes the cache key of every instruction after it, so the layer is really
// rebuilt — Dagger, unlike `docker build`, has no option to ignore its cache. It is declared in
// every stage, so a multi-stage build is rebuilt whole, like `docker build --no-cache`.
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

// layerContextExcludes are the paths left out of the uploaded build context: the patterns of the
// Dockerfile's own `<name>.dockerignore` when it exists, else of the context's .dockerignore —
// the file `docker build` would honour locally — in order, negations (`!keep`) included, plus
// the project's own excludes when the context is the project root (vendor/, var/, node_modules/
// would otherwise be shipped to the engine on every run).
func layerContextExcludes(contextDir, dockerfileName string, projectRoot bool) []string {
	var excludes []string
	if projectRoot {
		excludes = append(excludes, HostExcludes(contextDir)...)
	}
	candidates := []string{".dockerignore"}
	if dockerfileName != "" {
		candidates = append([]string{dockerfileName + ".dockerignore"}, candidates...)
	}
	for _, name := range candidates {
		src, err := os.ReadFile(filepath.Join(contextDir, name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(src), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if neg, ok := strings.CutPrefix(line, "!"); ok {
				excludes = append(excludes, "!"+strings.TrimPrefix(neg, "/"))
				continue
			}
			excludes = append(excludes, strings.TrimPrefix(line, "/"))
		}
		break
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
