// Package pipeline builds and runs the deploy pipeline: Dagger for everything that happens
// before the remote host is touched, PHP Deployer for the remote release itself.
package pipeline

import (
	"bytes"
	"io/fs"
	"text/template"

	"github.com/algoritma-dev/orobox/internal/config"
)

// Templates holds the embedded filesystem for deploy-related templates.
// It is assigned in main, next to docker.Templates.
var Templates fs.FS

// Template paths inside Templates.
const (
	recipeTemplatePath = "templates/deploy/oro.php.tmpl"
	stubTemplatePath   = "templates/deploy/deploy.php.tmpl"
)

// The rendered files are PHP that uses Deployer's own {{var}} interpolation, so the Go
// templates use [[ ]] delimiters and leave {{ }} untouched.
const (
	leftDelim  = "[["
	rightDelim = "]]"
)

// templateData is the data both deploy templates render against. Values come from
// internal/config so the Go code and the generated PHP cannot disagree about artifact
// names or defaults.
type templateData struct {
	RecipePath          string
	VendorArtifact      string
	AssetsArtifact      string
	DefaultKeepReleases int
	DefaultSSHPort      int
}

func newTemplateData() templateData {
	return templateData{
		RecipePath:          config.DeployRecipeRelPath,
		VendorArtifact:      config.VendorArtifactName,
		AssetsArtifact:      config.AssetsArtifactName,
		DefaultKeepReleases: config.DefaultKeepReleases,
		DefaultSSHPort:      config.DefaultSSHPort,
	}
}

// RenderRecipe renders the Oro Deployer recipe that orobox owns and rewrites on every
// deploy-init run.
func RenderRecipe() ([]byte, error) {
	return render(recipeTemplatePath)
}

// RenderStub renders the deploy.php entry point, which is written only when absent because
// the project owns it afterwards.
func RenderStub() ([]byte, error) {
	return render(stubTemplatePath)
}

func render(path string) ([]byte, error) {
	raw, err := fs.ReadFile(Templates, path)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New(path).Delims(leftDelim, rightDelim).Parse(string(raw))
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, newTemplateData()); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
