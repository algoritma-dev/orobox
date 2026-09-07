package docker

import (
	"strings"
	"testing"
)

const customDockerfilePath = "../../templates/docker/Dockerfile.custom"

func TestCustomDockerfileInstallsTheConfiguredPackages(t *testing.T) {
	out := renderRealTemplate(t, customDockerfilePath, map[string]any{
		"BaseImage":      BaseImageRef("6.1", "project"),
		"SystemPackages": []string{"imagemagick", "poppler-utils"},
	})

	// The layer sits on the published tag, so an image update reaches the project through a
	// plain pull instead of a rebuild of everything the base Dockerfile does.
	mustContain(t, out, "FROM algoritmadev/orobox:6.1-project-latest")
	mustContain(t, out, "apk add --no-cache")
	mustContain(t, out, "imagemagick")
	mustContain(t, out, "poppler-utils")
	// A single layer, so adding a package does not multiply image size by the number of entries.
	if got := strings.Count(out, "RUN apk add"); got != 1 {
		t.Errorf("expected exactly one apk invocation, got %d\n---\n%s", got, out)
	}
}

// A project with extra packages runs a locally built tag that exists in no registry, so the
// compose files have to name that tag and stop Compose from ever trying to pull it. Without
// packages nothing changes: the published image, pulled as before.
func TestComposeImageFollowsSystemPackages(t *testing.T) {
	for _, path := range []string{
		"../../templates/docker/docker-compose.yml",
		"../../templates/docker/docker-compose.setup.yml",
	} {
		t.Run("published image without packages", func(t *testing.T) {
			out := renderRealTemplate(t, path, projectComposeData())
			assertValidYAML(t, path, out)
			mustContain(t, out, "algoritmadev/orobox:6.1-project-latest")
			mustNotContain(t, out, "pull_policy")
		})

		t.Run("local layer with packages", func(t *testing.T) {
			data := projectComposeData()
			data["AppImage"] = CustomImageRef("6.1", "project")
			data["SystemPackages"] = []string{"imagemagick"}

			out := renderRealTemplate(t, path, data)
			assertValidYAML(t, path, out)
			mustContain(t, out, CustomImageRef("6.1", "project"))
			mustContain(t, out, "pull_policy: never")
			mustNotContain(t, out, "algoritmadev/orobox:6.1-project-latest")
		})
	}
}
