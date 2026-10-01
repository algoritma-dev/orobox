package pipeline

import (
	"fmt"
	"os"
	"path/filepath"

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
}

// buildOpts are the DockerBuild options for the layer. Kept apart from the runner so what the
// build is told can be tested without a Dagger engine.
func (l *LayerSpec) buildOpts() dagger.DirectoryDockerBuildOpts {
	return dagger.DirectoryDockerBuildOpts{
		Dockerfile: layerDockerfileName,
		BuildArgs:  []dagger.BuildArg{{Name: config.DockerfileBaseImageArg, Value: l.BaseImage}},
	}
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
