package docker

import (
	"testing"

	"github.com/spf13/viper"
)

func TestSanitizeImageName(t *testing.T) {
	cases := map[string]string{
		"my-bundle":       "my-bundle",
		"MyBundle":        "mybundle",
		"acme_shop":       "acme-shop",
		"acme shop 2":     "acme-shop-2",
		"--acme--shop--":  "acme-shop",
		"Åäö":             "project",
		"":                "project",
		"Oro.Custom@2024": "oro-custom-2024",
	}
	for in, want := range cases {
		if got := sanitizeImageName(in); got != want {
			t.Errorf("sanitizeImageName(%q) = %q, want %q", in, got, want)
		}
	}
}

// The published tag and the locally built layer must never collide: PullAllLocalOrobotImages
// pulls every local tag under algoritmadev/orobox, and the layer exists in no registry.
func TestCustomImageRefIsNotThePublishedRepository(t *testing.T) {
	base := BaseImageRef("6.1", "bundle")
	custom := CustomImageRef("6.1", "bundle")

	if base == custom {
		t.Fatalf("the base and custom references are identical: %q", base)
	}
	if IsCustomImageRef(base) {
		t.Errorf("%q must not be treated as a locally built layer", base)
	}
	if !IsCustomImageRef(custom) {
		t.Errorf("%q must be treated as a locally built layer", custom)
	}
	if want := "algoritmadev/orobox:6.1-bundle-latest"; base != want {
		t.Errorf("BaseImageRef = %q, want %q", base, want)
	}
}

// The layer is stale both when the package list changes and when the base image it was built
// on is replaced — an `orobox self-update` does the latter without touching the Dockerfile.
func TestCustomLayerHashCoversTheDockerfileAndTheBaseImage(t *testing.T) {
	const baseID = "sha256:aaaa"
	dockerfile := []byte("FROM base\nRUN apk add --no-cache poppler-utils\n")

	reference := customLayerHash(dockerfile, baseID)

	if again := customLayerHash(dockerfile, baseID); again != reference {
		t.Errorf("the hash is not stable: %q then %q", reference, again)
	}
	if changed := customLayerHash([]byte("FROM base\nRUN apk add --no-cache imagemagick\n"), baseID); changed == reference {
		t.Error("a changed package list must change the hash")
	}
	if changed := customLayerHash(dockerfile, "sha256:bbbb"); changed == reference {
		t.Error("a changed base image must change the hash")
	}
}

func TestComposeNeedsAppImage(t *testing.T) {
	needs := [][]string{
		{"up", "-d"},
		{"run", "--rm", "application", "bash"},
		{"exec", "application", "bash"},
		{"start", "application"},
		{"create"},
	}
	for _, args := range needs {
		if !composeNeedsAppImage(args) {
			t.Errorf("%v should require the application image", args)
		}
	}

	skips := [][]string{
		{"down", "-v"},
		{"logs", "-f"},
		{"config", "--images"},
		{"ps"},
		{"stop"},
		{},
	}
	for _, args := range skips {
		if composeNeedsAppImage(args) {
			t.Errorf("%v should not require the application image", args)
		}
	}
}

// The compose files and the builder must never disagree about which image the stack runs, so
// both read it from here.
func TestProjectImageRefs(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	viper.Set("type", "project")
	viper.Set("oro_version", "6.1")

	base, app := ProjectImageRefs()
	if base != BaseImageRef("6.1", "project") {
		t.Errorf("base = %q", base)
	}
	if app != base {
		t.Errorf("without system_packages the stack must run the published image, got %q", app)
	}

	viper.Set("system_packages", []string{"imagemagick"})
	base, app = ProjectImageRefs()
	if base != BaseImageRef("6.1", "project") {
		t.Errorf("base changed with system_packages: %q", base)
	}
	if !IsCustomImageRef(app) {
		t.Errorf("with system_packages the stack must run the local layer, got %q", app)
	}
}
