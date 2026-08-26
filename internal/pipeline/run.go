package pipeline

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dagger.io/dagger"
	"golang.org/x/sync/errgroup"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/qatools"
)

// artifactContainerDir is where every build step writes its tarballs and where the release
// container finds them. deploy.php reads the same path from OROBOX_DEPLOY_ARTIFACT_DIR.
const artifactContainerDir = "/artifacts"

// Options carries the host-side facts the pipeline cannot derive from the config.
type Options struct {
	// ProjectDir is the host project root; artifacts are exported below it.
	ProjectDir string
	// Debug streams the Dagger engine output instead of hiding it.
	Debug bool
	// SSHAuthSock is the host SSH agent socket. Used for both the git clone and the remote
	// release when set, which is the normal local case.
	SSHAuthSock string
	// SSHPrivateKey is a private key passed as a secret instead of an agent socket. This is
	// the CI case, where no agent exists in the job container.
	SSHPrivateKey string
	// KnownHostsPath is a host file with the remote's public keys. When empty the pipeline
	// scans the stage host and says so.
	KnownHostsPath string
	// GitHTTPToken authenticates an https repository URL, which is how a GitLab job clones
	// without an SSH agent (CI_JOB_TOKEN).
	GitHTTPToken string
	// GitHTTPUser is the username paired with GitHTTPToken. GitLab expects "gitlab-ci-token".
	GitHTTPUser string
	// ReportHostDir is the host directory the raw per-tool reports are exported to. Required when
	// the plan asks for a report, ignored otherwise.
	ReportHostDir string
}

// Result is everything a run produced on the host. The report directories hold the raw per-tool
// files — one JSON per QA tool, one JUnit XML per PHPUnit invocation, plus the status file the
// steps wrote instead of failing. Merging them into the single document GitLab reads is the
// caller's job, because that is where --report-path is known.
type Result struct {
	Artifacts     []string
	QAReportDir   string
	TestReportDir string
}

// deployKeyPath is where a private key passed as a secret is mounted in the release
// container. An ssh config entry points the stage host at it, so no agent is needed in CI.
const deployKeyPath = "/root/.ssh/id_orobox_deploy"

// containerSSHAgentSocket is where the host agent socket is forwarded inside every container
// that talks to an SSH server: composer's git clones and the remote release alike.
const containerSSHAgentSocket = "/tmp/ssh-agent.sock"

type runner struct {
	client *dagger.Client
	plan   *Plan
	opts   Options
	log    *logBuffer
	report *reporter

	// runID only matters when the plan disables caching, where it is what makes the dependency
	// layers differ from the previous run's.
	runID string

	source    *dagger.Directory
	sshSocket *dagger.Socket
	sshKey    *dagger.Secret
}

// Run executes the whole pipeline: build, QA and tests in Dagger, then the remote release
// through Deployer. It returns the host paths of the exported artifacts. QA and test failures
// cancel each other, so nothing reaches the remote host after a violation.
func Run(ctx context.Context, plan *Plan, opts Options) (Result, error) {
	// The engine output is always captured so a failure can quote it; with --debug it is also
	// streamed live.
	var tee io.Writer
	if opts.Debug {
		tee = os.Stderr
	}
	log := newLogBuffer(tee)

	// Without DAGGER_PROGRESS=plain the CLI session buffers its whole progress output and prints
	// it when the session closes, with the commands' own output left out entirely. Plain progress
	// streams it line by line instead, which is what makes both --debug and the heartbeats of a
	// long command show anything at all while it runs.
	client, err := dagger.Connect(ctx,
		dagger.WithLogOutput(log),
		dagger.WithEnvironmentVariable("DAGGER_PROGRESS", "plain"))
	if err != nil {
		return Result{}, fmt.Errorf("could not start the Dagger engine (is the Docker daemon running?): %w", err)
	}
	defer client.Close()

	r := &runner{
		client: client,
		plan:   plan,
		opts:   opts,
		log:    log,
		report: newReporter(os.Stdout, opts.Debug, log),
		runID:  strconv.FormatInt(time.Now().UnixNano(), 10),
	}

	if opts.SSHAuthSock != "" {
		r.sshSocket = client.Host().UnixSocket(opts.SSHAuthSock)
	}
	if opts.SSHPrivateKey != "" {
		r.sshKey = client.SetSecret("orobox-deploy-ssh-key", opts.SSHPrivateKey)
	}

	if err := r.resolveSource(ctx); err != nil {
		return Result{}, err
	}

	// The dependency layers come first and are the only thing that survives between runs. They
	// are built from the composer files alone, so a commit that does not change the lock reuses
	// all three from Dagger's cache.
	lockSource, err := r.lockSource(ctx)
	if err != nil {
		return Result{}, err
	}

	deps, err := r.exec(ctx, r.container(plan.Deps).WithDirectory(config.OroRootDir, lockSource), plan.Deps)
	if err != nil {
		return Result{}, r.describe("installing the dependencies", err)
	}

	// The dev dependencies and the QA tools are installed only when something still consumes them.
	// Both stay nil otherwise, which is safe because the only readers are guarded by the same
	// predicates: qa-tools is built whenever the QA step runs, and deps-dev whenever either does.
	var depsDev, qaTools *dagger.Container
	if plan.NeedsDevDependencies() {
		depsDev, err = r.exec(ctx, deps, plan.DepsDev)
		if err != nil {
			return Result{}, r.describe("installing the development dependencies", err)
		}
	}
	if plan.RunsQA() {
		qaTools, err = r.exec(ctx, depsDev, plan.QaTools)
		if err != nil {
			return Result{}, r.describe("installing the QA tools", err)
		}
	}

	// From here on every step overlays the sources, so all of it is per-commit work — which is
	// exactly why a run that will not release skips it: see Plan.BuildsArtifacts.
	artifacts := map[string]*dagger.File{}
	if plan.BuildsArtifacts() {
		vendor, err := r.exec(ctx, r.on(deps, plan.Vendor), plan.Vendor)
		if err != nil {
			return Result{}, r.describe("building the vendor tree", err)
		}
		artifacts[config.VendorArtifactName] = vendor.File(artifactContainerDir + "/" + config.VendorArtifactName)

		if plan.BuildsAssets() {
			// The assets are built on top of the finished vendor container: they need the
			// production autoloader the vendor step just dumped.
			assets, err := r.exec(ctx, r.on(vendor, *plan.Assets), *plan.Assets)
			if err != nil {
				return Result{}, r.describe("building the assets", err)
			}
			artifacts[config.AssetsArtifactName] = assets.File(artifactContainerDir + "/" + config.AssetsArtifactName)
		}
	}

	// QA and tests are independent consumers of the development dependencies, so they run
	// together and the first failure cancels the other. With both skipped the group is empty and
	// Wait returns at once.
	//
	// The finished containers are kept because in report mode the reports are read back out of
	// them: the steps exit 0 whatever the tools concluded, precisely so this is possible.
	var qaContainer, testContainer *dagger.Container
	group, groupCtx := errgroup.WithContext(ctx)
	if plan.RunsQA() {
		group.Go(func() error {
			ctr, err := r.exec(groupCtx, r.on(qaTools, plan.QA), plan.QA)
			if err != nil {
				return r.describe("QA checks", err)
			}
			qaContainer = ctr
			return nil
		})
	}
	if plan.RunsTests() {
		group.Go(func() error {
			ctr, err := r.exec(groupCtx, r.on(depsDev, plan.Test), plan.Test)
			if err != nil {
				return r.describe("tests", err)
			}
			testContainer = ctr
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return Result{}, err
	}

	result := Result{}
	if plan.Report != qatools.ReportNone {
		if qaContainer != nil {
			dir, err := r.exportReports(ctx, qaContainer, QAReportDir(), "qa")
			if err != nil {
				return result, err
			}
			result.QAReportDir = dir
		}
		if testContainer != nil {
			dir, err := r.exportReports(ctx, testContainer, TestReportDir(), "test")
			if err != nil {
				return result, err
			}
			result.TestReportDir = dir
		}
	}

	// Export before releasing: a CI job can then publish the artifacts even if the remote
	// release fails, and the developer can inspect exactly what was uploaded.
	exported, err := r.exportArtifacts(ctx, artifacts)
	if err != nil {
		return result, err
	}
	result.Artifacts = exported

	// Everything up to here is local; the release is the only part that reaches the stage host, so
	// skipping it leaves the exported artifacts as the run's whole result.
	if !plan.RunsRelease() {
		return result, nil
	}

	release, err := r.releaseContainer(artifacts)
	if err != nil {
		return result, err
	}
	if _, err := r.exec(ctx, release, r.plan.Release); err != nil {
		return result, r.describe("the remote release", err)
	}

	return result, nil
}

// resolveSource puts the application tree on r.source.
//
// It resolves eagerly, before any container needs it. Dagger is lazy, so otherwise a bad ref,
// missing credentials or an unreadable directory would surface as a failure of whichever container
// happened to touch the sources first — and be reported as a QA or test failure.
func (r *runner) resolveSource(ctx context.Context) error {
	switch r.plan.Source.Kind {
	case SourceHost:
		dir := r.plan.Source.Dir
		excludes := HostExcludes(dir)
		r.source = r.client.Host().Directory(dir, dagger.HostDirectoryOpts{Exclude: excludes})
		fmt.Printf("Building the working tree in %s (%d excluded paths)\n", dir, len(excludes))
		// Named, not counted: a count is what made the "stat vendor-bin/qa: no such file or
		// directory" failure unreconstructable. Absent patterns are not an error — see
		// MissingExcludes — so this is a note, printed only when there are any.
		if missing := MissingExcludes(dir, excludes); len(missing) > 0 {
			fmt.Printf("Excluded paths no longer on disk (harmless, listed for diagnosis): %s\n",
				strings.Join(missing, ", "))
		}
	case SourceGit:
		if err := r.resolveGitSource(ctx); err != nil {
			return err
		}
	}

	if subdir := r.plan.sourceSubdir(); subdir != "" {
		// Monorepo: only the application subdirectory becomes the container's /var/www/oro, so
		// every step sees a normal Oro checkout and the sibling projects never reach a release.
		r.source = r.source.Directory(subdir)
	}
	if _, err := r.source.Sync(ctx); err != nil {
		if r.plan.Source.Kind == SourceHost {
			return fmt.Errorf("could not read the project directory %s: %w", r.plan.Source.Dir, err)
		}
		return r.cloneError(err)
	}
	return nil
}

// resolveGitSource is the clone path: everything that was inline in Run before the host directory
// became an option. Unchanged in behaviour.
func (r *runner) resolveGitSource(ctx context.Context) error {
	cloneURL := r.plan.Repository
	gitOpts := dagger.GitOpts{}
	switch {
	case r.sshSocket != nil:
		gitOpts.SSHAuthSocket = r.sshSocket
		// The clone happens inside a container with no ~/.ssh, so without these the host key
		// of the git server cannot be verified and git exits 128.
		gitOpts.SSHKnownHosts = r.knownHosts()
	case r.opts.GitHTTPToken != "":
		// GitLab clones over https with a job token; the username is fixed by GitLab. A token
		// cannot authenticate an SSH URL, so one is converted here — for the clone only, since
		// Deployer still needs the configured value.
		if derived, ok := httpsCloneURL(cloneURL); ok {
			fmt.Printf("No SSH agent available: cloning over https from %s\n", derived)
			cloneURL = derived
		}
		gitOpts.HTTPAuthToken = r.client.SetSecret("orobox-git-token", r.opts.GitHTTPToken)
		if r.opts.GitHTTPUser != "" {
			gitOpts.HTTPAuthUsername = r.opts.GitHTTPUser
		}
	}

	ref := r.client.Git(cloneURL, gitOpts).Ref(r.plan.Ref)
	commit, err := ref.Commit(ctx)
	if err != nil {
		return r.cloneError(err)
	}
	r.source = ref.Tree()
	fmt.Printf("Building %s at %s (%s)\n", r.plan.Ref, commit, r.plan.Repository)
	return nil
}

// sourceDescription names where the sources came from, for an error message. A host directory has
// neither a repository nor a ref, and naming an empty repository "at HEAD" sends the reader looking
// for a clone that never happened.
func (r *runner) sourceDescription() string {
	if r.plan.Source.Kind == SourceHost {
		return r.plan.Source.Dir
	}
	return fmt.Sprintf("%s at %s", r.plan.Repository, r.plan.Ref)
}

// exportReports copies a step's report directory out of its container. The files stay raw and
// per-tool: merging them is the command layer's job, which is where the requested output path is
// known.
func (r *runner) exportReports(ctx context.Context, ctr *dagger.Container, containerDir, name string) (string, error) {
	if r.opts.ReportHostDir == "" {
		return "", fmt.Errorf("the plan asks for a %s report but no host directory was given to export it to", name)
	}

	hostDir := filepath.Join(r.opts.ReportHostDir, name)
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		return "", fmt.Errorf("could not create the report directory %s: %w", hostDir, err)
	}
	if _, err := ctr.Directory(containerDir).Export(ctx, hostDir); err != nil {
		return "", r.describe("exporting the "+name+" report", err)
	}
	return hostDir, nil
}

// ExportedArtifacts returns the host paths the tarballs are written to, for reporting.
func (p *Plan) ExportedArtifacts(projectDir string) []string {
	paths := make([]string, 0, 2)
	for _, name := range p.Artifacts() {
		paths = append(paths, filepath.Join(projectDir, p.ArtifactDir, name))
	}
	return paths
}

func (r *runner) exportArtifacts(ctx context.Context, artifacts map[string]*dagger.File) ([]string, error) {
	// A run that builds none must not even create the directory: an empty artifact directory on a
	// lint job reads as a build that produced nothing rather than one that was never asked to.
	if len(artifacts) == 0 {
		return nil, nil
	}

	dir := filepath.Join(r.opts.ProjectDir, r.plan.ArtifactDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("could not create the artifact directory %s: %w", dir, err)
	}

	var exported []string
	for _, name := range r.plan.Artifacts() {
		path := filepath.Join(dir, name)
		if _, err := artifacts[name].Export(ctx, path); err != nil {
			return nil, r.describe("exporting "+name, err)
		}
		exported = append(exported, path)
	}
	return exported, nil
}

// knownHosts returns the content of the host's known_hosts file, which Dagger needs to verify
// the git server's key inside its own container. An empty result is not fatal: the clone hint
// covers it if the server then refuses.
func (r *runner) knownHosts() string {
	if r.opts.KnownHostsPath == "" {
		return ""
	}
	content, err := os.ReadFile(r.opts.KnownHostsPath)
	if err != nil {
		return ""
	}
	return string(content)
}

// lockSource is the directory the dependency layers are built from: the composer files and the
// few paths an install needs, and nothing else. Keeping the application sources out is what makes
// the layers reusable — Dagger keys an exec on the container state before it, so a directory that
// changed with every commit would make every install a cache miss.
func (r *runner) lockSource(ctx context.Context) (*dagger.Directory, error) {
	manifest, err := r.source.File("composer.json").Contents(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not read composer.json from %s: %w", r.sourceDescription(), err)
	}

	entries, err := r.source.Entries(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not list the repository contents: %w", err)
	}
	present := map[string]bool{}
	directories := map[string]bool{}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry, "/")
		present[name] = true
		if strings.HasSuffix(entry, "/") {
			directories[name] = true
		}
	}
	if !present["composer.lock"] {
		return nil, fmt.Errorf("composer.lock is missing from %s: the pipeline installs dependencies from the lock file and will not resolve them itself",
			r.sourceDescription())
	}

	paths := LockLayerPaths([]byte(manifest))
	// A patches-file declares its patches outside composer.json, so the paths they live in can
	// only be known after reading it. A file that cannot be read is left to composer to report.
	if file := PatchesFile([]byte(manifest)); file != "" && present[strings.SplitN(file, "/", 2)[0]] {
		if contents, err := r.source.File(file).Contents(ctx); err == nil {
			paths = append(paths, PatchFilePaths([]byte(contents))...)
		}
	}

	dir := r.client.Directory()
	mounted := map[string]bool{}
	for _, path := range paths {
		// Only the top-level name can be checked cheaply; a nested path repository is trusted to
		// exist, and composer reports it clearly enough when it does not.
		if top := strings.SplitN(path, "/", 2)[0]; !present[top] {
			continue
		}
		if mounted[path] {
			continue
		}
		mounted[path] = true
		if !strings.Contains(path, "/") && !directories[path] {
			dir = dir.WithFile(path, r.source.File(path))
			continue
		}
		dir = dir.WithDirectory(path, r.source.Directory(path))
	}
	return dir, nil
}

// on builds a step's container on top of an already-resolved layer: the step's own environment
// and service bindings, then the application sources, then its caches. The sources come after the
// layer because the layers deliberately do not contain them, and the caches after the sources
// because a mount inside a directory being written would be shadowed by it.
func (r *runner) on(base *dagger.Container, step Step) *dagger.Container {
	ctr := base
	for _, service := range step.Services {
		ctr = ctr.WithServiceBinding(service.Name, r.service(service))
	}
	for name, value := range step.Env {
		ctr = ctr.WithEnvVariable(name, value)
	}
	if step.Workdir != "" {
		ctr = ctr.WithWorkdir(step.Workdir)
	}
	return r.withCaches(ctr.WithDirectory(config.OroRootDir, r.source), step)
}

// withCaches mounts the step's persistent directories. It runs after the application tree is in
// place, because a mount inside the directory being written would otherwise be shadowed by it.
// Locked sharing serializes concurrent pipelines instead of letting them interleave writes into
// the same Symfony cache.
func (r *runner) withCaches(ctr *dagger.Container, step Step) *dagger.Container {
	for _, cache := range step.Caches {
		ctr = ctr.WithMountedCache(cache.Path, r.client.CacheVolume(cache.Volume),
			dagger.ContainerWithMountedCacheOpts{Sharing: dagger.CacheSharingModeLocked})
	}
	return ctr
}

// container applies everything that is independent of a step's commands: image, caches,
// environment, service bindings and the artifact directory.
func (r *runner) container(step Step) *dagger.Container {
	ctr := r.client.Container().From(r.plan.Image).
		// The published image may declare a non-root user; the pipeline needs to write into
		// the application root and install packages, so it runs as root throughout.
		WithUser("root").
		WithMountedCache("/cache/composer", r.client.CacheVolume("orobox-composer-"+r.plan.OroVersion)).
		WithMountedCache("/cache/js", r.client.CacheVolume("orobox-js-"+r.plan.OroVersion)).
		WithEnvVariable("COMPOSER_CACHE_DIR", "/cache/composer").
		WithEnvVariable("COMPOSER_ALLOW_SUPERUSER", "1").
		WithEnvVariable("npm_config_cache", "/cache/js").
		WithEnvVariable("npm_config_store_dir", "/cache/js/pnpm")

	for name, value := range r.plan.CacheEnv(r.runID) {
		ctr = ctr.WithEnvVariable(name, value)
	}

	// Private Composer repositories: the same auth block the dev environment uses, injected as
	// a secret so tokens never land in a layer's environment.
	if auth := r.composerAuth(); auth != nil {
		ctr = ctr.WithSecretVariable("COMPOSER_AUTH", auth)
	}

	for _, service := range step.Services {
		ctr = ctr.WithServiceBinding(service.Name, r.service(service))
	}

	for name, value := range step.Env {
		ctr = ctr.WithEnvVariable(name, value)
	}

	if step.Workdir != "" {
		ctr = ctr.WithWorkdir(step.Workdir)
	}

	// composer.json may point at private VCS repositories over SSH, so every layer that runs
	// composer needs the same credentials the clone got.
	ctr = r.withComposerSSH(ctr)

	return ctr.WithExec([]string{"mkdir", "-p", artifactContainerDir})
}

// withComposerSSH gives a container the credentials composer needs to clone a private VCS
// repository declared in composer.json. The layer containers have no ~/.ssh at all, so without
// this an SSH repository URL fails on host key verification before authentication is even
// attempted. The agent socket is preferred when one exists; a key passed as a secret is the CI
// case. Nothing here depends on the host's known_hosts, which would make the dependency layers
// differ per developer and lose their cache.
func (r *runner) withComposerSSH(ctr *dagger.Container) *dagger.Container {
	if r.sshSocket == nil && r.sshKey == nil {
		return ctr
	}

	// HOME may be unwritable in the published image, so the host keys are collected in /tmp
	// rather than ~/.ssh; accept-new trusts a server on first sight but still detects a key
	// that changed within the run.
	sshCommand := "ssh -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/tmp/known_hosts"

	switch {
	case r.sshSocket != nil:
		ctr = ctr.WithUnixSocket(containerSSHAgentSocket, r.sshSocket).
			WithEnvVariable("SSH_AUTH_SOCK", containerSSHAgentSocket)
	case r.sshKey != nil:
		ctr = ctr.WithMountedSecret(deployKeyPath, r.sshKey, dagger.ContainerWithMountedSecretOpts{
			Owner: "root",
			Mode:  0o600,
		})
		sshCommand += " -i " + deployKeyPath + " -o IdentitiesOnly=yes"
	}

	return ctr.WithEnvVariable("GIT_SSH_COMMAND", sshCommand)
}

// exec runs the step's commands, one exec per command so the engine can cache them
// individually. Each command is resolved before the next one is appended rather than building
// the whole chain lazily: that is what makes it possible to say which command is running and
// to print its output as it completes, instead of a single opaque wait per step.
func (r *runner) exec(ctx context.Context, ctr *dagger.Container, step Step) (*dagger.Container, error) {
	for _, command := range step.Commands {
		task := r.report.start(step.Name, command)

		// stderr is folded into stdout so the report shows the command's output in the order it
		// was written; composer and the Symfony console send most of theirs to stderr.
		next := ctr.WithExec([]string{"bash", "-o", "pipefail", "-c", "exec 2>&1\n" + command})

		// Stdout resolves the container, so the command has really run once this returns.
		out, err := next.Stdout(ctx)
		if err != nil {
			task.fail()
			return nil, err
		}

		task.ok(out)
		ctr = next
	}
	return ctr, nil
}

func (r *runner) service(service Service) *dagger.Service {
	ctr := r.client.Container().From(service.Image)
	for name, value := range service.Env {
		ctr = ctr.WithEnvVariable(name, value)
	}
	if service.Name == testDBService || service.Name == qaDBService {
		// Oro needs uuid-ossp and pg_trgm; the compose stack installs them through the same
		// initdb script.
		ctr = ctr.WithNewFile("/docker-entrypoint-initdb.d/init-db.sql",
			"CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";\nCREATE EXTENSION IF NOT EXISTS \"pg_trgm\";\n")
	}
	if service.DataCache != "" {
		// A persistent data directory is what lets the QA database be installed once and reused.
		// Postgres is a single writer, so this cache is never shared between concurrent runs.
		ctr = ctr.WithMountedCache(service.DataPath, r.client.CacheVolume(service.DataCache),
			dagger.ContainerWithMountedCacheOpts{Sharing: dagger.CacheSharingModeLocked})
	}
	return ctr.WithExposedPort(service.Port).AsService()
}

// composerAuth returns the COMPOSER_AUTH secret when the project configures one.
func (r *runner) composerAuth() *dagger.Secret {
	if r.plan.ComposerAuth == "" {
		return nil
	}
	return r.client.SetSecret("orobox-composer-auth", r.plan.ComposerAuth)
}

// releaseContainer prepares the Deployer run: artifacts in place, SSH configured, host key
// known. The step's own commands are not appended here; the caller runs them so their progress
// is reported like every other step's.
func (r *runner) releaseContainer(artifacts map[string]*dagger.File) (*dagger.Container, error) {
	if r.sshSocket == nil && r.sshKey == nil {
		return nil, fmt.Errorf("no SSH credentials available: start an agent (SSH_AUTH_SOCK) or set OROBOX_DEPLOY_SSH_KEY")
	}

	ctr := r.container(r.plan.Release).WithDirectory(config.OroRootDir, r.source)

	for name, file := range artifacts {
		ctr = ctr.WithFile(artifactContainerDir+"/"+name, file)
	}

	// Deployer uploads with rsync, which the runtime image does not ship.
	ctr = ctr.WithExec([]string{"apk", "add", "--no-cache", "rsync", "openssh-client"})

	ctr = ctr.WithExec([]string{"mkdir", "-p", "/root/.ssh"})

	if r.sshSocket != nil {
		ctr = ctr.WithUnixSocket(containerSSHAgentSocket, r.sshSocket).
			WithEnvVariable("SSH_AUTH_SOCK", containerSSHAgentSocket)
	}
	if r.sshKey != nil {
		// An ssh config entry beats starting an agent inside the container: the key never
		// leaves the mounted secret and ssh picks it up for this host only.
		ctr = ctr.WithMountedSecret(deployKeyPath, r.sshKey, dagger.ContainerWithMountedSecretOpts{
			Owner: "root",
			Mode:  0o600,
		}).WithNewFile("/root/.ssh/config", fmt.Sprintf(
			"Host %s\n  IdentityFile %s\n  IdentitiesOnly yes\n", r.plan.Stage.Host, deployKeyPath))
	}

	if r.opts.KnownHostsPath != "" {
		ctr = ctr.WithFile("/root/.ssh/known_hosts", r.client.Host().File(r.opts.KnownHostsPath))
	} else {
		// Without a known_hosts file the host key is accepted on first sight. The command layer
		// warns about this before the pipeline starts.
		ctr = ctr.WithExec([]string{"bash", "-c", fmt.Sprintf(
			"ssh-keyscan -p %d %s >> /root/.ssh/known_hosts 2>/dev/null",
			r.plan.Stage.SSHPort(), r.plan.Stage.Host)})
	}

	return ctr, nil
}
