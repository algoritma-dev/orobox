package docker

import "testing"

// A project with its own Dockerfile runs a locally built tag that exists in no registry, so the
// compose files have to name that tag and stop Compose from ever trying to pull it. Without one
// nothing changes: the published image, pulled as before.
func TestComposeImageFollowsTheCustomDockerfile(t *testing.T) {
	for _, path := range []string{
		"../../templates/docker/docker-compose.yml",
		"../../templates/docker/docker-compose.setup.yml",
	} {
		t.Run("published image without a custom dockerfile", func(t *testing.T) {
			out := renderRealTemplate(t, path, projectComposeData())
			assertValidYAML(t, path, out)
			mustContain(t, out, "algoritmadev/orobox:6.1-project-latest")
			mustNotContain(t, out, "pull_policy")
		})

		t.Run("local layer with a custom dockerfile", func(t *testing.T) {
			data := projectComposeData()
			data["AppImage"] = CustomImageRef("6.1", "project")
			data["CustomDockerfile"] = "docker/Dockerfile"

			out := renderRealTemplate(t, path, data)
			assertValidYAML(t, path, out)
			mustContain(t, out, CustomImageRef("6.1", "project"))
			mustContain(t, out, "pull_policy: never")
			mustContain(t, out, "docker/Dockerfile")
			mustNotContain(t, out, "algoritmadev/orobox:6.1-project-latest")
		})
	}
}
