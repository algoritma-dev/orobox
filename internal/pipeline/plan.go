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

// releaseStepName is the step the reporter streams in full: it runs alone and last, so its
// output cannot interleave with another step's.
const releaseStepName = "release"

// Service is a container the pipeline binds to a step under Name as its hostname.
type Service struct {
	Name  string
	Image string
	Port  int
	Env   map[string]string
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

// Plan is the fully resolved pipeline for one stage. The DAG shape is fixed — vendor feeds
// assets, qa and test; release consumes the artifacts — so the plan only carries the
// contents of each step.
type Plan struct {
	Stage       config.StageConfig
	Repository  string
	Ref         string
	Image       string
	OroVersion  string
	ArtifactDir string // host directory the tarballs are exported to
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

// BuildsAssets reports whether the pipeline builds and ships the webpack assets.
func (p *Plan) BuildsAssets() bool {
	return p.Assets != nil
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

// Artifacts lists the tarball names this plan produces, in upload order.
func (p *Plan) Artifacts() []string {
	artifacts := []string{config.VendorArtifactName}
	if p.BuildsAssets() {
		artifacts = append(artifacts, config.AssetsArtifactName)
	}
	return artifacts
}

// New resolves a stage into a plan. repository is passed in rather than read from the config
// so the caller can fall back to the git origin and report that failure with context.
func New(conf *config.OroConfig, stage config.StageConfig, repository string) *Plan {
	oroRoot := config.OroRootDir
	versions := config.GetVersionsForOro(conf.OroVersion)
	suffix := volumeSuffix(conf.OroVersion, stage.Ref)

	p := &Plan{
		Stage:        stage,
		Repository:   repository,
		Ref:          stage.Ref,
		Image:        fmt.Sprintf("algoritmadev/orobox:%s-%s-latest", conf.OroVersion, config.InstallTypeProject),
		OroVersion:   conf.OroVersion,
		ArtifactDir:  filepath.Join(config.DeployArtifactsDir, stage.Name),
		SourceDir:    conf.Deploy.Source(),
		ComposerAuth: docker.ComposerAuthJSON(conf.Composer.Auth),
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

	if !conf.Deploy.PreBuiltAssetsEnabled {
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
		Commands: qaCommands(conf.OroVersion),
		Services: qaServices(suffix, versions.Postgres),
		Caches:   qaCaches(suffix),
	}

	p.Test = Step{
		Name:     "test",
		Workdir:  oroRoot,
		Env:      testEnv(versions.Postgres),
		Commands: testCommands(stage),
		Services: testServices(conf, versions),
		Caches:   testCaches(stage, suffix),
	}

	p.Release = Step{
		Name:    releaseStepName,
		Workdir: oroRoot,
		Env:     releaseEnv(stage, repository, p.SourceDir),
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
	return `php -r '$manifest = json_decode(@file_get_contents("composer.json"), true) ?: [];
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
func volumeSuffix(oroVersion, ref string) string {
	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, ref)
	return oroVersion + "-" + sanitized
}

// qaServices is the QA step's own Postgres, with its data directory on a named cache volume.
// That is what makes the Oro install reusable: the next run finds the schema already there and
// only has to check the fingerprint.
func qaServices(suffix, postgresVersion string) []Service {
	return []Service{{
		Name:      qaDBService,
		Image:     "postgres:" + postgresVersion,
		Port:      5432,
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
func qaWarmupCommand() string {
	return fmt.Sprintf(`set -e
stamp=%[1]s/.orobox-qa-fingerprint
fingerprint=$(cat composer.lock $(find src -path '*Migrations*' -type f 2>/dev/null | sort) 2>/dev/null | md5sum | cut -c1-32)
if [ -z "$OROBOX_NO_CACHE" ] && [ "$(cat "$stamp" 2>/dev/null)" = "$fingerprint" ] && [ -f %[2]s ] && [ -d %[3]s ]; then
  echo 'Reusing the cached Oro install and warmed test cache.'
  exit 0
fi
echo 'Rebuilding the QA cache: installing Oro and warming the test cache. Later runs reuse this.'
rm -rf %[4]s "$stamp"
%[5]s
%[6]s
php bin/console oro:install --env=test --no-interaction --drop-database --skip-translations
php bin/console cache:warmup --env=test
printf '%%s' "$fingerprint" > "$stamp"`,
		qatools.CacheVolumeDir(), qatools.ContainerXMLPath(), qatools.SymfonyConfigDir(),
		qatools.CacheDir(), oroWritableDirsCommand(), qaDatabaseResetCommand())
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
			qatools.ComposerInstallCommand(plan.ComposerPackages),
		)
	}

	if plan.NeedsJSTools {
		jsInstall := fmt.Sprintf("cd %s && %s %s %s", qaDir, plan.JSManager, plan.JSInstallArg, plan.JSSaveDevFlag)
		if plan.JSManager == "pnpm" {
			jsInstall += " --ignore-workspace-root-check"
		}
		commands = append(commands, jsInstall+" "+strings.Join(plan.JSPackages, " "))
	}

	return commands
}

// qaCommands is the per-commit half of the QA stage: it runs after the sources are overlaid on
// the qa-tools layer, and runs every enabled tool in check-only mode. The configuration scripts
// are here rather than in the layer because they must not overwrite a configuration committed in
// the repository, and the overlay is what puts that configuration in place.
func qaCommands(oroVersion string) []string {
	plan := qatools.NewInstallPlan(oroVersion)
	oroRoot := config.OroRootDir

	// The layers installed the packages without an autoloader; the classmap needs src/, which
	// only exists now.
	commands := []string{"composer dump-autoload --optimize --no-scripts"}

	if plan.NeedsComposerTools && plan.NeedsPhpstan {
		commands = append(commands, qatools.PhpstanConfigScript())
	}
	if plan.NeedsComposerTools && plan.NeedsTwigCS {
		commands = append(commands, qatools.TwigConfigScript())
	}
	if plan.NeedsPhpstan {
		commands = append(commands, qaWarmupCommand())
	}

	var enabled []qatools.Tool
	for _, tool := range qatools.Tools(oroRoot, config.QaAnalyzePathFor(config.InstallTypeProject), qatools.ModeCheck) {
		if config.IsQaToolEnabled(tool.Name) {
			enabled = append(enabled, tool)
		}
	}
	if len(enabled) > 0 {
		commands = append(commands, qatools.Script(enabled))
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
)

// testCaches mounts the database dump for a stage that installs Oro. A unit-only stage never
// touches a schema, so it mounts nothing and pays nothing.
func testCaches(stage config.StageConfig, suffix string) []Cache {
	if !stage.RunsFunctionalTests() {
		return nil
	}
	return []Cache{{
		Path:   testDBCacheDir,
		Volume: "orobox-test-dbdump-" + suffix,
	}}
}

// testDatabaseCommand restores the cached Oro install, or performs one and caches it. The
// fingerprint covers composer.lock and every migration file — what decides the schema — so a
// normal commit restores and only a dependency or migration change pays for an install.
//
// psql and pg_dump come from the distribution rather than the image, as rsync does in the release
// step. The dump is plain SQL, so a client newer than the pinned server is fine; the reverse never
// happens, because the client always follows the distribution.
func testDatabaseCommand() string {
	return fmt.Sprintf(`set -e
apk add --no-cache postgresql-client >/dev/null
export PGPASSWORD=%[6]s
psql_run() { psql -h %[4]s -U %[5]s -d %[7]s -v ON_ERROR_STOP=1 "$@"; }
stamp=%[2]s
fingerprint=$(cat composer.lock $(find src -path '*Migrations*' -type f 2>/dev/null | sort) 2>/dev/null | md5sum | cut -c1-32)
php bin/console doctrine:database:create --env=test --if-not-exists
if [ -z "$OROBOX_NO_CACHE" ] && [ "$(cat "$stamp" 2>/dev/null)" = "$fingerprint" ] && [ -f %[1]s ]; then
  echo 'Restoring the cached test database.'
  psql_run -c 'DROP SCHEMA IF EXISTS public CASCADE'
  psql_run -c 'CREATE SCHEMA public'
  psql_run -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp"'
  psql_run -c 'CREATE EXTENSION IF NOT EXISTS pg_trgm'
  gunzip -c %[1]s | psql_run
  exit 0
fi
echo 'Installing Oro for the test suites and caching the result. Later runs restore the dump.'
%[3]s
php bin/console oro:install --no-interaction --env=test --skip-translations
pg_dump -h %[4]s -U %[5]s --no-owner --no-privileges %[7]s | gzip -c > %[1]s
printf '%%s' "$fingerprint" > "$stamp"`,
		testDBDumpFile, testDBStampFile, oroWritableDirsCommand(),
		testDBService, testDBUser, testDBPassword, testDBName)
}

// dsn builds the Doctrine URL for one of the pipeline's Postgres services. Both databases use
// the same credentials, so only the host and the database name change.
func dsn(service, database, postgresVersion string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable&charset=utf8&serverVersion=%s",
		testDBUser, testDBPassword, service, database, postgresVersion)
}

func testCommands(stage config.StageConfig) []string {
	// The dev dependencies — PHPUnit reaches the container through symfony/phpunit-bridge, a
	// require-dev package — come from the deps-dev layer. All that is left here is an autoloader
	// that knows the project's own classes.
	commands := []string{"composer dump-autoload --optimize --no-scripts"}

	if stage.RunsFunctionalTests() {
		// Functional tests need a real schema. The install is cached as a dump and restored on
		// every run, so the suites always start from the same database.
		commands = append(commands, oroWritableDirsCommand(), testDatabaseCommand())
	}

	for _, suite := range stage.Suites() {
		commands = append(commands, "php bin/simple-phpunit --testsuite "+suite)
	}

	return commands
}

// testServices binds the databases and search services the test run needs. Only what the
// project actually enables is started, so a project without Elasticsearch pays nothing.
func testServices(conf *config.OroConfig, versions config.OroVersions) []Service {
	services := []Service{
		{
			Name:  testDBService,
			Image: "postgres:" + versions.Postgres,
			Port:  5432,
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
func releaseEnv(stage config.StageConfig, repository, sourceDir string) map[string]string {
	env := map[string]string{
		"OROBOX_DEPLOY_REPOSITORY":    repository,
		"OROBOX_DEPLOY_REF":           stage.Ref,
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
