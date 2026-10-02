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
	"github.com/algoritma-dev/orobox/internal/output"
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
	return fmt.Sprintf("%s:%s-%s", CustomImageRepository(config.GetProjectName()), oroVersion, imageSuffix)
}

// CustomImageRepository is the repository a project's locally built layers are tagged under, for
// a project directory named projectName. The e2e suite uses it to remove the layers it built.
func CustomImageRepository(projectName string) string {
	return customImageRepository + "/" + sanitizeImageName(projectName)
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

// composeNeedsAppImage reports whether a `docker compose` invocation is one that creates
// containers from the application image, and so needs it to exist and be current. `exec` is
// not one: it runs in a container that already exists, on the image that container was created
// from, so building a layer there would only delay qa, test and xdebug with a build they never
// use. The list is an allow-list rather than a skip-list: a subcommand nobody thought of here
// costs an image that is built one step later, while a skip-list that missed one would run the
// stack on a stale layer.
func composeNeedsAppImage(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "up", "run", "start", "create":
		return true
	default:
		return false
	}
}

// ProjectImageRefs returns the published image this project's stack is based on and the image
// its services actually run. They are the same reference unless the project asks for a custom
// layer — an `image.dockerfile` or any other `image.*` key — which puts a locally built image
// in between.
func ProjectImageRefs() (base, app string) {
	oroVersion := viper.GetString("oro_version")
	installType, err := config.InstallTypeFor(viper.GetString("type"))
	if err != nil {
		// Fall back to bundle semantics on an unknown/unset type; OroConfig.Validate reports it.
		installType, _ = config.InstallTypeFor(config.InstallTypeBundle)
	}

	base = BaseImageRef(oroVersion, installType.ImageSuffix())
	if !config.HasCustomLayer() {
		return base, base
	}
	return base, CustomImageRef(oroVersion, installType.ImageSuffix())
}

// layerSource names what the layer is built from, for messages and the compose comment: the
// project Dockerfile, the declarative `image.*` keys, or both.
func layerSource() string {
	img := config.GetImageConfig()
	declarative := len(img.Apk)+len(img.PhpExtensions)+len(img.Npm)+len(img.Run) > 0

	switch {
	case img.Dockerfile != "" && declarative:
		return img.Dockerfile + " + image keys"
	case img.Dockerfile != "":
		return img.Dockerfile
	default:
		return "image keys"
	}
}

// layerContextDir is the build context of a layer that has no project Dockerfile. Such a layer
// has nothing to COPY, so the context only has to exist; it is an empty directory of our own
// rather than the project root so Docker never tars up the whole checkout for nothing.
func layerContextDir() (string, error) {
	dir := filepath.Join(config.GetInternalDir(), "layer-context")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("could not create the image layer build context %s: %w", dir, err)
	}
	return dir, nil
}

// EnsureCustomImage builds the project's image layer if the project asks for one and the
// existing layer is not current. It is a no-op — and costs nothing beyond stat'ing the build
// context and one image inspect — when no layer is configured or the layer is up to date.
func EnsureCustomImage() error {
	customImageOnce.Do(func() { customImageErr = ensureCustomImage() })
	return customImageErr
}

func ensureCustomImage() error {
	if !config.HasCustomLayer() {
		return nil
	}

	// The project Dockerfile is optional: `image.*` keys alone render a complete one.
	var projectDockerfile []byte
	contextDir := ""
	dockerfile := config.GetDockerfilePath()
	if dockerfile != "" {
		content, err := os.ReadFile(dockerfile)
		if err != nil {
			return fmt.Errorf("could not read the 'image.dockerfile' configured in .orobox.yaml (%s): %w", dockerfile, err)
		}
		if err := CheckExtendsBaseImage(config.GetDockerfile(), content); err != nil {
			return err
		}
		projectDockerfile = content
		contextDir = filepath.Dir(dockerfile)
	} else {
		dir, err := layerContextDir()
		if err != nil {
			return err
		}
		contextDir = dir
	}

	img := config.GetImageConfig()
	rendered := RenderLayerDockerfile(projectDockerfile, img)

	// A Dockerfile-only layer is the project file byte for byte, so it is built from that file
	// by path: Docker then honours the project's <name>.dockerignore, which it ignores for a
	// Dockerfile read from stdin. Only a rendered layer, which exists in no file, goes on stdin.
	buildFrom := ""
	if dockerfile != "" && len(layerRunLines(img)) == 0 {
		buildFrom = dockerfile
	}

	base, ref := ProjectImageRefs()

	// --rebuild refreshes the base first, so the hash below is computed from the image the
	// build then uses. Pulling during the build (`docker build --pull`) would label the layer
	// with the previous base's ID and the next command would find it stale and rebuild again.
	if forceCustomImageRebuild {
		if err := layerPullImage(base); err != nil {
			return fmt.Errorf("could not pull %s to rebuild the project image layer on: %w", base, err)
		}
	}

	// The base image ID goes into the hash, so it has to be resolvable. On a first run nothing
	// has pulled it yet: `docker build` would pull it itself, but then the hash would be
	// computed from an image the build did not use.
	baseID, err := layerImageField(base, "{{.Id}}")
	if err != nil {
		if pullErr := layerPullImage(base); pullErr != nil {
			return fmt.Errorf("could not pull %s to build the project image layer on: %w", base, pullErr)
		}
		baseID, err = layerImageField(base, "{{.Id}}")
		if err != nil {
			return fmt.Errorf("could not inspect %s after pulling it: %w", base, err)
		}
	}

	want, err := customLayerHash(contextDir, baseID, rendered)
	if err != nil {
		return err
	}

	if !forceCustomImageRebuild {
		got, err := layerImageField(ref, fmt.Sprintf("{{index .Config.Labels %q}}", customImageHashLabel))
		if err == nil && got == want {
			return nil
		}
	}

	return layerBuildImage(ref, base, contextDir, buildFrom, rendered, want)
}

// The Docker calls ensureCustomImage makes, as variables so tests can follow the order of pull,
// inspect and build without a Docker daemon.
var (
	layerPullImage  = pullImage
	layerImageField = imageField
	layerBuildImage = buildCustomImage
)

// heredocMarker matches a Dockerfile heredoc opener (`<<EOF`, `<<-EOF`, `<<"EOF"`) and captures
// the dash and the delimiter word.
var heredocMarker = regexp.MustCompile(`<<(-?)["']?([A-Za-z_][A-Za-z0-9_]*)["']?`)

// startsHeredocWord reports whether the `<<` at index at begins a word outside any quotes.
func startsHeredocWord(line string, at int) bool {
	if at > 0 {
		if prev := line[at-1]; prev != ' ' && prev != '\t' {
			return false
		}
	}
	var quote byte
	for i := 0; i < at; i++ {
		switch c := line[i]; {
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
		case quote == '"' && c == '\\':
			i++
		}
	}
	return quote == 0
}

// heredocOpeners are the instructions that accept heredocs.
var heredocOpeners = map[string]bool{"RUN": true, "COPY": true, "ADD": true}

// fromImages returns the image of every FROM instruction, in order. It reads the Dockerfile the
// way Docker splits it into instructions, so a line that merely starts with FROM is not one:
// continuation lines (after a line ending in `\`), comments, and heredoc bodies are skipped, and
// FROM's own flags (`--platform=…`) are skipped to reach the image.
func fromImages(content []byte) []string {
	var images []string
	for _, f := range fromInstructions(content) {
		images = append(images, f.image)
	}
	return images
}

// fromInstruction is one FROM of a Dockerfile: the image it names and the index of the last
// line it spans (a FROM continued with `\` ends on a later line).
type fromInstruction struct {
	image   string
	endLine int
}

// InsertAfterEveryFrom returns content with line inserted right after every FROM instruction.
// A build argument is scoped to its stage, so busting the cache of a multi-stage build means
// declaring it in each one — as `docker build --no-cache` rebuilds every stage.
func InsertAfterEveryFrom(content []byte, line string) []byte {
	froms := fromInstructions(content)
	if len(froms) == 0 {
		return content
	}
	lines := strings.Split(string(content), "\n")
	out := make([]string, 0, len(lines)+len(froms))
	next := 0
	for i, l := range lines {
		out = append(out, l)
		if next < len(froms) && froms[next].endLine == i {
			out = append(out, line)
			next++
		}
	}
	return []byte(strings.Join(out, "\n"))
}

// fromInstructions parses content the way fromImages documents.
func fromInstructions(content []byte) []fromInstruction {
	type heredoc struct {
		word      string
		stripTabs bool
	}
	var (
		froms       []fromInstruction
		pending     []heredoc // heredocs opened by the current instruction, read in order
		continued   bool      // the previous line ended in `\`
		instruction string    // keyword of the instruction being read
	)

	for i, line := range strings.Split(string(content), "\n") {
		line = strings.TrimRight(line, "\r")

		if len(pending) > 0 {
			body := line
			if pending[0].stripTabs {
				body = strings.TrimLeft(body, "\t")
			}
			if body == pending[0].word {
				pending = pending[1:]
			}
			continue
		}

		trimmed := strings.TrimSpace(line)
		// A comment or blank line inside a continuation does not end it.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if !continued {
			fields := strings.Fields(trimmed)
			instruction = strings.ToUpper(fields[0])
			if instruction == "FROM" {
				froms = append(froms, fromInstruction{endLine: i})
			}
		}
		if instruction == "FROM" && len(froms) > 0 {
			// The image is the first token that is not a flag, possibly on a continuation line.
			last := &froms[len(froms)-1]
			last.endLine = i
			if last.image == "" {
				fields := strings.Fields(strings.TrimSuffix(trimmed, "\\"))
				if !continued && len(fields) > 0 {
					fields = fields[1:]
				}
				for _, f := range fields {
					if !strings.HasPrefix(f, "--") {
						last.image = f
						break
					}
				}
			}
		}

		if heredocOpeners[instruction] {
			for _, m := range heredocMarker.FindAllStringSubmatchIndex(line, -1) {
				// BuildKit only opens a heredoc on a word that starts with `<<`: not a `<<<`
				// here-string, not inside quotes (`echo "<<EOF"`), not mid-word (`$((1<<FOO))`).
				if !startsHeredocWord(line, m[0]) {
					continue
				}
				pending = append(pending, heredoc{word: line[m[4]:m[5]], stripTabs: m[3] > m[2]})
			}
		}

		continued = strings.HasSuffix(trimmed, "\\")
	}
	return froms
}

// CheckExtendsBaseImage refuses a Dockerfile whose final stage does not build on the published
// Orobox image. Earlier stages are free to use anything — compiling a tool against a plain
// Alpine and copying the result over is exactly what multi-stage builds are for — but the stage
// that produces the runtime image has to be the Orobox one, or `oro_version` would decide
// nothing and the stack would fail in ways that point nowhere near this config key.
func CheckExtendsBaseImage(configured string, content []byte) error {
	images := fromImages(content)
	if len(images) == 0 {
		return fmt.Errorf("%s contains no FROM instruction", configured)
	}

	final := images[len(images)-1]
	if final == "$"+config.DockerfileBaseImageArg || final == "${"+config.DockerfileBaseImageArg+"}" {
		return nil
	}

	return fmt.Errorf(
		"the final stage of %s must be `FROM ${%s}` (found %q). Orobox passes the published image "+
			"for the configured oro_version in that build argument, so declare it above the "+
			"instruction:\n\n    ARG %s\n    FROM ${%s}\n",
		configured, config.DockerfileBaseImageArg, final, config.DockerfileBaseImageArg, config.DockerfileBaseImageArg)
}

// customLayerHash digests everything the layer's content depends on: the base image it sits on,
// the rendered Dockerfile, and every file in the build context, by path, size and modification
// time. Contents of context files are not read — a touched file that Docker's own cache then
// finds unchanged costs one cached build, while reading a large context on every command would
// cost far more. The Dockerfile text, by contrast, is hashed in full: with only `image.*` keys
// it exists nowhere on disk, so the context alone could not tell two layers apart.
func customLayerHash(contextDir, baseID string, dockerfile []byte) (string, error) {
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
	sum.Write([]byte("\nDockerfile\x00"))
	sum.Write(dockerfile)
	return hex.EncodeToString(sum.Sum(nil))[:16], nil
}

// customImageBuildArgs builds the `docker build` argument list. dockerfilePath is the project
// Dockerfile to build from by path, or "" to read the rendered layer from stdin (`-f -`), which
// may exist in no file at all. Extracted from buildCustomImage so the flags can be asserted
// without running Docker.
//
// noCache adds only --no-cache: a forced rebuild pulls the base before computing the hash (see
// ensureCustomImage), so --pull here could only build on a base the label does not describe.
func customImageBuildArgs(ref, base, contextDir, dockerfilePath, hash string, noCache bool) []string {
	file := dockerfilePath
	if file == "" {
		file = "-"
	}
	args := []string{
		"build",
		"--build-arg", config.DockerfileBaseImageArg + "=" + base,
		"--label", customImageHashLabel + "=" + hash,
		"-t", ref,
		"-f", file,
	}
	if noCache {
		args = append(args, "--no-cache")
	}
	return append(args, contextDir)
}

func buildCustomImage(ref, base, contextDir, dockerfilePath string, dockerfile []byte, hash string) error {
	args := customImageBuildArgs(ref, base, contextDir, dockerfilePath, hash, forceCustomImageRebuild)

	debug := viper.GetBool("debug")
	source := layerSource()
	message := fmt.Sprintf("Building the project image from %s...", source)
	if !debug {
		utils.StartLoader(message)
		defer utils.StopLoader()
	} else {
		utils.PrintInfo(message)
	}

	cmd := exec.Command("docker", args...)
	if dockerfilePath == "" {
		cmd.Stdin = bytes.NewReader(dockerfile)
	}
	PrintDebugCommand("docker", args)
	if debug {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("could not build %s from %s: %w", ref, source, err)
		}
		return nil
	}

	var buildLog bytes.Buffer
	cmd.Stdout = &buildLog
	cmd.Stderr = &buildLog
	if err := cmd.Run(); err != nil {
		utils.StopLoader()
		// A failed build's own log is the only diagnostic there is, so agent mode keeps it — on
		// stderr, where a caller can tell it apart from the payload.
		if output.Agent() {
			output.Err(buildLog.String())
		} else {
			fmt.Print(buildLog.String())
		}
		return fmt.Errorf("could not build %s from %s: %w", ref, source, err)
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
