//go:build e2e

package e2e

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Box owns one isolated environment for one matrix case.
type Box struct {
	t   *testing.T
	c   Case
	dir string
	bin string
	// logDir collects one file per orobox invocation plus the generated compose configuration,
	// so a failed CI run leaves an artifact behind instead of only scrollback. Empty disables
	// the capture, which is what a unit test wants.
	logDir string
	step   int
}

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
	buildDir  string
)

// binaryPath resolves OROBOX_BIN or builds the binary once for the whole test process.
func binaryPath(t *testing.T) string {
	if p, needBuild := ResolveBinary(os.Getenv); !needBuild {
		return p
	}
	buildOnce.Do(func() {
		// The binary is built once but used by every matrix case, so it must outlive the
		// subtest that happens to build it first. t.TempDir() cannot hold it: Go removes a
		// subtest's temp dir when that subtest ends, so every later case exec'd a deleted
		// file and failed instantly with exit -1 and no output. Use a process-scoped dir
		// that TestMain removes after the whole run instead.
		dir, err := os.MkdirTemp("", "orobox-e2e-bin")
		if err != nil {
			buildErr = errors.New("could not create build dir: " + err.Error())
			return
		}
		buildDir = dir
		out := filepath.Join(dir, "orobox")
		cmd := exec.Command("go", "build", "-o", out, ".")
		// Build from the module root (dir containing go.mod).
		cmd.Dir = repoRoot(t)
		if b, err := cmd.CombinedOutput(); err != nil {
			buildErr = errors.New("go build orobox failed: " + string(b))
			return
		}
		builtBin = out
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return builtBin
}

// cleanupBuiltBinary removes the process-scoped directory holding the binary built by
// binaryPath. Called from TestMain, once the whole suite is done with it.
func cleanupBuiltBinary() {
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
		buildDir = ""
	}
}

// repoRoot walks up from the working directory to the module root (dir containing go.mod).
func repoRoot(t *testing.T) string {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test working directory")
		}
		dir = parent
	}
}

// fixtureFor returns the fixture file path for the case's install type.
func fixtureFor(c Case) string {
	return filepath.Join("fixtures", string(c.Type)+".orobox.yaml")
}

// seedCheckout copies the install type's checkout fixture into dir.
func seedCheckout(t *testing.T, c Case, dir string) {
	t.Helper()

	source := checkoutFixtureDir(c)
	if source == "" {
		return
	}
	if err := os.CopyFS(dir, os.DirFS(source)); err != nil {
		t.Fatalf("seed the %s checkout from %s: %v", c.Type, source, err)
	}
}

// NewBox creates an isolated workdir, writes the rendered .orobox.yaml, resolves the binary,
// and registers teardown.
func NewBox(t *testing.T, c Case) *Box {
	t.Helper()
	// Orobox derives the docker compose project name from filepath.Base(cwd)
	// (config.GetProjectName). t.TempDir() basenames are just "001", "002", ... which
	// collide across cases and never match our ProjectName()-based cleanup. Nest a
	// workdir named exactly c.ProjectName() so orobox's project name is unique and
	// matches the -p target used by pre-clean and teardown.
	dir := filepath.Join(t.TempDir(), c.ProjectName())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	seedCheckout(t, c, dir)

	raw, err := os.ReadFile(fixtureFor(c))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rendered, err := RenderConfig(string(raw), c)
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orobox.yaml"), []byte(rendered), 0o644); err != nil {
		t.Fatalf("write .orobox.yaml: %v", err)
	}

	b := &Box{t: t, c: c, dir: dir, bin: binaryPath(t)}
	b.logDir = filepath.Join(ResolveLogDir(os.Getenv), c.ProjectName())
	if err := os.MkdirAll(b.logDir, 0o755); err != nil {
		// Losing the artifact must not fail the case: the suite still reports through the
		// job log, which is what it did before the directory existed at all.
		t.Logf("could not create the log directory %s: %v", b.logDir, err)
		b.logDir = ""
	}
	// No pre-clean: init must start the install from whatever state it manages itself,
	// so the suite exercises the real installation (including init's own volume handling).
	// Teardown still removes volumes afterwards for test hygiene.
	t.Cleanup(b.teardown)
	return b
}

// Dir returns the case working directory.
func (b *Box) Dir() string { return b.dir }

func (b *Box) exec(ctx context.Context, args ...string) RunResult {
	cmd := exec.CommandContext(ctx, b.bin, args...)
	cmd.Dir = b.dir
	cmd.Env = os.Environ() // forwards COMPOSER_AUTH / GITHUB_TOKEN

	// orobox shells out to `docker compose`, and CommandContext only signals the process it
	// started. A follow-mode command such as `logs --nginx` therefore leaves
	// `docker compose logs -f` alive after the deadline kills orobox, and that orphan still
	// holds the inherited output pipes — so cmd.Run() blocks forever waiting for EOF, with
	// the orphan streaming into the captured output the whole time. Run the child in its own
	// process group so the entire tree can be signalled, and cap how long Run() waits on the
	// pipes in case anything still holds them.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid signals the whole process group.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 10 * time.Second
	var stdout, stderr strings.Builder
	// With E2E_STREAM set, tee the subprocess output live to the terminal so long
	// steps (composer install, oro:install) show progress instead of running silent.
	if os.Getenv("E2E_STREAM") != "" {
		b.t.Logf("+ orobox %s", strings.Join(args, " "))
		cmd.Stdout = io.MultiWriter(&stdout, os.Stderr)
		cmd.Stderr = io.MultiWriter(&stderr, os.Stderr)
	} else {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}
	err := cmd.Run()
	res := RunResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		if ee := new(exec.ExitError); errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
		}
	}
	b.writeStepLog(args, res)
	return res
}

// writeStepLog records one invocation. Best-effort throughout: an artifact that cannot be
// written is worth a note, never a failed case.
func (b *Box) writeStepLog(args []string, res RunResult) {
	if b.logDir == "" {
		return
	}
	b.step++
	if err := WriteStepLog(b.logDir, b.step, args, res); err != nil {
		b.t.Logf("could not write the log for step %d: %v", b.step, err)
	}
}

// stepTimeout bounds a single orobox invocation. Without it a stalled step (composer
// install, oro:install) consumes the whole `go test -timeout`, and since the captured
// output is only printed when the step returns, the run shows nothing at all about where it
// stopped. E2E_STEP_TIMEOUT (any time.ParseDuration value, "0" to disable) turns the stall
// into a per-step failure that reports what the step had printed so far.
func stepTimeout() time.Duration {
	raw := os.Getenv("E2E_STEP_TIMEOUT")
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

// execBounded runs orobox under the E2E_STEP_TIMEOUT deadline, or unbounded when none is set.
func (b *Box) execBounded(args ...string) RunResult {
	d := stepTimeout()
	if d == 0 {
		return b.exec(context.Background(), args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return b.exec(ctx, args...)
}

// Run executes orobox and fails the test on a nonzero exit or a printed error marker
// (hard gate).
func (b *Box) Run(args ...string) RunResult {
	b.t.Helper()
	res := b.execBounded(args...)
	if failed(res) {
		b.t.Fatalf("orobox %s failed (exit %d)\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), res.ExitCode, res.Stdout, res.Stderr)
	}
	return res
}

// TryRun executes orobox and returns the result without failing the test (best-effort).
func (b *Box) TryRun(args ...string) RunResult {
	b.t.Helper()
	return b.execBounded(args...)
}

// RunTimeout executes orobox under a deadline; a deadline kill is not a failure.
func (b *Box) RunTimeout(d time.Duration, args ...string) RunResult {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return b.exec(ctx, args...)
}

// AssertHTTP200 polls url until it returns 200 or the timeout elapses.
func (b *Box) AssertHTTP200(url string) {
	b.t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	var last string
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			last = resp.Status
		} else {
			last = err.Error()
		}
		time.Sleep(5 * time.Second)
	}
	b.t.Fatalf("no HTTP 200 from %s (last: %s)", url, last)
}

// teardownTimeout bounds the `orobox down` run in teardown: cleanup must not be able to
// hold the whole suite open the way an unbounded step can.
const teardownTimeout = 10 * time.Minute

func (b *Box) teardown() {
	// The generated configuration is copied first: it is the part of the case that answers
	// "what did orobox actually render", and it lives in the workdir Go removes once every
	// cleanup has run.
	b.captureGeneratedConfig()

	// Best-effort: never fail the test in teardown.
	ctx, cancel := context.WithTimeout(context.Background(), teardownTimeout)
	defer cancel()
	_ = b.exec(ctx, "down")
	b.dockerDownVolumes()
}

// captureGeneratedConfig copies this case's generated configuration into the log directory.
func (b *Box) captureGeneratedConfig() {
	if b.logDir == "" {
		return
	}
	if err := CaptureGeneratedConfig(b.dir, b.logDir); err != nil {
		b.t.Logf("could not capture the generated configuration: %v", err)
	}
	if err := CaptureRawReports(b.dir, b.logDir); err != nil {
		b.t.Logf("could not capture the raw reports: %v", err)
	}
}

// dockerDownVolumes removes this case's containers AND named volumes during teardown so
// runs do not leak Docker state. Best-effort.
//
// It first tries `docker compose down -v` (works once init has generated a compose file
// in the workdir), then removes any leftover volumes by the compose project label to catch
// anything the compose file no longer references.
func (b *Box) dockerDownVolumes() {
	proj := b.c.ProjectName()

	down := exec.Command("docker", "compose", "-p", proj, "down", "-v", "--remove-orphans")
	down.Dir = b.dir
	_ = down.Run()

	label := "label=com.docker.compose.project=" + proj
	out, err := exec.Command("docker", "volume", "ls", "-q", "--filter", label).Output()
	if err != nil {
		return
	}
	for _, vol := range strings.Fields(string(out)) {
		_ = exec.Command("docker", "volume", "rm", "-f", vol).Run()
	}
}
