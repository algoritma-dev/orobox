package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/algoritma-dev/orobox/internal/config"
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
		t.Errorf("without a custom dockerfile the stack must run the published image, got %q", app)
	}

	viper.Set("dockerfile", "docker/Dockerfile")
	base, app = ProjectImageRefs()
	if base != BaseImageRef("6.1", "project") {
		t.Errorf("base changed with a custom dockerfile: %q", base)
	}
	if !IsCustomImageRef(app) {
		t.Errorf("with a custom dockerfile the stack must run the local layer, got %q", app)
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

// A Dockerfile whose runtime stage is not the Orobox image makes oro_version decide nothing,
// and the stack then fails somewhere far from the config key that caused it. Earlier stages are
// unconstrained, which is what makes compiling a tool and copying it over possible.
func TestCheckExtendsBaseImage(t *testing.T) {
	arg := config.DockerfileBaseImageArg

	accepted := map[string]string{
		"braced": "ARG " + arg + "\nFROM ${" + arg + "}\nRUN apk add --no-cache imagemagick\n",
		"bare":   "ARG " + arg + "\nFROM $" + arg + "\n",
		"named stage": "ARG " + arg + "\n" +
			"FROM golang:1.24-alpine AS builder\nRUN go build ./...\n" +
			"FROM ${" + arg + "}\nCOPY --from=builder /app/tool /usr/local/bin/tool\n",
		"lowercase from": "ARG " + arg + "\nfrom ${" + arg + "}\n",
	}
	for name, content := range accepted {
		t.Run(name, func(t *testing.T) {
			if err := CheckExtendsBaseImage("docker/Dockerfile", []byte(content)); err != nil {
				t.Errorf("expected the Dockerfile to be accepted, got %v", err)
			}
		})
	}

	rejected := map[string]string{
		"hardcoded oro tag": "FROM algoritmadev/orobox:6.1-project-latest\n",
		"unrelated image":   "FROM php:8.4-fpm-alpine\n",
		"final stage is not the base": "ARG " + arg + "\nFROM ${" + arg + "} AS oro\n" +
			"FROM alpine\nCOPY --from=oro /var/www/oro /oro\n",
		"no from at all": "RUN apk add --no-cache imagemagick\n",
	}
	for name, content := range rejected {
		t.Run(name, func(t *testing.T) {
			err := CheckExtendsBaseImage("docker/Dockerfile", []byte(content))
			if err == nil {
				t.Fatal("expected the Dockerfile to be rejected")
			}
			// The message has to say what to write, not just that something is wrong.
			if !strings.Contains(err.Error(), "docker/Dockerfile") {
				t.Errorf("the error should name the configured path, got %v", err)
			}
		})
	}
}

// The layer is stale when anything in the build context changes and when the base image it was
// built on is replaced — an `orobox self-update` does the latter without touching a file.
func TestCustomLayerHashCoversTheContextAndTheBaseImage(t *testing.T) {
	const baseID = "sha256:aaaa"
	dir := t.TempDir()
	dockerfile := filepath.Join(dir, "Dockerfile")
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		// Modification times are part of the hash, and a test that writes twice in the same
		// clock tick would otherwise compare two identical stamps.
		stamp := time.Now().Add(time.Duration(len(content)) * time.Second)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	hash := func() string {
		t.Helper()
		h, err := customLayerHash(dir, baseID, nil)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	write(dockerfile, "FROM ${OROBOX_BASE_IMAGE}\n")
	reference := hash()

	if again := hash(); again != reference {
		t.Errorf("the hash is not stable: %q then %q", reference, again)
	}

	write(dockerfile, "FROM ${OROBOX_BASE_IMAGE}\nRUN apk add --no-cache imagemagick\n")
	if changed := hash(); changed == reference {
		t.Error("an edited Dockerfile must change the hash")
	}
	reference = hash()

	// A file the Dockerfile copies is part of the layer just as much as the Dockerfile is.
	copied := filepath.Join(dir, "php.ini")
	write(copied, "memory_limit=4096M\n")
	if changed := hash(); changed == reference {
		t.Error("a new file in the build context must change the hash")
	}
	reference = hash()

	write(copied, "memory_limit=8192M\n")
	if changed := hash(); changed == reference {
		t.Error("an edited file in the build context must change the hash")
	}
	reference = hash()

	if changed, err := customLayerHash(dir, "sha256:bbbb", nil); err != nil {
		t.Fatal(err)
	} else if changed == reference {
		t.Error("a changed base image must change the hash")
	}
}

func TestCustomLayerHashReportsAMissingContext(t *testing.T) {
	if _, err := customLayerHash(filepath.Join(t.TempDir(), "absent"), "sha256:aaaa", nil); err == nil {
		t.Error("expected an error for a build context that does not exist")
	}
}

// The declarative image.* keys render a Dockerfile that lives only in memory, so the build
// context cannot tell two different layers apart: the rendered text itself has to be hashed.
func TestCustomLayerHashIncludesDockerfile(t *testing.T) {
	dir := t.TempDir()
	a := []byte("ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}\nRUN apk add --no-cache git\n")
	b := []byte("ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}\nRUN apk add --no-cache imagemagick\n")

	hashA, err := customLayerHash(dir, "sha256:aaaa", a)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := customLayerHash(dir, "sha256:aaaa", b)
	if err != nil {
		t.Fatal(err)
	}
	if hashA == hashB {
		t.Error("two different rendered Dockerfiles must produce different hashes")
	}

	again, err := customLayerHash(dir, "sha256:aaaa", append([]byte(nil), a...))
	if err != nil {
		t.Fatal(err)
	}
	if again != hashA {
		t.Errorf("identical Dockerfiles must hash identically: %q vs %q", hashA, again)
	}
}

func TestProjectImageRefsDeclarativeOnly(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	viper.Set("type", "project")
	viper.Set("oro_version", "6.1")
	viper.Set("image.apk", []string{"imagemagick"})

	base, app := ProjectImageRefs()
	if app == base {
		t.Fatalf("image keys alone must put the local layer in front of the stack, got %q", app)
	}
	if want := CustomImageRef("6.1", "project"); app != want {
		t.Errorf("app = %q, want %q", app, want)
	}
}

// Both spellings of the Dockerfile path must resolve to the same tag, or moving the key under
// `image:` would orphan the layer that was already built.
func TestEnsureCustomImageDeprecatedAliasSameRef(t *testing.T) {
	refFor := func(set func()) string {
		viper.Reset()
		viper.Set("type", "project")
		viper.Set("oro_version", "6.1")
		set()
		_, app := ProjectImageRefs()
		return app
	}
	defer viper.Reset()

	old := refFor(func() { viper.Set("dockerfile", "docker/Dockerfile") })
	renamed := refFor(func() { viper.Set("image.dockerfile", "docker/Dockerfile") })
	if old != renamed {
		t.Errorf("deprecated key gives %q, image.dockerfile gives %q", old, renamed)
	}
	if !IsCustomImageRef(old) {
		t.Errorf("expected a local layer reference, got %q", old)
	}
}

func TestLayerSource(t *testing.T) {
	defer viper.Reset()
	cases := map[string]struct {
		set  func()
		want string
	}{
		"dockerfile only":   {func() { viper.Set("image.dockerfile", "docker/Dockerfile") }, "docker/Dockerfile"},
		"deprecated key":    {func() { viper.Set("dockerfile", "docker/Dockerfile") }, "docker/Dockerfile"},
		"keys only":         {func() { viper.Set("image.apk", []string{"git"}) }, "image keys"},
		"dockerfile + keys": {func() { viper.Set("image.dockerfile", "docker/Dockerfile"); viper.Set("image.run", []string{"true"}) }, "docker/Dockerfile + image keys"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			viper.Reset()
			c.set()
			if got := layerSource(); got != c.want {
				t.Errorf("layerSource() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestCustomImageBuildArgs(t *testing.T) {
	args := customImageBuildArgs("orobox-custom/p:6.1-project", "algoritmadev/orobox:6.1-project-latest", "/ctx", "abc123", false)

	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-f -",
		"--build-arg " + config.DockerfileBaseImageArg + "=algoritmadev/orobox:6.1-project-latest",
		"--label " + customImageHashLabel + "=abc123",
		"-t orobox-custom/p:6.1-project",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %v are missing %q", args, want)
		}
	}
	if args[len(args)-1] != "/ctx" {
		t.Errorf("the build context must come last, got %v", args)
	}
	if strings.Contains(joined, "--no-cache") || strings.Contains(joined, "--pull") {
		t.Errorf("a normal build must keep the layer cache: %v", args)
	}

	rebuild := customImageBuildArgs("r", "b", "/ctx", "h", true)
	rebuildJoined := strings.Join(rebuild, " ")
	if !strings.Contains(rebuildJoined, "--no-cache") || !strings.Contains(rebuildJoined, "--pull") {
		t.Errorf("--rebuild must bypass the cache and refresh the base: %v", rebuild)
	}
	if rebuild[len(rebuild)-1] != "/ctx" {
		t.Errorf("the build context must come last, got %v", rebuild)
	}
}
