package docker

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
// generated Dockerfile *and* of the base image it was built on, so both a changed
// `system_packages` list and a base image updated by `orobox self-update` invalidate it.
const customImageHashLabel = "dev.orobox.custom-layer"

// customImageOnce memoizes the check for the lifetime of the process. Every command that
// starts or execs into a container asks for the image, and the answer cannot change while a
// single command runs.
var (
	customImageOnce sync.Once
	customImageErr  error
)

// BaseImageRef returns the published image tag for an Oro version and install type. This is
// what the stack runs unless the project asks for extra system packages.
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

// customImageContextDir is the build context for the layer. It is a directory of its own and
// not the internal directory, whose `.env` files and certificates have no business being
// uploaded to the Docker daemon on every build.
func customImageContextDir() string {
	return filepath.Join(config.GetInternalDir(), "custom")
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
// its services actually run. They are the same reference unless `system_packages` puts a
// locally built layer in between.
func ProjectImageRefs() (base, app string) {
	oroVersion := viper.GetString("oro_version")
	installType, err := config.InstallTypeFor(viper.GetString("type"))
	if err != nil {
		// Fall back to bundle semantics on an unknown/unset type; OroConfig.Validate reports it.
		installType, _ = config.InstallTypeFor(config.InstallTypeBundle)
	}

	base = BaseImageRef(oroVersion, installType.ImageSuffix())
	if len(config.GetSystemPackages()) == 0 {
		return base, base
	}
	return base, CustomImageRef(oroVersion, installType.ImageSuffix())
}

// EnsureCustomImage builds the project's layer if `system_packages` asks for one and the
// existing layer is not current. It is a no-op — and costs nothing beyond a single image
// inspect — when the list is empty or the layer is already up to date.
func EnsureCustomImage() error {
	customImageOnce.Do(func() { customImageErr = ensureCustomImage() })
	return customImageErr
}

func ensureCustomImage() error {
	if len(config.GetSystemPackages()) == 0 {
		return nil
	}

	dockerfile := filepath.Join(customImageContextDir(), "Dockerfile")
	content, err := os.ReadFile(dockerfile)
	if err != nil {
		return fmt.Errorf("could not read the generated %s: %w", dockerfile, err)
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

	want := customLayerHash(content, baseID)
	if got, err := imageField(ref, fmt.Sprintf("{{index .Config.Labels %q}}", customImageHashLabel)); err == nil && got == want {
		return nil
	}

	return buildCustomImage(ref, want)
}

// customLayerHash digests everything the layer's content depends on: the Dockerfile the
// package list was rendered into, and the base image it sits on.
func customLayerHash(dockerfile []byte, baseID string) string {
	sum := sha256.New()
	sum.Write(dockerfile)
	sum.Write([]byte("\n"))
	sum.Write([]byte(baseID))
	return hex.EncodeToString(sum.Sum(nil))[:16]
}

func buildCustomImage(ref, hash string) error {
	context := customImageContextDir()
	args := []string{
		"build",
		"--label", customImageHashLabel + "=" + hash,
		"-t", ref,
		"-f", filepath.Join(context, "Dockerfile"),
		context,
	}

	debug := viper.GetBool("debug")
	message := "Building the project image layer (system_packages)..."
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
			return fmt.Errorf("could not build %s: %w", ref, err)
		}
		return nil
	}

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		utils.StopLoader()
		fmt.Print(output.String())
		return fmt.Errorf("could not build %s: %w", ref, err)
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
