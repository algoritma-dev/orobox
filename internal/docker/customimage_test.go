package docker

import (
	"errors"
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
		{"start", "application"},
		{"create"},
	}
	for _, args := range needs {
		if !composeNeedsAppImage(args) {
			t.Errorf("%v should require the application image", args)
		}
	}

	// exec runs in a container that already exists, from whatever image it was created with;
	// building a layer it will not use only delays qa, test and xdebug.
	skips := [][]string{
		{"exec", "application", "bash"},
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

// `FROM` flags and lines that only look like a FROM must not decide which stage is final:
// `--platform` precedes the image, and a heredoc body or a continuation line is not an
// instruction at all.
func TestCheckExtendsBaseImageParsing(t *testing.T) {
	arg := config.DockerfileBaseImageArg
	head := "ARG " + arg + "\nFROM ${" + arg + "}\n"

	accepted := map[string]string{
		"platform flag":           "ARG " + arg + "\nFROM --platform=linux/amd64 ${" + arg + "}\n",
		"platform flag + stage":   "ARG " + arg + "\nFROM --platform=$BUILDPLATFORM ${" + arg + "} AS app\n",
		"heredoc body":            head + "RUN <<EOF\nFROM alpine\nEOF\n",
		"quoted heredoc":          head + "COPY <<'EOT' /etc/motd\nFROM alpine\nEOT\n",
		"dash heredoc":            head + "RUN <<-EOT\n\tFROM alpine\n\tEOT\n",
		"two heredocs":            head + "RUN <<A cat /dev/stdin; <<B cat /dev/stdin\nFROM x\nA\nFROM y\nB\n",
		"continuation":            head + "RUN echo one \\\n  FROM alpine\n",
		"comment in continuation": head + "RUN apk add \\\n# FROM alpine\n  FROM \\\n  alpine\n",
	}
	for name, content := range accepted {
		t.Run("accepts "+name, func(t *testing.T) {
			if err := CheckExtendsBaseImage("docker/Dockerfile", []byte(content)); err != nil {
				t.Errorf("expected the Dockerfile to be accepted, got %v", err)
			}
		})
	}

	rejected := map[string]string{
		"platform flag on another image": "FROM --platform=linux/amd64 alpine\n",
		"FROM after a closed heredoc":    head + "RUN <<EOF\necho hi\nEOF\nFROM alpine\n",
		"FROM after a continuation ends": head + "RUN echo one \\\n  two\nFROM alpine\n",
		"here-string is not a heredoc":   head + "RUN cat <<<\"x\"\nFROM alpine\n",
	}
	for name, content := range rejected {
		t.Run("rejects "+name, func(t *testing.T) {
			if err := CheckExtendsBaseImage("docker/Dockerfile", []byte(content)); err == nil {
				t.Error("expected the Dockerfile to be rejected")
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
	args := customImageBuildArgs("orobox-custom/p:6.1-project", "algoritmadev/orobox:6.1-project-latest", "/ctx", "", "abc123", false)

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

	// The base is pulled before the hash is computed (see ensureCustomImage), so --pull here
	// would only let the build use a base the hash does not describe.
	rebuild := customImageBuildArgs("r", "b", "/ctx", "", "h", true)
	rebuildJoined := strings.Join(rebuild, " ")
	if !strings.Contains(rebuildJoined, "--no-cache") || strings.Contains(rebuildJoined, "--pull") {
		t.Errorf("--rebuild must bypass the cache without pulling during the build: %v", rebuild)
	}
	if rebuild[len(rebuild)-1] != "/ctx" {
		t.Errorf("the build context must come last, got %v", rebuild)
	}

	// A project Dockerfile built as written is passed by path, so Docker applies its
	// <name>.dockerignore.
	byPath := strings.Join(customImageBuildArgs("r", "b", "/ctx", "/ctx/Dockerfile", "h", false), " ")
	if !strings.Contains(byPath, "-f /ctx/Dockerfile") || strings.Contains(byPath, "-f -") {
		t.Errorf("expected the Dockerfile to be passed by path: %v", byPath)
	}
}

// fakeLayerDocker replaces the Docker calls ensureCustomImage makes, recording them in order.
type fakeLayerDocker struct {
	calls   []string
	pulled  bool
	built   bool
	buildDF string // the -f argument: a path, or "" for stdin
	hash    string
	content []byte
}

func installFakeLayerDocker(t *testing.T) *fakeLayerDocker {
	t.Helper()
	f := &fakeLayerDocker{}
	oldPull, oldField, oldBuild := layerPullImage, layerImageField, layerBuildImage
	t.Cleanup(func() { layerPullImage, layerImageField, layerBuildImage = oldPull, oldField, oldBuild })

	layerPullImage = func(image string) error {
		f.calls = append(f.calls, "pull "+image)
		f.pulled = true
		return nil
	}
	layerImageField = func(image, _ string) (string, error) {
		f.calls = append(f.calls, "inspect "+image)
		if IsCustomImageRef(image) {
			return "", errors.New("no such image")
		}
		// The pull is what moves the local base to its newer ID.
		if f.pulled {
			return "sha256:new", nil
		}
		return "sha256:old", nil
	}
	layerBuildImage = func(_, base, contextDir, dockerfilePath string, dockerfile []byte, hash string) error {
		f.calls = append(f.calls, "build")
		f.built = true
		f.buildDF = dockerfilePath
		f.hash = hash
		f.content = dockerfile
		return nil
	}
	return f
}

// setUpLayerProject writes a project with .orobox.yaml and docker/Dockerfile and points viper at
// it. It returns the Dockerfile's absolute path.
func setUpLayerProject(t *testing.T, image map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	dockerfile := filepath.Join(dir, "docker", "Dockerfile")
	content := "ARG " + config.DockerfileBaseImageArg + "\nFROM ${" + config.DockerfileBaseImageArg + "}\nRUN true\n"
	if err := os.WriteFile(dockerfile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(filepath.Join(dir, ".orobox.yaml"))
	viper.Set("type", "project")
	viper.Set("oro_version", "6.1")
	viper.Set("image.dockerfile", "docker/Dockerfile")
	for k, v := range image {
		viper.Set("image."+k, v)
	}
	return dockerfile
}

// A Dockerfile-only project builds from its file by path: that is how Docker finds the
// project's <name>.dockerignore, which a Dockerfile on stdin loses.
func TestEnsureCustomImageDockerfileOnlyBuildsByPath(t *testing.T) {
	dockerfile := setUpLayerProject(t, nil)
	f := installFakeLayerDocker(t)

	if err := ensureCustomImage(); err != nil {
		t.Fatal(err)
	}
	if !f.built || f.buildDF != dockerfile {
		t.Errorf("expected a build from %s, got built=%v -f %q", dockerfile, f.built, f.buildDF)
	}
}

// With image.* lines appended, the rendered Dockerfile exists in no file and goes on stdin.
func TestEnsureCustomImageAppendedLinesBuildFromStdin(t *testing.T) {
	setUpLayerProject(t, map[string]any{"apk": []string{"git"}})
	f := installFakeLayerDocker(t)

	if err := ensureCustomImage(); err != nil {
		t.Fatal(err)
	}
	if !f.built || f.buildDF != "" {
		t.Errorf("expected a build from stdin, got built=%v -f %q", f.built, f.buildDF)
	}
	if !strings.Contains(string(f.content), "RUN apk add --no-cache git") {
		t.Errorf("the rendered layer was not passed to the build:\n%s", f.content)
	}
}

// --rebuild refreshes the base before hashing, so the label describes the base the build used;
// pulling during the build instead would label the layer with the old base's ID, and the next
// command would rebuild it again.
func TestEnsureCustomImageRebuildPullsBeforeHashing(t *testing.T) {
	dockerfile := setUpLayerProject(t, nil)
	f := installFakeLayerDocker(t)
	SetForceCustomImageRebuild(true)
	t.Cleanup(func() { SetForceCustomImageRebuild(false) })

	if err := ensureCustomImage(); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) == 0 || !strings.HasPrefix(f.calls[0], "pull ") {
		t.Fatalf("expected the base to be pulled first, calls: %v", f.calls)
	}
	content, err := os.ReadFile(dockerfile)
	if err != nil {
		t.Fatal(err)
	}
	want, err := customLayerHash(filepath.Dir(dockerfile), "sha256:new", content)
	if err != nil {
		t.Fatal(err)
	}
	if f.hash != want {
		t.Errorf("hash = %s, want the hash over the pulled base %s", f.hash, want)
	}
}

func TestEnsureCustomImageWithoutRebuildDoesNotPullAPresentBase(t *testing.T) {
	setUpLayerProject(t, nil)
	f := installFakeLayerDocker(t)

	if err := ensureCustomImage(); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "pull ") {
			t.Errorf("a present base must not be pulled without --rebuild, calls: %v", f.calls)
		}
	}
}

// The pipeline busts the layer cache under --no-cache by declaring a build argument right after
// each FROM, before every RUN of its stage. The line must land after the FROMs Docker
// really reads, not after a FROM inside a heredoc or in the middle of a continuation.
func TestInsertAfterEveryFromSkipsHeredocsAndContinuations(t *testing.T) {
	src := "ARG OROBOX_BASE_IMAGE\n" +
		"FROM golang:1 AS tool\n" +
		"RUN <<EOF\nFROM users;\nEOF\n" +
		"FROM --platform=$BUILDPLATFORM \\\n  ${OROBOX_BASE_IMAGE}\n" +
		"RUN echo final\n"
	got := string(InsertAfterEveryFrom([]byte(src), "ARG OROBOX_CACHE_BUST"))
	want := "ARG OROBOX_BASE_IMAGE\n" +
		"FROM golang:1 AS tool\n" +
		"ARG OROBOX_CACHE_BUST\n" +
		"RUN <<EOF\nFROM users;\nEOF\n" +
		"FROM --platform=$BUILDPLATFORM \\\n  ${OROBOX_BASE_IMAGE}\n" +
		"ARG OROBOX_CACHE_BUST\n" +
		"RUN echo final\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// `<<EOF` inside quotes or shell arithmetic is not a heredoc to Docker; treating it as one would
// swallow the following FROM and judge the wrong stage final.
func TestFromImagesIgnoresHeredocMarkersThatAreNotWords(t *testing.T) {
	for _, run := range []string{`RUN echo "<<EOF"`, `RUN echo '<<EOF'`, `RUN echo $((1<<FOO))`} {
		src := "FROM alpine AS a\n" + run + "\nFROM ${OROBOX_BASE_IMAGE}\n"
		if err := CheckExtendsBaseImage("Dockerfile", []byte(src)); err != nil {
			t.Errorf("%s: %v", run, err)
		}
	}
}

// --no-cache must rebuild every stage, as `docker build --no-cache` does locally: an ARG is
// scoped to its stage, so it is declared after each FROM.
func TestInsertAfterEveryFrom(t *testing.T) {
	src := "ARG OROBOX_BASE_IMAGE\nFROM golang:1 AS tool\nRUN go install x@latest\nFROM ${OROBOX_BASE_IMAGE}\nRUN echo final\n"
	got := string(InsertAfterEveryFrom([]byte(src), "ARG OROBOX_CACHE_BUST"))
	want := "ARG OROBOX_BASE_IMAGE\nFROM golang:1 AS tool\nARG OROBOX_CACHE_BUST\nRUN go install x@latest\nFROM ${OROBOX_BASE_IMAGE}\nARG OROBOX_CACHE_BUST\nRUN echo final\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// BuildKit heredoc forms the FROM parser must skip: a backslash-quoted delimiter, a hyphenated
// one, an fd prefix, and a heredoc under ONBUILD.
func TestFromImagesSkipsEveryHeredocForm(t *testing.T) {
	for _, body := range []string{
		"RUN python3 <<\\EOF\nfrom os import path\nEOF\n",
		"RUN cat <<END-X\nFROM nope\nEND-X\n",
		"RUN cat 3<<EOF\nfrom x import y\nEOF\n",
		"ONBUILD RUN <<EOF\nFROM nope\nEOF\n",
	} {
		src := "ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}\n" + body
		if err := CheckExtendsBaseImage("Dockerfile", []byte(src)); err != nil {
			t.Errorf("%q: %v", body, err)
		}
	}
}
