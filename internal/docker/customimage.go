package docker

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/utils"
	"github.com/spf13/viper"
)

// customImageRepository is the repository the per-project layer is tagged under. It is
// deliberately not `algoritmadev/orobox`: PullAllLocalOrobotImages pulls every local tag in
// that repository, and this one exists only on this machine.
const customImageRepository = "orobox-custom"

// customImageHashLabel records what the layer was built from. The tag rendered into the
// compose files has to be stable — they are written before Docker is ever consulted — so the
// tag alone cannot say whether the layer is current. The label can: it holds a digest of the
// whole build context and of the base image the layer sits on, so a changed Dockerfile, a
// changed file the Dockerfile copies, and a base image updated by `orobox self-update` all
// invalidate it.
const customImageHashLabel = "dev.orobox.custom-layer"

// customImageOnce memoizes the check for the lifetime of the process. Every command that
// starts or execs into a container asks for the image, and the answer cannot change while a
// single command runs.
var (
	customImageOnce sync.Once
	customImageErr  error
)

// forceCustomImageRebuild makes the next build ignore the Docker layer cache. `orobox up
// --rebuild` sets it: a `RUN apk add` with no pinned version is a cache hit forever, so
// picking up a new upstream package needs an explicit request.
var forceCustomImageRebuild bool

// SetForceCustomImageRebuild requests a cache-less rebuild of the project's image layer.
func SetForceCustomImageRebuild(force bool) {
	forceCustomImageRebuild = force
}

// BaseImageRef returns the published image tag for an Oro version and install type. This is
// what the stack runs unless the project extends it with its own Dockerfile.
func BaseImageRef(oroVersion, imageSuffix string) string {
	return fmt.Sprintf("algoritmadev/orobox:%s-%s-latest", oroVersion, imageSuffix)
}

// CustomImageRef returns the tag of the locally built layer for this project. The project name
// is part of it so two checkouts on the same machine never share a layer, and the Oro version
// and install type are part of it so switching either does not reuse the wrong base.
func CustomImageRef(oroVersion, imageSuffix string) string {
	return fmt.Sprintf("%s/%s:%s-%s", customImageRepository, sanitizeImageName(config.GetProjectName()), oroVersion, imageSuffix)
}

// IsCustomImageRef reports whether an image reference is a locally built Orobox layer. Such a
// reference exists in no registry, so the pull paths have to leave it alone.
func IsCustomImageRef(image string) bool {
	return strings.HasPrefix(image, customImageRepository+"/")
}

// sanitizeImageName turns a project directory name into a valid Docker repository path
// component: lowercase alphanumerics separated by single dashes.
func sanitizeImageName(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	sanitized := strings.Trim(b.String(), "-")
	if sanitized == "" {
		return "project"
	}
	return sanitized
}

// composeNeedsAppImage reports whether a `docker compose` invocation is one that needs the
// application image to exist. The list is an allow-list rather than a skip-list: a subcommand
// nobody thought of here costs an image that is built one step later, while a skip-list that
// missed one would run the stack on a stale layer.
func composeNeedsAppImage(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "up", "run", "exec", "start", "create":
		return true
	default:
		return false
	}
}

// ProjectImageRefs returns the published image this project's stack is based on and the image
// its services actually run. They are the same reference unless a custom `dockerfile` puts a
// locally built layer in between.
func ProjectImageRefs() (base, app string) {
	oroVersion := viper.GetString("oro_version")
	installType, err := config.InstallTypeFor(viper.GetString("type"))
	if err != nil {
		// Fall back to bundle semantics on an unknown/unset type; OroConfig.Validate reports it.
		installType, _ = config.InstallTypeFor(config.InstallTypeBundle)
	}

	base = BaseImageRef(oroVersion, installType.ImageSuffix())
	if config.GetDockerfile() == "" {
		return base, base
	}
	return base, CustomImageRef(oroVersion, installType.ImageSuffix())
}

// EnsureCustomImage builds the project's image layer if `dockerfile` asks for one and the
// existing layer is not current. It is a no-op — and costs nothing beyond stat'ing the build
// context and one image inspect — when no Dockerfile is configured or the layer is up to date.
func EnsureCustomImage() error {
	customImageOnce.Do(func() { customImageErr = ensureCustomImage() })
	return customImageErr
}

func ensureCustomImage() error {
	dockerfile := config.GetDockerfilePath()
	if dockerfile == "" {
		return nil
	}

	content, err := os.ReadFile(dockerfile)
	if err != nil {
		return fmt.Errorf("could not read the 'dockerfile' configured in .orobox.yaml (%s): %w", dockerfile, err)
	}
	if err := checkExtendsBaseImage(config.GetDockerfile(), content); err != nil {
		return err
	}

	base, ref := ProjectImageRefs()

	// The base image ID goes into the hash, so it has to be resolvable. On a first run nothing
	// has pulled it yet: `docker build` would pull it itself, but then the hash would be
	// computed from an image the build did not use.
	baseID, err := imageField(base, "{{.Id}}")
	if err != nil {
		if pullErr := pullImage(base); pullErr != nil {
			return fmt.Errorf("could not pull %s to build the project image layer on: %w", base, pullErr)
		}
		baseID, err = imageField(base, "{{.Id}}")
		if err != nil {
			return fmt.Errorf("could not inspect %s after pulling it: %w", base, err)
		}
	}

	contextDir := filepath.Dir(dockerfile)
	want, err := customLayerHash(contextDir, baseID)
	if err != nil {
		return err
	}

	if !forceCustomImageRebuild {
		got, err := imageField(ref, fmt.Sprintf("{{index .Config.Labels %q}}", customImageHashLabel))
		if err == nil && got == want {
			return nil
		}
	}

	return buildCustomImage(ref, base, contextDir, dockerfile, want)
}

// fromInstruction matches a Dockerfile `FROM` line and captures the image it names.
var fromInstruction = regexp.MustCompile(`(?im)^\s*FROM\s+(\S+)`)

// checkExtendsBaseImage refuses a Dockerfile whose final stage does not build on the published
// Orobox image. Earlier stages are free to use anything — compiling a tool against a plain
// Alpine and copying the result over is exactly what multi-stage builds are for — but the stage
// that produces the runtime image has to be the Orobox one, or `oro_version` would decide
// nothing and the stack would fail in ways that point nowhere near this config key.
func checkExtendsBaseImage(configured string, content []byte) error {
	matches := fromInstruction.FindAllStringSubmatch(string(content), -1)
	if len(matches) == 0 {
		return fmt.Errorf("%s contains no FROM instruction", configured)
	}

	final := matches[len(matches)-1][1]
	if final == "$"+config.DockerfileBaseImageArg || final == "${"+config.DockerfileBaseImageArg+"}" {
		return nil
	}

	return fmt.Errorf(
		"the final stage of %s must be `FROM ${%s}` (found %q). Orobox passes the published image "+
			"for the configured oro_version in that build argument, so declare it above the "+
			"instruction:\n\n    ARG %s\n    FROM ${%s}\n",
		configured, config.DockerfileBaseImageArg, final, config.DockerfileBaseImageArg, config.DockerfileBaseImageArg)
}

// customLayerHash digests everything the layer's content depends on: the base image it sits on
// and every file in the build context, by path, size and modification time. Contents are not
// read — a touched file that Docker's own cache then finds unchanged costs one cached build,
// while reading a large context on every command would cost far more.
func customLayerHash(contextDir, baseID string) (string, error) {
	type entry struct {
		path string
		size int64
		mod  int64
	}

	var entries []entry
	err := filepath.WalkDir(contextDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(contextDir, p)
		if err != nil {
			return err
		}
		entries = append(entries, entry{filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano()})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("could not read the image layer build context %s: %w", contextDir, err)
	}

	// WalkDir is already lexical, but the hash must not depend on that guarantee.
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })

	sum := sha256.New()
	sum.Write([]byte(baseID))
	for _, e := range entries {
		sum.Write([]byte("\n" + e.path + "\x00" + strconv.FormatInt(e.size, 10) + "\x00" + strconv.FormatInt(e.mod, 10)))
	}
	return hex.EncodeToString(sum.Sum(nil))[:16], nil
}

func buildCustomImage(ref, base, contextDir, dockerfile, hash string) error {
	args := []string{
		"build",
		"--build-arg", config.DockerfileBaseImageArg + "=" + base,
		"--label", customImageHashLabel + "=" + hash,
		"-t", ref,
		"-f", dockerfile,
	}
	if forceCustomImageRebuild {
		args = append(args, "--no-cache", "--pull")
	}
	args = append(args, contextDir)

	debug := viper.GetBool("debug")
	message := fmt.Sprintf("Building the project image from %s...", config.GetDockerfile())
	if !debug {
		utils.StartLoader(message)
		defer utils.StopLoader()
	} else {
		utils.PrintInfo(message)
	}

	cmd := exec.Command("docker", args...)
	PrintDebugCommand("docker", args)
	if debug {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("could not build %s from %s: %w", ref, config.GetDockerfile(), err)
		}
		return nil
	}

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		utils.StopLoader()
		fmt.Print(output.String())
		return fmt.Errorf("could not build %s from %s: %w", ref, config.GetDockerfile(), err)
	}
	return nil
}

// imageField inspects one Go-template field of a local image. It fails when the image is not
// present locally, which is how the callers tell "missing" from "out of date".
func imageField(image, format string) (string, error) {
	args := []string{"image", "inspect", "-f", format, image}
	cmd := exec.Command("docker", args...)
	PrintDebugCommand("docker", args)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func pullImage(image string) error {
	debug := viper.GetBool("debug")
	if !debug {
		utils.StartLoader(fmt.Sprintf("Pulling %s...", image))
		defer utils.StopLoader()
	}

	args := []string{"pull", image}
	cmd := exec.Command("docker", args...)
	PrintDebugCommand("docker", args)
	if debug {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}
