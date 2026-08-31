package docker

import "testing"

// dockerfileData is the minimal data set the Dockerfile template dereferences.
func dockerfileData(installType string) map[string]any {
	return map[string]any{
		"Type":        installType,
		"PHPVersion":  "8.4",
		"NodeVersion": "22",
		"NpmVersion":  "10",
		"PnpmVersion": "",
		"MemoryLimit": "2048M",
		"OroRootDir":  "/var/www/oro",
	}
}

func TestDockerfileOpcachePerType(t *testing.T) {
	const path = "../../templates/docker/Dockerfile"

	t.Run("bundle keeps opcache off and Xdebug available", func(t *testing.T) {
		out := renderRealTemplate(t, path, dockerfileData("bundle"))
		mustContain(t, out, "opcache.enable=0")
		mustContain(t, out, "opcache.enable_cli=0")
		mustContain(t, out, "opcache.validate_timestamps=1")
		mustContain(t, out, `[ "bundle" != "demo" ]`)
	})

	t.Run("project keeps opcache off", func(t *testing.T) {
		out := renderRealTemplate(t, path, dockerfileData("project"))
		mustContain(t, out, "opcache.enable=0")
		mustContain(t, out, "opcache.validate_timestamps=1")
	})

	t.Run("demo enables opcache and drops Xdebug", func(t *testing.T) {
		out := renderRealTemplate(t, path, dockerfileData("demo"))
		mustContain(t, out, "opcache.enable=1")
		mustContain(t, out, "opcache.enable_cli=1")
		mustContain(t, out, "opcache.validate_timestamps=0")
		// The Xdebug ini lines sit inside a shell `if [ "<type>" != "demo" ]` guard, so the
		// rendered Dockerfile always contains them; what changes is the condition, which is
		// false for demo. Assert the condition, not the absence of the lines.
		mustContain(t, out, `[ "demo" != "demo" ]`)
	})
}

func TestDockerfileSymfonyRecommendedValues(t *testing.T) {
	const path = "../../templates/docker/Dockerfile"

	// These come from https://symfony.com/doc/current/performance.html and apply to every
	// install type: with opcache.enable=0 the buffers are never allocated, and the realpath
	// cache helps development just as much as production.
	want := []string{
		"opcache.memory_consumption=256",
		"opcache.interned_strings_buffer=32",
		"opcache.max_accelerated_files=32531",
		"realpath_cache_size=4096K",
		"realpath_cache_ttl=600",
	}

	for _, installType := range []string{"bundle", "project", "demo"} {
		t.Run(installType, func(t *testing.T) {
			out := renderRealTemplate(t, path, dockerfileData(installType))
			for _, needle := range want {
				mustContain(t, out, needle)
			}
			// Preloading needs a config/preload.php that the Oro application skeleton does
			// not ship, so the directive must stay out of the image.
			mustNotContain(t, out, "opcache.preload")
		})
	}
}
