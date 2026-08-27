package pipeline

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/qatools"
)

// releaseStepName is the step that reaches the remote host. It runs alone and last, which is why
// the deploy timings the reporter prints belong to it alone.
const releaseStepName = "release"

// Service is a container the pipeline binds to a step under Name as its hostname.
type Service struct {
	Name  string
	Image string
	Port  int
	Env   map[string]string
	// Args replaces the image's default command, through its entrypoint. Empty leaves the image's
	// own command in place.
	Args []string
	// DataCache makes the service's DataPath survive between pipeline runs, which is what turns
	// the QA database into a one-off install instead of a per-run one.
	DataCache string
	DataPath  string
}

// Cache is a directory a step keeps between pipeline runs. Unlike the package caches it is
// part of the step's state, not just a download accelerator, so it is always mounted with
// exclusive access: two runs sharing a half-written Symfony cache is worse than waiting.
type Cache struct {
	Path   string
	Volume string
}

// Step is one container in the pipeline: an image, a working directory and a list of shell
// lines to run in order. Keeping steps declarative is what makes the planning layer testable
// without a Dagger engine.
type Step struct {
	Name     string
	Workdir  string
	Env      map[string]string
	Commands []string
	Services []Service
	Caches   []Cache
}

// SourceKind selects where the pipeline takes the application sources from.
type SourceKind int

const (
	// SourceGit clones the repository at the plan's ref inside the engine. This is what a local
	// deploy does: the artifacts and the remote checkout then come from the same committed tree,
	// whatever the developer happens to have uncommitted.
	SourceGit SourceKind = iota
	// SourceHost uploads a directory from the host. It is what `orobox qa` and `orobox test` always
	// use — analysing the last pushed commit instead of the tree being edited would be useless —
	// and what a CI deploy uses, where the job already holds the checkout.
	SourceHost
)

// SourceSpec is the resolved answer to "where do the sources come from". Dir is meaningful only
// for SourceHost.
type SourceSpec struct {
	Kind SourceKind
	Dir  string
}

// ContainerReportDir is the directory inside a container that the report files are written to. It
// mirrors artifactContainerDir: a fixed path the steps write to and the runner reads back.
const ContainerReportDir = "/reports"

// QAReportDir is where the QA step writes its per-tool reports and its status file. It is kept
// apart from the test step's directory so exporting one never overwrites the other's status.
func QAReportDir() string { return ContainerReportDir + "/qa" }

// TestReportDir is where the test step writes its JUnit logs and its status file.
func TestReportDir() string { return ContainerReportDir + "/test" }

// Plan is the fully resolved pipeline for one stage. The DAG shape is fixed — vendor feeds
// assets, qa and test; release consumes the artifacts — so the plan only carries the
// contents of each step.
type Plan struct {
	Stage      config.StageConfig
	Repository string
	Ref        string
	// CacheScope and BaseCacheScope are recorded for the deploy summary. The volume names
	// themselves are already resolved into the steps.
	CacheScope     string
	BaseCacheScope string
	Image          string
	OroVersion     string
	ArtifactDir    string // host directory the tarballs are exported to
	// SourceDir is the repository-relative directory holding the application, empty when the
	// repository root is the application. Every step's sources are taken from it, so a monorepo
	// builds only its Oro project.
	SourceDir string
	// ComposerAuth is the JSON COMPOSER_AUTH value for private repositories, injected as a
	// secret so tokens never appear in a container's environment listing.
	ComposerAuth string
	// NoCache makes the run ignore everything it could have reused. It never changes what the
	// pipeline does, only what it reuses.
	NoCache bool

	// SkipQA, SkipTest and SkipRelease each drop one part of the pipeline. Like NoCache they never
	// change what a step does, only whether it runs at all, so a run with none of them set is the
	// only thing a stage's configuration alone describes.
	SkipQA      bool
	SkipTest    bool
	SkipRelease bool

	// Source decides whether the sources are cloned or uploaded from the host. Like the skip flags
	// it never changes what a step does — only where the tree it works on came from.
	Source SourceSpec

	// Suites are the PHPUnit suites to run, empty meaning everything phpunit.xml defines. Filter
	// narrows by test name. Both live on the plan rather than being read from Stage because a plan
	// built for `orobox test` has no stage: what a stage gates its release on and what the
	// developer asked to run are different questions.
	Suites []string
	Filter string

	// PreparesTestDatabase makes the test step install or restore an Oro database before PHPUnit.
	// A deploy sets it from the stage's suites; `orobox test` always sets it, matching what the
	// compose engine requires of `orobox test-init`.
	PreparesTestDatabase bool

	// Report makes the QA and test steps emit machine-readable output into ContainerReportDir.
	Report qatools.Report

	// Deps, DepsDev and QaTools are built from composer.json and composer.lock alone, before the
	// application sources are overlaid. Dagger keys each exec on the container state before it, so
	// they are reused across runs and across refs for as long as the lock does not change. That is
	// the whole caching strategy for the dependency trees: no fingerprint, nothing to invalidate.
	Deps    Step
	DepsDev Step
	QaTools Step

	Vendor  Step
	Assets  *Step // nil when the repository already ships pre-built assets
	QA      Step
	Test    Step
	Release Step
}

// sourceSubdir is the directory to descend into after the sources are resolved, so the container's
// application root holds a plain Oro checkout.
//
// It is SourceDir for a clone, which starts at the repository root, and empty for a host directory,
// which is already the application root: .orobox.yaml lives beside the application, so the path the
// commands upload is the subdirectory itself. Applying it twice would look for b2b/b2b.
func (p *Plan) sourceSubdir() string {
	if p.Source.Kind == SourceHost {
		return ""
	}
	return p.SourceDir
}

// BuildsAssets reports whether the pipeline builds and ships the webpack assets.
func (p *Plan) BuildsAssets() bool {
	return p.Assets != nil
}

// RunsQA reports whether the QA step and the qa-tools layer it needs are part of this run.
func (p *Plan) RunsQA() bool { return !p.SkipQA }

// RunsTests reports whether the test step is part of this run.
func (p *Plan) RunsTests() bool { return !p.SkipTest }

// RunsRelease reports whether the pipeline ends on the remote host.
func (p *Plan) RunsRelease() bool { return !p.SkipRelease }

// NeedsDevDependencies reports whether the deps-dev layer still has a consumer. It feeds the test
// step directly and the QA step through the qa-tools layer built on top of it, so skipping just
// one of the two leaves it needed; only skipping both turns the run into a plain build.
func (p *Plan) NeedsDevDependencies() bool { return p.RunsQA() || p.RunsTests() }

// BuildsArtifacts reports whether the vendor and assets tarballs are produced and exported.
//
// They are the release payload and nothing else reads them, so a run that releases needs them and
// a run that only checks does not. That distinction matters because the artifact steps overlay the
// application sources, which makes their cache key change with every commit: unlike the dependency
// layers they are never reused, so a lint job that builds them pays a full authoritative
// dump-autoload and a tar of the whole vendor tree for a file it then throws away — three times
// per commit across a pipeline of lint, test and deploy.
//
// The exception is the run that skips everything: with no QA, no tests and no release the tarballs
// are the only thing left to produce, so building them is the whole point rather than waste. The
// reasoning is the same as NeedsDevDependencies': skipping every check turns the run into a plain
// build.
func (p *Plan) BuildsArtifacts() bool {
	return p.RunsRelease() || !p.NeedsDevDependencies()
}

// SkippedSteps names the parts of the pipeline this run leaves out, in pipeline order. It is nil
// for a normal run, which is what keeps a normal deploy's summary unchanged.
func (p *Plan) SkippedSteps() []string {
	var skipped []string
	if !p.RunsQA() {
		skipped = append(skipped, "qa")
	}
	if !p.RunsTests() {
		skipped = append(skipped, "test")
	}
	if !p.RunsRelease() {
		skipped = append(skipped, "release")
	}
	return skipped
}

// CacheEnv is the environment that turns caching off. OROBOX_NO_CACHE is read by the fingerprint
// scripts, which then rebuild the QA install and the test database. OROBOX_CACHE_BUST carries a
// per-run value, which is the only way to make Dagger rebuild the dependency layers: they are
// keyed on the container state, and nothing else about them changes between runs.
func (p *Plan) CacheEnv(runID string) map[string]string {
	if !p.NoCache {
		return nil
	}
	return map[string]string{
		"OROBOX_NO_CACHE":   "1",
		"OROBOX_CACHE_BUST": runID,
	}
}

// Artifacts lists the tarball names this plan produces, in upload order. It is empty for a run
// that builds none, which is what keeps the summary and the export loop honest without either of
// them repeating the predicate.
func (p *Plan) Artifacts() []string {
	if !p.BuildsArtifacts() {
		return nil
	}
	artifacts := []string{config.VendorArtifactName}
	if p.BuildsAssets() {
		artifacts = append(artifacts, config.AssetsArtifactName)
	}
	return artifacts
}

// Overrides are the per-invocation values that replace what the stage configuration would
// otherwise decide. They exist for CI, where the commit under test is not the stage's ref and
// the caches belong to a branch rather than to that ref.
type Overrides struct {
	// Ref replaces stage.Ref as the commit the pipeline builds and the release checks out.
	Ref string
	// CacheScope names the cache volume family. Empty means the effective ref, which is what
	// keeps a run without flags identical to one before this existed.
	CacheScope string
	// BaseCacheScope is the family a missing test database dump is seeded from. Empty, or equal
	// to CacheScope, means no seeding.
	BaseCacheScope string
}

// New resolves a stage into a plan with no overrides: everything comes from the configuration.
// repository is passed in rather than read from the config so the caller can fall back to the
// git origin and report that failure with context.
func New(conf *config.OroConfig, stage config.StageConfig, repository string) *Plan {
	return NewWithOverrides(conf, stage, repository, Overrides{})
}

// NewWithOverrides is New with the per-invocation values a CI job needs: a ref that is not the
// stage's, and a cache scope that outlives it.
func NewWithOverrides(conf *config.OroConfig, stage config.StageConfig, repository string, o Overrides) *Plan {
	ref := stage.Ref
	if o.Ref != "" {
		ref = o.Ref
	}
	scope := o.CacheScope
	if scope == "" {
		scope = ref
	}
	baseScope := o.BaseCacheScope
	if baseScope == scope {
		// A base equal to the scope would mount the same volume twice under two paths.
		baseScope = ""
	}

	oroRoot := config.OroRootDir
	versions := config.GetVersionsForOro(conf.OroVersion)
	suffix := volumeSuffix(conf.OroVersion, scope)
	baseSuffix := ""
	if baseScope != "" {
		baseSuffix = volumeSuffix(conf.OroVersion, baseScope)
	}

	p := &Plan{
		Stage:          stage,
		Repository:     repository,
		Ref:            ref,
		CacheScope:     scope,
		BaseCacheScope: baseScope,
		Image:          fmt.Sprintf("algoritmadev/orobox:%s-%s-latest", conf.OroVersion, config.InstallTypeProject),
		OroVersion:     conf.OroVersion,
		ArtifactDir:    filepath.Join(config.DeployArtifactsDir, stage.Name),
		SourceDir:      conf.Deploy.Source(),
		ComposerAuth:   docker.ComposerAuthJSON(conf.Composer.Auth),

		Source:               SourceSpec{Kind: SourceGit},
		Suites:               stage.Suites(),
		PreparesTestDatabase: stage.RunsFunctionalTests(),
	}

	// --no-autoloader is not an optimization: the root package's classmap is built from src/,
	// which this layer does not have, and an authoritative classmap missing the project's own
	// classes makes them unloadable. Every consumer dumps the autoloader after the overlay.
	p.Deps = Step{
		Name:    "deps",
		Workdir: oroRoot,
		Env:     prodEnv(),
		Commands: []string{
			autoloadPlaceholderCommand(),
			"composer install --no-dev --no-interaction --no-progress --no-scripts --no-autoloader",
		},
	}

	// The QA and test steps both need the dev dependencies — PHPUnit and the bundles the test
	// environment registers — so they share one layer instead of installing them twice. The
	// exported vendor.tar.gz is archived from the vendor step, which branches off Deps, so the dev
	// packages never reach a release.
	p.DepsDev = Step{
		Name:    "deps-dev",
		Workdir: oroRoot,
		Env:     prodEnv(),
		Commands: []string{
			"composer install --no-interaction --no-progress --no-scripts --no-autoloader",
		},
	}

	p.QaTools = Step{
		Name:     "qa-tools",
		Workdir:  oroRoot,
		Env:      prodEnv(),
		Commands: qaToolCommands(conf.OroVersion),
	}

	p.Vendor = Step{
		Name:    "vendor",
		Workdir: oroRoot,
		Env:     prodEnv(),
		Commands: []string{
			"composer dump-autoload --no-dev --optimize --classmap-authoritative --no-scripts",
			archiveCommand(config.VendorArtifactName, "vendor"),
		},
	}

	if !preBuiltAssets(conf) {
		// Assets are built here only when the repository does not already carry them. Either
		// way the remote never rebuilds them, so it always runs a plain assets:install.
		p.Assets = &Step{
			Name:    "assets",
			Workdir: oroRoot,
			Env:     prodEnv(),
			Commands: []string{
				"php bin/console oro:assets:install --env=prod --no-interaction",
				archiveCommand(config.AssetsArtifactName, "public/build", "public/js", "public/media/js"),
			},
		}
	}

	p.QA = Step{
		Name:    "qa",
		Workdir: oroRoot,
		// PHPStan's Oro bootstrap boots the kernel in test with debug on and reads the dumped
		// test container, the same environment the functional tests install.
		Env:      qaEnv(versions.Postgres),
		Commands: p.qaCommands(conf.OroVersion),
		Services: qaServices(suffix, versions.Postgres),
		Caches:   qaCaches(suffix),
	}

	p.Test = Step{
		Name:     "test",
		Workdir:  oroRoot,
		Env:      testEnv(versions.Postgres),
		Commands: p.testCommands(versions.Postgres),
		Services: testServices(conf, versions),
		Caches:   testCaches(p.PreparesTestDatabase, suffix, baseSuffix),
	}

	p.Release = Step{
		Name:    releaseStepName,
		Workdir: oroRoot,
		Env:     releaseEnv(stage, ref, repository, p.SourceDir),
		Commands: []string{
			// The clone carries vendor-bin/deploy/composer.json but not its vendor tree, so
			// Deployer itself is installed here rather than uploaded from the developer's disk.
			// This is what makes a CI run work without any pre-existing state.
			"composer install --working-dir=vendor-bin/deploy --no-interaction --no-progress",
			"php vendor-bin/deploy/vendor/bin/dep deploy --file=deploy.php --no-interaction -v",
		},
	}

	return p
}

// ChecksOptions describes a run that checks the code and stops there: no release, no artifacts,
// no stage.
type ChecksOptions struct {
	// ProjectDir is the host directory the sources are uploaded from.
	ProjectDir string
	// CacheScope names the cache volume family; the caller resolves it from the branch.
	// BaseCacheScope seeds a missing test database dump, exactly as it does for a deploy.
	CacheScope     string
	BaseCacheScope string
	// RunQA and RunTest select the step. The commands set exactly one, but both are honoured.
	RunQA   bool
	RunTest bool
	// Suites and Filter narrow the PHPUnit run; empty Suites means everything in phpunit.xml.
	Suites []string
	Filter string
	Report qatools.Report
}

// NewChecks builds a plan for `orobox qa` or `orobox test` on the Dagger engine: the same steps a
// deploy would run, minus everything only a release needs.
//
// The plan carries a zero StageConfig, which is safe because after the suites and the database
// decision moved onto the Plan, the release step is the only reader of Stage — and SkipRelease is
// always set here.
func NewChecks(conf *config.OroConfig, o ChecksOptions) *Plan {
	p := NewWithOverrides(conf, config.StageConfig{}, "", Overrides{
		Ref:            o.CacheScope,
		CacheScope:     o.CacheScope,
		BaseCacheScope: o.BaseCacheScope,
	})

	p.Source = SourceSpec{Kind: SourceHost, Dir: o.ProjectDir}
	p.SkipQA = !o.RunQA
	p.SkipTest = !o.RunTest
	p.SkipRelease = true
	p.Suites = o.Suites
	p.Filter = o.Filter
	p.Report = o.Report
	// `orobox test` prepares the database whatever the suites are: the command's contract on the
	// compose engine is that `orobox test-init` has run, and the two engines must not disagree
	// about what the command does.
	p.PreparesTestDatabase = o.RunTest

	// NewWithOverrides rendered the steps from the deploy defaults, so the two that read the fields
	// set above are rebuilt here.
	versions := config.GetVersionsForOro(conf.OroVersion)
	p.QA.Commands = p.qaCommands(conf.OroVersion)
	p.Test.Commands = p.testCommands(versions.Postgres)
	p.Test.Caches = testCaches(p.PreparesTestDatabase,
		volumeSuffix(conf.OroVersion, p.CacheScope), baseVolumeSuffix(conf.OroVersion, p.BaseCacheScope))

	return p
}

// baseVolumeSuffix is volumeSuffix for an optional base scope: an empty scope has no volume.
func baseVolumeSuffix(oroVersion, baseScope string) string {
	if baseScope == "" {
		return ""
	}
	return volumeSuffix(oroVersion, baseScope)
}

// preBuiltAssets reports whether the repository already ships the built assets. It tolerates a
// missing deploy section, which a project that only uses `orobox qa` and `orobox test` has no
// reason to write.
func preBuiltAssets(conf *config.OroConfig) bool {
	return conf.Deploy != nil && conf.Deploy.PreBuiltAssetsEnabled
}

// autoloadPlaceholderCommand creates empty stand-ins for the root package's classmap and files
// autoload entries.
//
// The dependency layers deliberately have no src/, and Composer's classmap generator treats a
// missing classmap entry as fatal: an Oro application maps src/AppKernel.php, so every command
// that dumps an autoloader — `require` does so whatever --no-scripts says — dies with `Could not
// scan for classes inside "src/AppKernel.php"`. A psr-4 prefix pointing at a missing directory is
// only skipped, so nothing else has to be faked.
//
// The stand-ins are replaced by the real files when the sources are overlaid, and the layers dump
// no autoloader of their own, so nothing downstream ever sees them.
func autoloadPlaceholderCommand() string {
	return `# Creating placeholders for the root package's autoload entries
php -r '$manifest = json_decode(@file_get_contents("composer.json"), true) ?: [];
foreach (["autoload", "autoload-dev"] as $section) {
    foreach (["classmap", "files"] as $kind) {
        foreach ($manifest[$section][$kind] ?? [] as $entry) {
            if ($entry === "" || file_exists($entry)) { continue; }
            if (substr($entry, -4) === ".php") {
                @mkdir(dirname($entry), 0777, true);
                file_put_contents($entry, "<?php\n");
            } else {
                @mkdir($entry, 0777, true);
            }
            echo "Placeholder for the autoload entry ", $entry, "\n";
        }
    }
}'`
}

// prodEnv is the environment every build command runs in: the artifacts must be the ones a
// production host would get.
func prodEnv() map[string]string {
	return map[string]string{"ORO_ENV": "prod", "APP_ENV": "prod", "COMPOSER_NO_INTERACTION": "1"}
}

// archiveCommand tars the given paths, rooted at the application root, so extraction on the
// remote is a plain `tar -xzf` inside the release directory. Missing paths are skipped so a
// project without, say, public/media/js still produces a valid archive.
func archiveCommand(name string, paths ...string) string {
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		quoted = append(quoted, fmt.Sprintf("$([ -e %s ] && echo %s)", path, path))
	}
	return fmt.Sprintf("tar -czf /artifacts/%s %s", name, strings.Join(quoted, " "))
}

// Fixed identifiers for the QA database. It is deliberately not the test one: the two steps
// run at the same time, and an identical service definition would be deduplicated by the
// engine into a single container the two installs would then fight over.
const (
	qaDBService = "db-qa"
	qaDBName    = "oro_db_qa"
)

// qaPGDataPath is a subdirectory of the mounted volume rather than the mount point itself,
// because initdb refuses a data directory that is not empty and a fresh volume is not.
const qaPGDataPath = "/var/lib/postgresql/data/pgdata"

// qaEnv runs the QA step as a test, debug-enabled application wired to the QA database. PHPStan
// needs all three: phpstan-symfony reads the dumped test debug container, phpstan-doctrine boots
// the kernel for an ObjectManager, and warming that cache queries Oro's config tables.
//
// The environment is test rather than dev because oro:install refuses to run non-interactively
// in any other environment without --user-email/--user-firstname/--user-lastname/--user-password;
// test supplies its own fixture admin. It also means PHPStan and the functional tests need only
// one kind of installed database.
func qaEnv(postgresVersion string) map[string]string {
	return map[string]string{
		"ORO_ENV":          "test",
		"APP_ENV":          "test",
		"ORO_DEBUG":        "1",
		"APP_DEBUG":        "1",
		"ORO_DB_HOST":      qaDBService,
		"ORO_DB_PORT":      "5432",
		"ORO_DB_NAME":      qaDBName,
		"ORO_DB_USER":      testDBUser,
		"ORO_DB_PASSWORD":  testDBPassword,
		"ORO_DB_DSN":       dsn(qaDBService, qaDBName, postgresVersion),
		"ORO_APP_PROTOCOL": "http",
		"ORO_APP_DOMAIN":   "localhost",
		"ORO_APP_URL":      "http://localhost/",
	}
}

// volumeSuffix scopes a mutable cache volume to one Oro version and one git ref. Without the ref,
// deploying two stages in turn makes each run invalidate the fingerprint the other just wrote, and
// neither cache is ever reused. Two stages that share a ref share the volumes, which is correct:
// what the cache holds is a function of the code at that ref.
//
// The download caches deliberately keep an Oro-version-only name: they are content-addressed
// package stores, so sharing them across refs is a pure gain.
func volumeSuffix(oroVersion, scope string) string {
	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, scope)
	return oroVersion + "-" + sanitized
}

// postgresArgs raises max_locks_per_transaction for the pipeline's databases, the way the compose
// stack already does for a developer's.
//
// A lock is taken per object, held to the end of the transaction, and the default 64 per
// transaction is what the shared lock table is sized from. `DROP SCHEMA public CASCADE` — how both
// database resets empty a reused volume — is one transaction over every table, index, sequence and
// constraint in an installed Oro, which is a five-figure object count on the larger versions. Past
// the default it fails with `SQLSTATE[53200]: Out of memory: 7 ERROR: out of shared memory`, which
// is what the Oro 7.0 QA step reported when its seed fell back to a reinstall.
func postgresArgs() []string {
	return []string{"postgres", "-c", "max_locks_per_transaction=1024"}
}

// qaServices is the QA step's own Postgres, with its data directory on a named cache volume.
// That is what makes the Oro install reusable: the next run finds the schema already there and
// only has to check the fingerprint.
func qaServices(suffix, postgresVersion string) []Service {
	return []Service{{
		Name:      qaDBService,
		Image:     "postgres:" + postgresVersion,
		Port:      5432,
		Args:      postgresArgs(),
		DataCache: "orobox-qa-db-" + suffix,
		DataPath:  "/var/lib/postgresql/data",
		Env: map[string]string{
			"POSTGRES_DB":       qaDBName,
			"POSTGRES_USER":     testDBUser,
			"POSTGRES_PASSWORD": testDBPassword,
			"PGDATA":            qaPGDataPath,
		},
	}}
}

// qaCaches keeps the warmed test cache — the dumped container PHPStan reads — between runs. The
// fingerprint that decides whether it is still valid lives inside it, so the cache and the
// database it was built against are invalidated together.
//
// The mount is var/cache, not the test cache inside it, so that oro:install's closing cache:clear
// can remove var/cache/test: see qatools.CacheVolumeDir.
func qaCaches(suffix string) []Cache {
	return []Cache{{
		Path:   qatools.CacheVolumeDir(),
		Volume: "orobox-qa-cache-" + suffix,
	}}
}

// qaWarmupCommand makes the cached install usable, and rebuilds it when it no longer matches
// the code being analysed. PHPStan cannot run without a dumped test debug container, and warming
// one queries Oro's own tables, so an installed database is part of the requirement.
//
// The fingerprint covers composer.lock and every migration file: those are what decide the
// schema and the compiled container. Anything else can change freely without paying for another
// install, which is the whole point of keeping the cache.
//
// A stamp that merely disagrees means an install is present and only the pending migrations are
// missing, so it is updated rather than rebuilt. That path falls back to a full install if the
// update fails, which is not the same judgement the test step makes: there the database was
// restored from a dump, so a failure is the branch's migrations being wrong, while here it comes
// from a Postgres data volume that can have drifted from the var/cache volume holding the stamp.
//
// The full install itself has a cheaper form: when the runtime image carries a dump of an already
// installed Oro, the database is restored from it and reconciled with oro:platform:update instead
// of being built by oro:install. It is the same substitution the test step's third rung makes, and
// it matters for the same reason — the QA database lives on a cache volume that a CI runner
// creates empty on every run, so "rebuild" is what CI does every time rather than rarely.
//
// Every way the seed can fail — no dump for this Postgres major, no client package, a dump that
// does not restore, an update that does not reconcile — falls back to the install that was there
// before, after emptying the schema the failed attempt may have half-written. A seed that does not
// work costs time; it must not cost a run.
func qaWarmupCommand(postgresVersion string) string {
	major := postgresMajor(postgresVersion)
	return fmt.Sprintf(`# Preparing the QA cache: Oro install and warmed test cache
set -e
stamp=%[1]s/.orobox-qa-fingerprint
%[7]s
seed_install() {
  if [ -n "$OROBOX_NO_CACHE" ]; then return 1; fi
  if [ ! -f %[8]s ]; then return 1; fi
  if ! command -v psql >/dev/null 2>&1; then
    apk add --no-cache postgresql%[9]s-client >/dev/null 2>&1 || apk add --no-cache postgresql-client >/dev/null 2>&1 || return 1
  fi
  echo 'Seeding the QA database from the dump the runtime image carries.'
  %[6]s
  export PGPASSWORD=%[12]s
  gunzip -c %[8]s | psql -h %[10]s -U %[11]s -d %[13]s -v ON_ERROR_STOP=1 >/dev/null || return 1
  # The reset above runs through bin/console, so it left a warmed var/cache/test describing the
  # empty database it ran against — the dumped container, the generated extend classes, the
  # compiled Doctrine metadata. The dump then replaced that database wholesale, and
  # oro:platform:update boots on top of whatever the cache holds rather than rebuilding it: on
  # Oro 7.0 that surfaced as "Circular reference detected for service
  # doctrine.orm.default_entity_manager", a container error with nothing to do with the database
  # it was handed. The test step drops the same directory for the same reason; see rebuild() in
  # testDatabaseCommand.
  rm -rf %[4]s
  php bin/console oro:platform:update --force --env=test --timeout=0 --skip-download-translations --skip-translations || return 1
}
full_install() {
  rm -rf %[4]s "$stamp"
  %[5]s
  if ! seed_install; then
    %[6]s
    php bin/console oro:install --env=test --no-interaction --drop-database --skip-translations
  fi
  php bin/console cache:warmup --env=test
  if [ -n "$fingerprint" ]; then
    printf '%%s' "$fingerprint" > "$stamp"
  fi
}
if [ -n "$fingerprint" ] && [ -z "$OROBOX_NO_CACHE" ] && [ "$(cat "$stamp" 2>/dev/null)" = "$fingerprint" ] && [ -f %[2]s ] && [ -d %[3]s ]; then
  echo 'Reusing the cached Oro install and warmed test cache.'
  exit 0
fi
if [ -n "$fingerprint" ] && [ -z "$OROBOX_NO_CACHE" ] && [ -s "$stamp" ]; then
  echo 'The cached install is stale: applying the pending migrations instead of reinstalling.'
  rm -rf %[4]s
  %[5]s
  if php bin/console oro:platform:update --force --env=test --timeout=0 && php bin/console cache:warmup --env=test; then
    printf '%%s' "$fingerprint" > "$stamp"
    exit 0
  fi
  echo 'The incremental update failed; falling back to a full install.'
fi
echo 'Rebuilding the QA cache: installing Oro and warming the test cache. Later runs reuse this.'
full_install`,
		qatools.CacheVolumeDir(), qatools.ContainerXMLPath(qatools.EnvTest), qatools.SymfonyConfigDir(qatools.EnvTest),
		qatools.CacheDir(qatools.EnvTest), oroWritableDirsCommand(), qaDatabaseResetCommand(),
		sourceFingerprintCommand("the QA cache will be rebuilt and not cached"),
		config.SeedDumpPath(major), major,
		qaDBService, testDBUser, testDBPassword, qaDBName)
}

// sourceFingerprintCommand computes the fingerprint the QA and test caches are keyed on —
// composer.lock plus every migration file — into the shell variable "fingerprint", and leaves it
// empty when it cannot.
//
// The files are read one per `cat` rather than as one argument list, which a project with many
// bundles can overflow, and the result is caught instead of being left to `set -e`. The earlier
// form put the whole computation in a command substitution with stderr sent to /dev/null, so any
// failure to read a file took the step down with no output whatsoever: the run reported a bare
// "exit code: 1" for a step that had not printed a single line. Now the reason reaches the step
// output and the step continues without its cache, which costs time and nothing else.
//
// consequence describes, for that message, what proceeding without a fingerprint means.
func sourceFingerprintCommand(consequence string) string {
	return fmt.Sprintf(`fingerprint_sources() {
  cat composer.lock
  find src -path '*Migrations*' -type f 2>/dev/null | LC_ALL=C sort | while IFS= read -r file; do
    cat "$file"
  done
}
fingerprint=$(fingerprint_sources | md5sum | cut -c1-32) || fingerprint=''
if [ -z "$fingerprint" ]; then
  echo 'Could not fingerprint composer.lock and the migration files: %s.'
fi`, consequence)
}

// qaDatabaseResetCommand empties the persistent QA database before oro:install runs against it.
//
// oro:install --drop-database drops what Doctrine knows about, which is the entity tables. The
// tables nothing maps — oro_session and oro_migrations — survive it, and on the reused data
// volume that is enough to take the next install down: the session table is created again and
// PostgreSQL answers with `relation "oro_session" already exists`. A surviving oro_migrations is
// worse, because the migrations already recorded there are then never replayed.
//
// Dropping the schema removes every table whatever created it. It also removes the two
// extensions Oro requires, which the volume received from the service's initdb script and which
// only runs when the cluster is initialised, so they are recreated here rather than assumed.
// The statements are separate console calls: DBAL sends a multi-statement string as one
// prepared statement, which PostgreSQL rejects.
func qaDatabaseResetCommand() string {
	statements := []string{
		"DROP SCHEMA IF EXISTS public CASCADE",
		"CREATE SCHEMA public",
		`CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\"`,
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
	}
	commands := make([]string, 0, len(statements)+1)
	commands = append(commands, "echo 'Emptying the QA database: the reused volume can hold tables the installer does not drop.'")
	for _, statement := range statements {
		commands = append(commands, fmt.Sprintf(`php bin/console doctrine:query:sql --env=test "%s"`, statement))
	}
	return strings.Join(commands, "\n")
}

// oroWritableDirsCommand creates the directories Oro's requirements check demands before
// oro:install runs it. They never come from the clone — var/ and public/uploads are gitignored,
// and nothing but the installer would create them — so the check fails with "Change the
// permissions of the var/data directory so that the web server can write into it" and the install
// stops before it starts. The pipeline runs as root, so existing directories need no chmod.
func oroWritableDirsCommand() string {
	return "mkdir -p var/data var/cache var/logs public/uploads public/media/cache"
}

// qaToolCommands populates the isolated QA tool set. It runs in the source-independent layer:
// the pipeline works on a clean clone, so vendor-bin/qa is never installed there, and nothing
// here reads the application sources.
func qaToolCommands(oroVersion string) []string {
	plan := qatools.NewInstallPlan(oroVersion)
	qaDir := config.QaToolsDir

	var commands []string

	if plan.NeedsComposerTools {
		commands = append(commands,
			fmt.Sprintf(`mkdir -p %s && [ -f %s/composer.json ] || printf '{"name":"orobox/qa-tools"}' > %s/composer.json`, qaDir, qaDir, qaDir),
			fmt.Sprintf("composer -d %s config --no-plugins allow-plugins.phpstan/extension-installer true", qaDir),
			fmt.Sprintf("composer -d %s config --no-plugins allow-plugins.algoritma/php-coding-standards true", qaDir),
			"composer config --no-plugins allow-plugins.bamarni/composer-bin-plugin true",
			"composer require --dev --no-scripts --no-interaction bamarni/composer-bin-plugin",
			"composer remove --dev --no-scripts --no-interaction friendsofphp/php-cs-fixer || true",
			// After the application's tree is final: the patch writes the versions actually
			// installed there into the QA manifest's `replace`.
			qatools.SharedVendorScript(),
			qatools.ComposerInstallCommand(plan.ComposerPackages),
		)
		// The base configurations belong to the layer that installed the packages: they are the
		// standard's own files, they never depend on the sources, and generating them per commit
		// would pay for them on every run.
		if base := qatools.BaseConfigScript(); base != "" {
			commands = append(commands, base)
		}
	}

	if plan.NeedsJSTools {
		commands = append(commands, qatools.JSInstallCommand(plan))
	}

	return commands
}

// qaCommands is the per-commit half of the QA stage: it runs after the sources are overlaid on
// the qa-tools layer, and runs every enabled tool in check-only mode. The configuration scripts
// are here rather than in the layer because they must not overwrite a configuration committed in
// the repository, and the overlay is what puts that configuration in place.
func (p *Plan) qaCommands(oroVersion string) []string {
	plan := qatools.NewInstallPlan(oroVersion)
	oroRoot := config.OroRootDir

	// The layers installed the packages without an autoloader; the classmap needs src/, which
	// only exists now.
	commands := []string{"composer dump-autoload --optimize --no-scripts"}

	if plan.NeedsComposerTools && plan.NeedsPhpstan {
		commands = append(commands, qatools.PhpstanConfigScript(qatools.EnvTest))
	}
	if plan.NeedsComposerTools && plan.NeedsTwigCS {
		commands = append(commands, qatools.TwigConfigScript())
	}
	if plan.NeedsPhpstan {
		commands = append(commands, qaWarmupCommand(config.GetVersionsForOro(oroVersion).Postgres))
	}

	var enabled []qatools.Tool
	for _, tool := range qatools.Tools(qatools.ToolsOptions{
		SourceRoot:  oroRoot,
		AnalyzePath: config.QaAnalyzePathFor(config.InstallTypeProject),
		Env:         qatools.EnvTest,
		Mode:        qatools.ModeCheck,
		Report:      p.Report,
		ReportDir:   QAReportDir(),
		OroVersion:  oroVersion,
	}) {
		if config.IsQaToolEnabled(tool.Name) {
			enabled = append(enabled, tool)
		}
	}
	if len(enabled) > 0 {
		if p.Report == qatools.ReportNone {
			commands = append(commands, qatools.Script(enabled))
		} else {
			commands = append(commands, qatools.ReportScript(enabled, QAReportDir()))
		}
	}

	return commands
}

// testEnv points the application at the Dagger Postgres service. The variable names and
// values mirror templates/docker/.env.test, so the pipeline's test run sees the same
// environment as `orobox test`.
func testEnv(postgresVersion string) map[string]string {
	return map[string]string{
		"ORO_ENV":          "test",
		"ORO_DB_HOST":      testDBService,
		"ORO_DB_PORT":      "5432",
		"ORO_DB_NAME":      testDBName,
		"ORO_DB_USER":      testDBUser,
		"ORO_DB_PASSWORD":  testDBPassword,
		"ORO_DB_DSN":       dsn(testDBService, testDBName, postgresVersion),
		"ORO_APP_PROTOCOL": "http",
		"ORO_APP_DOMAIN":   "localhost",
		"ORO_APP_URL":      "http://localhost/",
	}
}

// Fixed identifiers for the pipeline's own database service, matching .env.test so a project
// that hardcodes them in a test bootstrap keeps working.
const (
	testDBService  = "db-test"
	testDBName     = "oro_db_test"
	testDBUser     = "oro_db_user"
	testDBPassword = "oro_db_pass"
)

// The test database cache holds a dump, not a data directory. Functional tests write to the
// database, so a reused cluster would carry one run's leftovers into the next; restoring a dump
// costs tens of seconds and gives every run a byte-identical starting point.
const (
	testDBCacheDir  = "/cache/test-db"
	testDBDumpFile  = testDBCacheDir + "/dump.sql.gz"
	testDBStampFile = testDBCacheDir + "/.orobox-test-fingerprint"

	// The base scope's volume, mounted only when --base-cache-scope names a different family.
	// The ladder reads from here and never writes: Dagger has no read-only cache mount, so the
	// rule lives in the script instead of in the mount. Its fingerprint is never consulted —
	// oro:platform:update reconciles whatever the base dump turns out to hold.
	testDBBaseCacheDir = "/cache/test-db-base"
	testDBBaseDumpFile = testDBBaseCacheDir + "/dump.sql.gz"
)

// testDBSeedDumpFile is the dump the runtime image was built with: an OroCommerce installed for
// this image's Oro version and nothing else. It is the last rung before a full install, and the
// only one that is there on a runner with no history at all, which is what every GitHub-hosted
// job is: the Dagger engine and its cache volumes are created and destroyed with the job, so both
// cache rungs above miss on every single CI run.
func testDBSeedDumpFile(postgresVersion string) string {
	return config.SeedDumpPath(postgresMajor(postgresVersion))
}

// testCaches mounts the database dump for a plan that installs Oro. A run that never touches a
// schema mounts nothing and pays nothing. When a base scope is given, its dump is mounted
// alongside so a scope with no dump of its own can start from it instead of from a full install.
func testCaches(preparesDatabase bool, suffix, baseSuffix string) []Cache {
	if !preparesDatabase {
		return nil
	}
	caches := []Cache{{
		Path:   testDBCacheDir,
		Volume: "orobox-test-dbdump-" + suffix,
	}}
	if baseSuffix != "" && baseSuffix != suffix {
		caches = append(caches, Cache{
			Path:   testDBBaseCacheDir,
			Volume: "orobox-test-dbdump-" + baseSuffix,
		})
	}
	return caches
}

// testDatabaseCommand restores the cached Oro install, seeds one from the base scope, seeds one
// from the dump the runtime image carries, or performs one and caches it — in that order.
//
// The fingerprint covers composer.lock and every migration file, which is what decides the schema,
// so a normal commit restores and only a dependency or migration change pays for anything more.
// The client's major version is part of it too: a dump is only restorable by the version pair that
// wrote it, so a changed client must produce a new dump rather than read the old one.
//
// The middle rung is what makes a new branch cheap. Its dump volume is empty, but the base scope's
// is not, so the branch starts from the base branch's schema and applies only the migrations it
// adds — oro:platform:update does exactly that, and its failure is a real failure of the branch's
// migrations, because the database it runs against came from a dump and not from a volume of
// unknown provenance. The result is cached under the branch's own key, so the second pipeline on
// that branch takes the first rung.
//
// The third rung is what makes a runner with no history cheap, which on GitHub is every runner:
// both cache volumes above are created empty with the job, so before this rung existed every CI
// run paid for a full oro:install. The image dump is an install of the same Oro version with
// nothing on top, so what oro:platform:update applies here is the whole project — its own
// migrations and whatever its dependencies add — rather than one branch's delta. That is still
// far less work than installing, and it is the same reconciliation the two rungs above run.
//
// Both restoring rungs end with oro:platform:update, and that is not only about migrations. A dump
// carries the schema and the oro_entity_config rows that describe the extend fields, but none of
// the generated classes that make those fields reachable — those live in var/cache, which a fresh
// container does not have. A kernel booting without them comes up blind to every extend field:
// Oro generates empty stubs so the boot can succeed, and the first query touching an extend field
// then dies on a semantic error naming a column that is plainly in the database. A bare
// cache:warmup does not help, because it boots on top of those same stubs. oro:platform:update
// rebuilds the extend config, regenerates the classes and warms what depends on them — which is
// what b2b's own pipeline ran before PHPUnit, against this same kind of restored database, for as
// long as it was green.
//
// Only the full-install rung is exempt: oro:install does all of it as part of installing.
//
// psql and pg_dump come from the distribution rather than the image, as rsync does in the release
// step, and the major is pinned to the service's. The unversioned package is whatever the
// distribution defaults to — Alpine 3.24 ships 18 — and a newer client is not compatible in this
// direction: pg_dump writes the settings its own server understands, so a dump from 18 restored
// into 16 dies on `unrecognized configuration parameter "transaction_timeout"`, a setting that
// only exists from 17 on. The fallback covers a major the distribution has not packaged; it is
// no worse than what the unversioned package would have given.
func testDatabaseCommand(postgresVersion string) string {
	major := postgresMajor(postgresVersion)
	return fmt.Sprintf(`# Preparing the test database: restoring a cached Oro install, seeding one, or installing it
set -e
apk add --no-cache postgresql%[8]s-client >/dev/null 2>&1 || apk add --no-cache postgresql-client >/dev/null
export PGPASSWORD=%[6]s
psql_run() { psql -h %[4]s -U %[5]s -d %[7]s -v ON_ERROR_STOP=1 "$@"; }
reset_schema() {
  psql_run -c 'DROP SCHEMA IF EXISTS public CASCADE'
  psql_run -c 'CREATE SCHEMA public'
  psql_run -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp"'
  psql_run -c 'CREATE EXTENSION IF NOT EXISTS pg_trgm'
}
restore_dump() {
  reset_schema
  gunzip -c "$1" | psql_run
}
save_dump() {
  pg_dump -h %[4]s -U %[5]s --no-owner --no-privileges %[7]s | gzip -c > %[1]s
  if [ -n "$fingerprint" ]; then
    printf '%%s' "$fingerprint" > %[2]s
  fi
}
# The dump replaced the database wholesale, so everything var/cache/test holds describes the
# previous one: the dumped container, the generated extend classes, the compiled Doctrine
# metadata. Both functions below boot on top of whatever is there, so a cache left over from
# another install is not a stale detail but the state they reason from — on Oro 7.0 that surfaced
# as "Circular reference detected for service doctrine.orm.default_entity_manager", a container
# error with nothing to do with the database it was handed. var/cache is the mounted directory
# and var/cache/test an ordinary one inside it, so this removes a directory and not a mount.
#
# regenerate is for a dump that is already migrated: it rebuilds the extend entity classes the
# dump does not carry, and nothing else. rebuild also applies the migrations the restored schema
# is missing, which is what the seeding rungs need.
regenerate() {
  rm -rf var/cache/test
  php bin/console oro:entity-extend:cache:warmup --env=test
  php bin/console cache:warmup --env=test
}
rebuild() {
  rm -rf var/cache/test
  php bin/console oro:platform:update --force --env=test --timeout=0 --skip-download-translations --skip-translations
}
%[10]s
if [ -n "$fingerprint" ]; then
  fingerprint="$fingerprint-pg%[8]s"
fi
php bin/console doctrine:database:create --env=test --if-not-exists
if [ -n "$fingerprint" ] && [ -z "$OROBOX_NO_CACHE" ] && [ "$(cat %[2]s 2>/dev/null)" = "$fingerprint" ] && [ -f %[1]s ]; then
  echo 'Restoring the cached test database.'
  restore_dump %[1]s
  # No migrations here, only the extend classes: the fingerprint this dump was saved under is the
  # one the sources have now, so there is nothing pending to apply. Running the update anyway is
  # not merely wasted time, it breaks the suites it is preparing. oro:platform:update replays the
  # test environment migrations on every run — they carry no version and are never recorded in
  # oro_migrations — and the dump already holds their result. They guard against that themselves,
  # but on Oro 7.0 the guard misses: Oro\Bundle\ProductBundle\Tests\Functional\Environment\
  # AddAttributesToProductMigration returns early only when oro_product has testAttrEnum_id, and
  # 7.0's addEnumField stopped adding a column for an enum field, so the migration runs a second
  # time and dies on: The column "testattrmanytoone_id" on table "oro_product" already exists.
  if regenerate; then
    exit 0
  fi
  echo 'Regenerating the caches over the restored dump failed; falling back to the full rebuild.'
  if rebuild; then
    exit 0
  fi
  echo 'The rebuild failed too; falling back to a full install.'
fi
if [ -z "$OROBOX_NO_CACHE" ] && [ -f %[9]s ]; then
  echo 'Seeding the test database from the base scope dump.'
  restore_dump %[9]s
  echo 'Applying the migrations this scope adds on top of the base dump.'
  if rebuild; then
    save_dump
    exit 0
  fi
  echo 'The migrations on top of the base dump failed; falling back to a full install.'
fi
if [ -z "$OROBOX_NO_CACHE" ] && [ -f %[11]s ]; then
  echo 'Seeding the test database from the dump the runtime image carries.'
  restore_dump %[11]s
  echo 'Applying the migrations this project adds on top of the image dump.'
  if rebuild; then
    save_dump
    exit 0
  fi
  echo 'The migrations on top of the image dump failed; falling back to a full install.'
fi
echo 'Installing Oro for the test suites and caching the result. Later runs restore the dump.'
%[3]s
# A rung that got as far as restoring a dump and then failed left a half-migrated schema behind,
# and oro:install has no --drop-database here: it would install on top of it and stop on the first
# table it cannot create. Emptying the schema first makes the install the clean bottom rung it is
# meant to be, whether it runs after a failed rung or on a database that was only just created.
reset_schema
rm -rf var/cache/test
php bin/console oro:install --no-interaction --env=test --skip-translations
save_dump`,
		testDBDumpFile, testDBStampFile, oroWritableDirsCommand(),
		testDBService, testDBUser, testDBPassword, testDBName, major,
		testDBBaseDumpFile,
		sourceFingerprintCommand("the cached test database will be rebuilt and not cached"),
		testDBSeedDumpFile(postgresVersion))
}

// postgresMajor is the major version of a pinned image tag: "16.1-alpine" is 16. It names the
// Alpine client package, and the client must not be newer than the server it talks to.
//
// The same major also names the seed dump the runtime image carries, so the two readings must
// agree: they share config.PostgresMajor rather than each keeping a copy of the rule.
func postgresMajor(version string) string {
	return config.PostgresMajor(version)
}

// dsn builds the Doctrine URL for one of the pipeline's Postgres services. Both databases use
// the same credentials, so only the host and the database name change.
func dsn(service, database, postgresVersion string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable&charset=utf8&serverVersion=%s",
		testDBUser, testDBPassword, service, database, postgresVersion)
}

func (p *Plan) testCommands(postgresVersion string) []string {
	// The dev dependencies — PHPUnit reaches the container through symfony/phpunit-bridge, a
	// require-dev package — come from the deps-dev layer. All that is left here is an autoloader
	// that knows the project's own classes.
	commands := []string{"composer dump-autoload --optimize --no-scripts"}

	if p.PreparesTestDatabase {
		// Functional tests need a real schema. The install is cached as a dump and restored on
		// every run, so the suites always start from the same database.
		//
		// Rebuilding the caches is the restoring rungs' own job, not a step bolted on here: see
		// testDatabaseCommand.
		commands = append(commands, oroWritableDirsCommand(), testDatabaseCommand(postgresVersion))
	}

	runs := p.phpunitRuns()
	if p.Report == qatools.ReportNone {
		return append(commands, runs...)
	}
	return append(commands, phpunitReportScript(runs, TestReportDir()))
}

// phpunitRuns renders one PHPUnit invocation per configured suite, or a single invocation of
// everything phpunit.xml defines when no suite is named — which is what `orobox test` without
// arguments means, on either engine.
func (p *Plan) phpunitRuns() []string {
	render := func(suite string) string {
		command := "php bin/simple-phpunit"
		if suite != "" {
			command += " --testsuite " + suite
		}
		if p.Filter != "" {
			command += " --filter " + p.Filter
		}
		if p.Report != qatools.ReportNone {
			// One log per invocation: PHPUnit truncates the file it is given, so a shared path
			// would leave only the last suite's results. The runner merges them.
			name := "junit.xml"
			if suite != "" {
				name = "junit-" + suite + ".xml"
			}
			command += " --log-junit " + TestReportDir() + "/" + name
		}
		return command
	}

	if len(p.Suites) == 0 {
		return []string{render("")}
	}

	runs := make([]string, 0, len(p.Suites))
	for _, suite := range p.Suites {
		runs = append(runs, render(suite))
	}
	return runs
}

// phpunitReportScript runs the PHPUnit invocations without letting a failure end the step, for the
// same reason qatools.ReportScript does: a failed exec makes the Dagger container unreadable, and
// a failing suite is exactly when the JUnit report is wanted. The outcome travels in the status
// file and the caller turns it back into an exit code.
func phpunitReportScript(runs []string, reportDir string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "mkdir -p %s\nstatus=0\n", reportDir)
	for _, run := range runs {
		fmt.Fprintf(&b, "%s || status=1\n", run)
	}
	fmt.Fprintf(&b, "printf '%%s' \"$status\" > %s/%s\n", reportDir, qatools.StatusFile)
	b.WriteString("exit 0")

	return b.String()
}

// testServices binds the databases and search services the test run needs. Only what the
// project actually enables is started, so a project without Elasticsearch pays nothing.
func testServices(conf *config.OroConfig, versions config.OroVersions) []Service {
	services := []Service{
		{
			Name:  testDBService,
			Image: "postgres:" + versions.Postgres,
			Port:  5432,
			Args:  postgresArgs(),
			Env: map[string]string{
				"POSTGRES_DB":       testDBName,
				"POSTGRES_USER":     testDBUser,
				"POSTGRES_PASSWORD": testDBPassword,
			},
		},
	}

	if conf.Services.Redis {
		services = append(services, Service{Name: "redis", Image: "redis:" + versions.Redis, Port: 6379})
	}
	if conf.Services.Elasticsearch {
		services = append(services, Service{
			Name:  "elasticsearch",
			Image: "elasticsearch:" + versions.Elasticsearch,
			Port:  9200,
			Env: map[string]string{
				"discovery.type":         "single-node",
				"xpack.security.enabled": "false",
			},
		})
	}

	return services
}

// releaseEnv is the bridge between .orobox.yaml and deploy.php: the stub reads exactly these
// variables, so the two can never disagree about where a stage deploys.
//
// ref is passed in rather than read from the stage: with --ref the pipeline builds a commit the
// stage configuration does not name, and Deployer has to check out that same commit or the
// uploaded artifacts and the deployed code would belong to two different revisions.
func releaseEnv(stage config.StageConfig, ref, repository, sourceDir string) map[string]string {
	env := map[string]string{
		"OROBOX_DEPLOY_REPOSITORY":    repository,
		"OROBOX_DEPLOY_REF":           ref,
		"OROBOX_DEPLOY_HOST":          stage.Host,
		"OROBOX_DEPLOY_USER":          stage.User,
		"OROBOX_DEPLOY_PORT":          strconv.Itoa(stage.SSHPort()),
		"OROBOX_DEPLOY_PATH":          stage.DeployPath,
		"OROBOX_DEPLOY_KEEP_RELEASES": strconv.Itoa(stage.Releases()),
		"OROBOX_DEPLOY_ARTIFACT_DIR":  "/artifacts",
	}
	if stage.RestartCommand != "" {
		env["OROBOX_DEPLOY_RESTART_COMMAND"] = stage.RestartCommand
	}
	if sourceDir != "" {
		// The recipe passes this to Deployer's sub_directory, so the release directory holds the
		// application itself and not the monorepo root.
		env["OROBOX_DEPLOY_SUB_DIRECTORY"] = sourceDir
	}
	return env
}
