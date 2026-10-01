package docker

import (
	"strings"
	"testing"
)

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

// Later stack-customization layers run `RUN install-php-extensions <ext>` FROM this
// image, so the installer must be in the final stage (not only the builder, which is
// discarded) for every PHP line, and fetched from a pinned release rather than
// `latest` so a rebuilt image never changes behaviour underneath an unchanged template.
func TestDockerfileShipsExtensionInstaller(t *testing.T) {
	const path = "../../templates/docker/Dockerfile"

	for _, phpVersion := range []string{"8.2", "8.3", "8.4", "8.5"} {
		t.Run(phpVersion, func(t *testing.T) {
			data := dockerfileData("bundle")
			data["PHPVersion"] = phpVersion
			out := renderRealTemplate(t, path, data)

			mustContain(t, out, "ARG INSTALL_PHP_EXTENSIONS_VERSION=")
			mustContain(t, out, "releases/download/${INSTALL_PHP_EXTENSIONS_VERSION}/install-php-extensions")
			mustNotContain(t, out, "releases/latest/download/install-php-extensions")

			// Present in the final stage, i.e. after the last FROM.
			final := out[strings.LastIndex(out, "\nFROM "):]
			mustContain(t, final, "/usr/local/bin/install-php-extensions")
			// A global ARG is invisible inside a stage until re-declared there.
			mustContain(t, final, "ARG INSTALL_PHP_EXTENSIONS_VERSION\n")
		})
	}
}
