package pipeline

import (
	"reflect"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/spf13/viper"
)

func testStage() config.StageConfig {
	return config.StageConfig{
		Name:       "staging",
		Ref:        "develop",
		Host:       "staging.acme.com",
		User:       "deploy",
		DeployPath: "/var/www/oro",
	}
}

func testConf(oroVersion string, preBuilt bool) *config.OroConfig {
	return &config.OroConfig{
		Type:       config.InstallTypeProject,
		OroVersion: oroVersion,
		Domains:    []config.DomainConfig{{Host: "oro.demo", Root: "public"}},
		Deploy: &config.DeployConfig{
			PreBuiltAssetsEnabled: preBuilt,
			Stages:                []config.StageConfig{testStage()},
		},
	}
}

func joined(commands []string) string {
	return strings.Join(commands, "\n")
}

func TestPlanImageAndArtifactDir(t *testing.T) {
	p := New(testConf("6.1", false), testStage(), "git@gitlab.com:acme/shop.git")

	if p.Image != "algoritmadev/orobox:6.1-project-latest" {
		t.Errorf("Image = %q", p.Image)
	}
	if p.ArtifactDir != "var/orobox/deploy/staging" {
		t.Errorf("ArtifactDir = %q", p.ArtifactDir)
	}
	if p.Ref != "develop" {
		t.Errorf("Ref = %q, want the stage ref", p.Ref)
	}
}

func TestPlanAssetsStageFollowsConfig(t *testing.T) {
	built := New(testConf("6.1", false), testStage(), "repo")
	if !built.BuildsAssets() {
		t.Fatal("pre_built_assets_enabled: false must produce an assets stage")
	}
	if got := built.Artifacts(); len(got) != 2 || got[1] != config.AssetsArtifactName {
		t.Errorf("Artifacts() = %v, want vendor + assets", got)
	}
	assets := joined(built.Assets.Commands)
	if !strings.Contains(assets, "oro:assets:install --env=prod") {
		t.Errorf("assets stage does not build the assets: %s", assets)
	}
	for _, want := range []string{"public/build", "public/js", "public/media/js"} {
		if !strings.Contains(assets, want) {
			t.Errorf("assets archive is missing %s: %s", want, assets)
		}
	}

	preBuilt := New(testConf("6.1", true), testStage(), "repo")
	if preBuilt.BuildsAssets() {
		t.Error("pre_built_assets_enabled: true must not build assets")
	}
	if got := preBuilt.Artifacts(); len(got) != 1 || got[0] != config.VendorArtifactName {
		t.Errorf("Artifacts() = %v, want vendor only", got)
	}
}

func TestPlanVendorStage(t *testing.T) {
	p := New(testConf("7.0", true), testStage(), "repo")
	vendor := joined(p.Vendor.Commands)

	// The dependencies come from the deps layer, which skipped the autoloader because the
	// classmap needs src/; the vendor step dumps it once the sources are in place.
	if !strings.Contains(joined(p.Deps.Commands), "composer install --no-dev") {
		t.Errorf("deps layer does not install the production dependencies: %s", joined(p.Deps.Commands))
	}
	for _, want := range []string{"composer dump-autoload", "--no-dev", "--optimize", "--classmap-authoritative"} {
		if !strings.Contains(vendor, want) {
			t.Errorf("vendor stage missing %q: %s", want, vendor)
		}
	}
	if !strings.Contains(vendor, "tar -czf /artifacts/"+config.VendorArtifactName) {
		t.Errorf("vendor stage does not archive the vendor tree: %s", vendor)
	}
	if p.Vendor.Env["ORO_ENV"] != "prod" {
		t.Errorf("vendor stage ORO_ENV = %q, want prod", p.Vendor.Env["ORO_ENV"])
	}
}

func TestPlanDependencyLayersAreSourceIndependent(t *testing.T) {
	p := New(testConf("6.1", true), testStage(), "repo")

	deps := joined(p.Deps.Commands)
	for _, want := range []string{"composer install", "--no-dev", "--no-autoloader", "--no-scripts"} {
		if !strings.Contains(deps, want) {
			t.Errorf("deps layer missing %q: %s", want, deps)
		}
	}
	// The classmap needs src/, which the layer does not have: dumping it here would produce an
	// authoritative classmap without the project's own classes.
	for _, forbidden := range []string{"--optimize-autoloader", "--classmap-authoritative"} {
		if strings.Contains(deps, forbidden) {
			t.Errorf("deps layer must not dump the autoloader (%s): %s", forbidden, deps)
		}
	}

	depsDev := joined(p.DepsDev.Commands)
	if !strings.Contains(depsDev, "composer install") || strings.Contains(depsDev, "--no-dev") {
		t.Errorf("deps-dev layer must install the dev dependencies: %s", depsDev)
	}
	if !strings.Contains(depsDev, "--no-autoloader") {
		t.Errorf("deps-dev layer must skip the autoloader: %s", depsDev)
	}

	// An Oro application maps src/AppKernel.php in its classmap, and Composer's classmap
	// generator treats a missing entry as fatal, so every autoload dump in the layers — including
	// the implicit one inside `composer require` — fails without a stand-in.
	placeholder := strings.Index(deps, `"classmap", "files"`)
	install := strings.Index(deps, "composer install")
	if placeholder == -1 || placeholder > install {
		t.Errorf("deps layer must create the autoload placeholders before installing: %s", deps)
	}

	// A layer keyed on composer.json and composer.lock cannot read the sources, so nothing in it
	// may touch a path that only exists once the clone is overlaid.
	for _, step := range []Step{p.Deps, p.DepsDev, p.QaTools} {
		if strings.Contains(joined(step.Commands), "bin/console") {
			t.Errorf("layer %q runs the application: %s", step.Name, joined(step.Commands))
		}
	}
}

func TestPlanQaToolsLayerHoldsTheToolInstall(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	p := New(testConf("6.1", true), testStage(), "repo")
	tools := joined(p.QaTools.Commands)

	for _, want := range []string{"composer bin qa", "allow-plugins.bamarni/composer-bin-plugin"} {
		if !strings.Contains(tools, want) {
			t.Errorf("qa-tools layer missing %q: %s", want, tools)
		}
	}
	// Every command runs under `bash -o pipefail`, where a bare `yes y |` turns the successful
	// install into exit code 141 as soon as composer stops reading its stdin.
	if strings.Contains(tools, "yes y | ") {
		t.Errorf("the QA install pipes `yes` without swallowing its SIGPIPE status: %s", tools)
	}

	// The configuration scripts must not be in the layer: a configuration committed in the
	// repository has to win, and the sources are not there yet.
	if strings.Contains(tools, "phpstan.neon") || strings.Contains(tools, ".twig-cs-fixer.php") {
		t.Errorf("qa-tools layer writes a configuration file before the sources exist: %s", tools)
	}
	qa := joined(p.QA.Commands)
	if !strings.Contains(qa, "phpstan.neon") {
		t.Errorf("qa step must write the PHPStan configuration after the overlay: %s", qa)
	}
}

func TestPlanStepsDumpTheAutoloaderAfterTheOverlay(t *testing.T) {
	stage := testStage()
	stage.TestSuites = []string{config.TestSuiteUnit}
	p := New(testConf("6.1", true), stage, "repo")

	vendor := joined(p.Vendor.Commands)
	for _, want := range []string{"composer dump-autoload", "--no-dev", "--optimize", "--classmap-authoritative"} {
		if !strings.Contains(vendor, want) {
			t.Errorf("vendor step missing %q: %s", want, vendor)
		}
	}
	// The dependencies come from the deps layer; re-installing them would throw the cache away.
	if strings.Contains(vendor, "composer install") {
		t.Errorf("vendor step must not install: %s", vendor)
	}
	dump := strings.Index(vendor, "composer dump-autoload")
	archive := strings.Index(vendor, "tar -czf")
	if dump == -1 || archive == -1 || dump > archive {
		t.Errorf("vendor step has dump=%d archive=%d, the autoloader must be in the tarball", dump, archive)
	}

	for name, commands := range map[string][]string{"qa": p.QA.Commands, "test": p.Test.Commands} {
		joinedCommands := joined(commands)
		if strings.Contains(joinedCommands, "composer install") {
			t.Errorf("%s step must not install, the deps-dev layer already did: %s", name, joinedCommands)
		}
		if !strings.Contains(joinedCommands, "composer dump-autoload") {
			t.Errorf("%s step must dump the autoloader after the overlay: %s", name, joinedCommands)
		}
	}
}

func TestPlanQAStageIsCheckOnlyAndSelfInstalling(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	p := New(testConf("6.1", true), testStage(), "repo")
	qa := joined(p.QA.Commands)

	// A clean clone has no vendor-bin/qa, so the pipeline installs the tools itself; that work
	// lives in the source-independent layer, which TestPlanQaToolsLayerHoldsTheToolInstall covers.
	if !strings.Contains(joined(p.QaTools.Commands), "composer bin qa") {
		t.Error("the pipeline never installs the QA tools")
	}

	// Nothing may mutate the sources.
	if strings.Contains(qa, "--fix") {
		t.Errorf("qa stage runs a tool with --fix: %s", qa)
	}
	if !strings.Contains(qa, "--dry-run") {
		t.Errorf("qa stage is missing --dry-run, so it is not check-only: %s", qa)
	}
}

func TestPlanQAStageReusesTheInstalledCache(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	p := New(testConf("6.1", true), testStage(), "repo")
	qa := joined(p.QA.Commands)

	// PHPStan reads the dumped test debug container, and oro:install only runs non-interactively
	// without user options in test, so the whole stage runs in test with debug on.
	for name, want := range map[string]string{"ORO_ENV": "test", "APP_ENV": "test", "ORO_DEBUG": "1"} {
		if got := p.QA.Env[name]; got != want {
			t.Errorf("qa stage %s = %q, want %q", name, got, want)
		}
	}
	if host := p.QA.Env["ORO_DB_HOST"]; host != qaDBService {
		t.Errorf("qa stage ORO_DB_HOST = %q, want %q", host, qaDBService)
	}

	// The warmed cache and the database it was built against are both persistent, and the
	// fingerprint decides whether they still match the code. The mount is the parent of the test
	// cache so that oro:install's cache:clear can rmdir var/cache/test, which a mount point refuses.
	if len(p.QA.Caches) != 1 || p.QA.Caches[0].Path != qatools.CacheVolumeDir() {
		t.Fatalf("qa stage does not persist the test cache: %+v", p.QA.Caches)
	}
	if p.QA.Caches[0].Path == qatools.CacheDir() {
		t.Errorf("qa cache is mounted on the test cache itself: cache:clear cannot remove a mount point")
	}
	if len(p.QA.Services) != 1 || p.QA.Services[0].DataCache == "" {
		t.Fatalf("qa stage database is not persistent: %+v", p.QA.Services)
	}
	// A service definition identical to the test one would be deduplicated into a single
	// container, and the two concurrent installs would then overwrite each other's schema.
	if p.QA.Services[0].Name == testDBService {
		t.Errorf("qa stage shares the test database service %q", testDBService)
	}

	for _, want := range []string{"orobox-qa-fingerprint", "oro:install --env=test", "cache:warmup --env=test"} {
		if !strings.Contains(qa, want) {
			t.Errorf("qa stage missing cache management step %q", want)
		}
	}

	// oro:install accepts neither --skip-assets nor --timeout on any supported version. Only the
	// oro:install line is checked: oro:platform:update, which the stale-cache path runs, does take
	// --timeout, so scanning the whole script would forbid a flag that is correct there.
	for _, line := range strings.Split(qa, "\n") {
		if !strings.Contains(line, "oro:install") {
			continue
		}
		for _, forbidden := range []string{"--skip-assets", "--timeout"} {
			if strings.Contains(line, forbidden) {
				t.Errorf("qa oro:install must not receive %q: %s", forbidden, line)
			}
		}
	}

	// The install has to precede the analysis, and the autoloader has to precede both.
	dump := strings.Index(qa, "composer dump-autoload")
	install := strings.Index(qa, "oro:install")
	phpstan := strings.Index(qa, "bin/phpstan")
	if dump == -1 || install == -1 || phpstan == -1 || dump > install || install > phpstan {
		t.Errorf("qa stage order is dump=%d install=%d phpstan=%d: %s", dump, install, phpstan, qa)
	}
}

func TestPlanEmptiesQADatabaseBeforeInstalling(t *testing.T) {
	p := New(testConf("6.1", true), testStage(), "repo")
	qa := joined(p.QA.Commands)

	// The QA database lives on a reused volume, and oro:install --drop-database leaves the tables
	// Doctrine has no mapping for: an oro_session left behind fails the next install with
	// `relation "oro_session" already exists`, and a left-behind oro_migrations silently skips
	// migrations. Dropping the schema is what removes them.
	drop := strings.Index(qa, "DROP SCHEMA IF EXISTS public CASCADE")
	install := strings.Index(qa, "oro:install")
	if drop == -1 || install == -1 || drop > install {
		t.Errorf("qa stage has drop=%d install=%d, the schema must be dropped first: %s", drop, install, qa)
	}

	// Dropping the schema takes Oro's extensions with it, and the service's initdb script only
	// runs when the data volume is initialised, so the rebuild has to put them back itself.
	for _, extension := range []string{`\"uuid-ossp\"`, "pg_trgm"} {
		want := "CREATE EXTENSION IF NOT EXISTS " + extension
		if index := strings.Index(qa, want); index == -1 || index > install {
			t.Errorf("qa stage does not recreate %s before oro:install: %s", extension, qa)
		}
	}
}

func TestPlanCreatesWritableDirsBeforeInstalling(t *testing.T) {
	stage := testStage()
	stage.TestSuites = []string{config.TestSuiteUnit, config.TestSuiteFunctional}
	p := New(testConf("6.1", true), stage, "repo")

	// var/ is gitignored, so a clone has no var/data and Oro's requirements check — which
	// oro:install runs first — refuses to go any further.
	for name, commands := range map[string][]string{"qa": p.QA.Commands, "test": p.Test.Commands} {
		joinedCommands := joined(commands)
		mkdir := strings.Index(joinedCommands, "mkdir -p var/data")
		install := strings.Index(joinedCommands, "oro:install")
		if mkdir == -1 || install == -1 || mkdir > install {
			t.Errorf("%s stage has mkdir=%d install=%d, var/data must exist first: %s",
				name, mkdir, install, joinedCommands)
		}
	}
}

func TestPlanQAStageRespectsDisabledTools(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	viper.Set("test.qa.eslint", false)
	viper.Set("test.qa.stylelint", false)

	qa := joined(New(testConf("6.1", true), testStage(), "repo").QA.Commands)
	if strings.Contains(qa, "npx --yes eslint") || strings.Contains(qa, "npx --yes stylelint") {
		t.Errorf("disabled JS tools still run: %s", qa)
	}
	if !strings.Contains(qa, "bin/phpstan") {
		t.Errorf("enabled tools must still run: %s", qa)
	}
}

func TestPlanCacheVolumesAreScopedToTheRef(t *testing.T) {
	stage := testStage()
	stage.Ref = "feature/big bang"
	p := New(testConf("6.1", true), stage, "repo")

	// A ref becomes part of a volume name, so anything but the usual identifier characters is
	// folded into a dash.
	const suffix = "6.1-feature-big-bang"

	if got := p.QA.Caches[0].Volume; got != "orobox-qa-cache-"+suffix {
		t.Errorf("qa cache volume = %q, want the ref-scoped name", got)
	}
	if got := p.QA.Services[0].DataCache; got != "orobox-qa-db-"+suffix {
		t.Errorf("qa database volume = %q, want the ref-scoped name", got)
	}
}

func TestPlanTestStageSuites(t *testing.T) {
	unitOnly := New(testConf("6.1", true), testStage(), "repo")
	unit := joined(unitOnly.Test.Commands)
	if strings.Contains(unit, "oro:install") {
		t.Errorf("unit-only stage must not install the application: %s", unit)
	}
	if !strings.Contains(unit, "bin/simple-phpunit --testsuite unit") {
		t.Errorf("unit suite missing: %s", unit)
	}
	// The dev dependencies come from the deps-dev layer; the step only has to dump an autoloader
	// that knows the project's own classes before PHPUnit boots.
	dump := strings.Index(unit, "composer dump-autoload")
	phpunit := strings.Index(unit, "bin/simple-phpunit")
	if dump == -1 || dump > phpunit {
		t.Errorf("test stage must dump the autoloader before running PHPUnit: %s", unit)
	}
	if strings.Contains(joined(unitOnly.DepsDev.Commands), "--no-dev") {
		t.Errorf("the deps-dev layer must install the dev dependencies: %s", joined(unitOnly.DepsDev.Commands))
	}

	stage := testStage()
	stage.TestSuites = []string{config.TestSuiteUnit, config.TestSuiteFunctional}
	full := New(testConf("6.1", true), stage, "repo")
	both := joined(full.Test.Commands)
	for _, want := range []string{
		"doctrine:database:create --env=test",
		"oro:install --no-interaction --env=test",
		"--testsuite unit",
		"--testsuite functional",
	} {
		if !strings.Contains(both, want) {
			t.Errorf("functional stage missing %q: %s", want, both)
		}
	}

	// oro:install accepts neither --skip-assets nor --timeout on any supported version. Only the
	// oro:install line is checked: oro:platform:update, which the seeding rung runs, does take
	// --timeout, so scanning the whole block would forbid a flag that is correct there.
	for _, line := range strings.Split(both, "\n") {
		if !strings.Contains(line, "oro:install") {
			continue
		}
		for _, forbidden := range []string{"--skip-assets", "--timeout"} {
			if strings.Contains(line, forbidden) {
				t.Errorf("oro:install must not receive %q: %s", forbidden, line)
			}
		}
	}
}

func TestPlanTestStageCachesTheOroInstall(t *testing.T) {
	stage := testStage()
	stage.TestSuites = []string{config.TestSuiteUnit, config.TestSuiteFunctional}
	p := New(testConf("6.1", true), stage, "repo")
	test := joined(p.Test.Commands)

	if len(p.Test.Caches) != 1 || p.Test.Caches[0].Path != testDBCacheDir {
		t.Fatalf("test stage does not persist the database dump: %+v", p.Test.Caches)
	}
	if got := p.Test.Caches[0].Volume; got != "orobox-test-dbdump-6.1-develop" {
		t.Errorf("test dump volume = %q, want the ref-scoped name", got)
	}
	// The dump is the cache; the live database is not. A data volume here would carry whatever
	// the previous run's tests wrote into the next run.
	if p.Test.Services[0].DataCache != "" {
		t.Errorf("the test database service must stay ephemeral: %+v", p.Test.Services[0])
	}

	for _, want := range []string{
		"orobox-test-fingerprint",
		"pg_dump",
		"gunzip -c",
		"oro:install --no-interaction --env=test",
		"DROP SCHEMA IF EXISTS public CASCADE",
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		"apk add --no-cache postgresql16-client",
	} {
		if !strings.Contains(test, want) {
			t.Errorf("test stage missing %q: %s", want, test)
		}
	}

	// The client major follows the service's. An unversioned package is whatever the distribution
	// defaults to, and a newer pg_dump writes settings the pinned server does not recognise.
	if major := postgresMajor(p.Test.Services[0].Image[len("postgres:"):]); !strings.Contains(test, "postgresql"+major+"-client") {
		t.Errorf("test stage installs a client that does not match the %s server: %s", major, test)
	}
	// A dump is only restorable by the version pair that wrote it, so the client is part of what
	// decides whether the cached one can be reused.
	if !strings.Contains(test, "-pg16") {
		t.Errorf("test fingerprint does not cover the client major: %s", test)
	}

	// The fingerprint covers exactly what decides the schema.
	if !strings.Contains(test, "cat composer.lock $(find src -path '*Migrations*'") {
		t.Errorf("test fingerprint does not cover composer.lock and the migrations: %s", test)
	}

	// var/ is gitignored, so the writable directories have to exist before the installer's
	// requirements check runs, and the suites have to come after the database is ready.
	mkdir := strings.Index(test, "mkdir -p var/data")
	database := strings.Index(test, "orobox-test-fingerprint")
	phpunit := strings.Index(test, "bin/simple-phpunit")
	if mkdir == -1 || database == -1 || phpunit == -1 || mkdir > database || database > phpunit {
		t.Errorf("test stage order is mkdir=%d database=%d phpunit=%d: %s", mkdir, database, phpunit, test)
	}
}

func TestPlanUnitOnlyStageHasNoDatabaseCache(t *testing.T) {
	p := New(testConf("6.1", true), testStage(), "repo")
	test := joined(p.Test.Commands)

	// Unit suites need no schema, so they must not pay for a restore or an install.
	if len(p.Test.Caches) != 0 {
		t.Errorf("unit-only stage must not mount a database cache: %+v", p.Test.Caches)
	}
	for _, forbidden := range []string{"oro:install", "pg_dump", "postgresql-client"} {
		if strings.Contains(test, forbidden) {
			t.Errorf("unit-only stage must not run %q: %s", forbidden, test)
		}
	}
}

func TestPlanTestServices(t *testing.T) {
	conf := testConf("6.1", true)
	p := New(conf, testStage(), "repo")

	if len(p.Test.Services) != 1 {
		t.Fatalf("got %d services, want only the database", len(p.Test.Services))
	}
	db := p.Test.Services[0]
	if db.Name != "db-test" {
		t.Errorf("database service name = %q, want db-test", db.Name)
	}
	if db.Image != "postgres:"+config.GetVersionsForOro("6.1").Postgres {
		t.Errorf("database image = %q, want the version pinned for Oro 6.1", db.Image)
	}
	if db.Env["POSTGRES_DB"] != testDBName || db.Env["POSTGRES_USER"] != testDBUser {
		t.Errorf("database service env = %v", db.Env)
	}
	if host := p.Test.Env["ORO_DB_HOST"]; host != db.Name {
		t.Errorf("ORO_DB_HOST = %q, want the bound service hostname %q", host, db.Name)
	}
	if dsn := p.Test.Env["ORO_DB_DSN"]; !strings.Contains(dsn, "@db-test:5432/"+testDBName) {
		t.Errorf("ORO_DB_DSN = %q", dsn)
	}

	conf.Services.Redis = true
	conf.Services.Elasticsearch = true
	withServices := New(conf, testStage(), "repo")
	names := map[string]bool{}
	for _, service := range withServices.Test.Services {
		names[service.Name] = true
	}
	for _, want := range []string{"db-test", "redis", "elasticsearch"} {
		if !names[want] {
			t.Errorf("service %q not bound although enabled in config", want)
		}
	}
}

func TestPlanCacheEnvIsEmptyByDefault(t *testing.T) {
	p := New(testConf("6.1", true), testStage(), "repo")
	if got := p.CacheEnv("1700000000"); len(got) != 0 {
		t.Errorf("CacheEnv() = %v, want nothing when the caches are wanted", got)
	}
}

func TestPlanCacheEnvBustsEveryCache(t *testing.T) {
	p := New(testConf("6.1", true), testStage(), "repo")
	p.NoCache = true

	env := p.CacheEnv("1700000000")
	// The fingerprint scripts read this and treat it as a miss.
	if env["OROBOX_NO_CACHE"] != "1" {
		t.Errorf("OROBOX_NO_CACHE = %q, want 1", env["OROBOX_NO_CACHE"])
	}
	// Dagger keys a layer on the container state, so only a value that differs per run rebuilds
	// the dependency layers.
	if env["OROBOX_CACHE_BUST"] != "1700000000" {
		t.Errorf("OROBOX_CACHE_BUST = %q, want the run id", env["OROBOX_CACHE_BUST"])
	}
}

func TestPlanFingerprintScriptsHonourNoCache(t *testing.T) {
	stage := testStage()
	stage.TestSuites = []string{config.TestSuiteFunctional}
	p := New(testConf("6.1", true), stage, "repo")

	for name, commands := range map[string][]string{"qa": p.QA.Commands, "test": p.Test.Commands} {
		if !strings.Contains(joined(commands), `[ -z "$OROBOX_NO_CACHE" ]`) {
			t.Errorf("%s stage ignores OROBOX_NO_CACHE: %s", name, joined(commands))
		}
	}
}

func TestPlanReleaseEnv(t *testing.T) {
	stage := testStage()
	stage.Port = 2222
	stage.KeepReleases = 3
	stage.RestartCommand = "sudo systemctl restart oro-consumer"

	p := New(testConf("6.1", true), stage, "git@gitlab.com:acme/shop.git")
	env := p.Release.Env

	want := map[string]string{
		"OROBOX_DEPLOY_REPOSITORY":      "git@gitlab.com:acme/shop.git",
		"OROBOX_DEPLOY_REF":             "develop",
		"OROBOX_DEPLOY_HOST":            "staging.acme.com",
		"OROBOX_DEPLOY_USER":            "deploy",
		"OROBOX_DEPLOY_PORT":            "2222",
		"OROBOX_DEPLOY_PATH":            "/var/www/oro",
		"OROBOX_DEPLOY_KEEP_RELEASES":   "3",
		"OROBOX_DEPLOY_ARTIFACT_DIR":    "/artifacts",
		"OROBOX_DEPLOY_RESTART_COMMAND": "sudo systemctl restart oro-consumer",
	}
	for key, value := range want {
		if env[key] != value {
			t.Errorf("release env %s = %q, want %q", key, env[key], value)
		}
	}

	cmd := joined(p.Release.Commands)
	if !strings.Contains(cmd, "vendor-bin/deploy/vendor/bin/dep deploy") {
		t.Errorf("release step does not run Deployer: %s", cmd)
	}
	// A CI clone has no vendor tree, so Deployer must be installed inside the release step.
	if !strings.Contains(cmd, "composer install --working-dir=vendor-bin/deploy") {
		t.Errorf("release step does not install Deployer: %s", cmd)
	}
}

func TestPlanSourceDirReachesTheReleaseEnv(t *testing.T) {
	conf := testConf("6.1", true)
	conf.Deploy.SourceDir = "/b2b/"

	p := New(conf, testStage(), "repo")
	if p.SourceDir != "b2b" {
		t.Errorf("SourceDir = %q, want the normalized %q", p.SourceDir, "b2b")
	}
	if got := p.Release.Env["OROBOX_DEPLOY_SUB_DIRECTORY"]; got != "b2b" {
		t.Errorf("OROBOX_DEPLOY_SUB_DIRECTORY = %q, want b2b", got)
	}
}

func TestPlanWithoutSourceDirOmitsSubDirectory(t *testing.T) {
	p := New(testConf("6.1", true), testStage(), "repo")
	if p.SourceDir != "" {
		t.Errorf("SourceDir = %q, want empty for a repository whose root is the application", p.SourceDir)
	}
	if _, ok := p.Release.Env["OROBOX_DEPLOY_SUB_DIRECTORY"]; ok {
		t.Error("OROBOX_DEPLOY_SUB_DIRECTORY must be absent when no source_dir is configured")
	}
}

func TestPlanReleaseEnvOmitsUnsetRestartCommand(t *testing.T) {
	p := New(testConf("6.1", true), testStage(), "repo")
	if _, ok := p.Release.Env["OROBOX_DEPLOY_RESTART_COMMAND"]; ok {
		t.Error("OROBOX_DEPLOY_RESTART_COMMAND must be absent when no restart command is configured")
	}
	if port := p.Release.Env["OROBOX_DEPLOY_PORT"]; port != "22" {
		t.Errorf("OROBOX_DEPLOY_PORT = %q, want the default 22", port)
	}
}

func TestPlanRunsEverythingByDefault(t *testing.T) {
	p := New(testConf("6.1", false), testStage(), "repo")

	if !p.RunsQA() || !p.RunsTests() || !p.RunsRelease() {
		t.Error("a plan built by New must run every step")
	}
	if !p.NeedsDevDependencies() {
		t.Error("NeedsDevDependencies() = false, want true when QA and the tests both run")
	}
	if got := p.SkippedSteps(); len(got) != 0 {
		t.Errorf("SkippedSteps() = %v, want none", got)
	}
}

func TestPlanSkipPredicates(t *testing.T) {
	tests := []struct {
		name          string
		skipQA        bool
		skipTest      bool
		skipRelease   bool
		wantDevDeps   bool
		wantArtifacts bool
		wantSkipped   []string
	}{
		{name: "nothing skipped", wantDevDeps: true, wantArtifacts: true},
		{name: "qa skipped", skipQA: true, wantDevDeps: true, wantArtifacts: true, wantSkipped: []string{"qa"}},
		{name: "tests skipped", skipTest: true, wantDevDeps: true, wantArtifacts: true, wantSkipped: []string{"test"}},
		{name: "release skipped", skipRelease: true, wantDevDeps: true, wantSkipped: []string{"release"}},
		{name: "qa and tests skipped", skipQA: true, skipTest: true, wantArtifacts: true, wantSkipped: []string{"qa", "test"}},
		// The two CI check jobs: each runs one half of the checks and releases nothing, so neither
		// pays for a tarball the other job, and then the deploy, would build again anyway.
		{name: "qa and release skipped", skipQA: true, skipRelease: true, wantDevDeps: true, wantSkipped: []string{"qa", "release"}},
		{name: "tests and release skipped", skipTest: true, skipRelease: true, wantDevDeps: true, wantSkipped: []string{"test", "release"}},
		// Nothing to check and nothing to ship leaves the tarballs as the run's only product.
		{name: "everything skipped", skipQA: true, skipTest: true, skipRelease: true, wantArtifacts: true, wantSkipped: []string{"qa", "test", "release"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(testConf("6.1", false), testStage(), "repo")
			p.SkipQA, p.SkipTest, p.SkipRelease = tt.skipQA, tt.skipTest, tt.skipRelease

			if p.RunsQA() == tt.skipQA {
				t.Errorf("RunsQA() = %v, want %v", p.RunsQA(), !tt.skipQA)
			}
			if p.RunsTests() == tt.skipTest {
				t.Errorf("RunsTests() = %v, want %v", p.RunsTests(), !tt.skipTest)
			}
			if p.RunsRelease() == tt.skipRelease {
				t.Errorf("RunsRelease() = %v, want %v", p.RunsRelease(), !tt.skipRelease)
			}
			if p.NeedsDevDependencies() != tt.wantDevDeps {
				t.Errorf("NeedsDevDependencies() = %v, want %v", p.NeedsDevDependencies(), tt.wantDevDeps)
			}
			if p.BuildsArtifacts() != tt.wantArtifacts {
				t.Errorf("BuildsArtifacts() = %v, want %v", p.BuildsArtifacts(), tt.wantArtifacts)
			}
			// Artifacts() is what the export loop and the summary read, so the predicate has to
			// reach both through it rather than being re-tested at each call site.
			if got := p.Artifacts(); tt.wantArtifacts != (len(got) > 0) {
				t.Errorf("Artifacts() = %v, want %d entries", got, map[bool]int{true: 1, false: 0}[tt.wantArtifacts])
			}
			if got := p.SkippedSteps(); !reflect.DeepEqual(got, tt.wantSkipped) {
				t.Errorf("SkippedSteps() = %v, want %v", got, tt.wantSkipped)
			}
		})
	}
}

func TestPostgresMajorFromAPinnedTag(t *testing.T) {
	for tag, want := range map[string]string{
		"16.1-alpine": "16",
		"17.6-alpine": "17",
		"17-alpine":   "17",
		"18":          "18",
	} {
		if got := postgresMajor(tag); got != want {
			t.Errorf("postgresMajor(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestPlanDefaultsToStageRefAndScope(t *testing.T) {
	p := New(testConf("6.1", true), testStage(), "repo")

	if p.Ref != "develop" {
		t.Errorf("Ref = %q, want %q", p.Ref, "develop")
	}
	if p.CacheScope != "develop" {
		t.Errorf("CacheScope = %q, want %q", p.CacheScope, "develop")
	}
	if p.BaseCacheScope != "" {
		t.Errorf("BaseCacheScope = %q, want empty", p.BaseCacheScope)
	}
	if got := p.QA.Services[0].DataCache; got != "orobox-qa-db-6.1-develop" {
		t.Errorf("QA data cache = %q", got)
	}
}

func TestPlanRefOverrideMovesBuildAndRelease(t *testing.T) {
	p := NewWithOverrides(testConf("6.1", true), testStage(), "repo", Overrides{Ref: "abc123"})

	if p.Ref != "abc123" {
		t.Errorf("Ref = %q, want %q", p.Ref, "abc123")
	}
	if got := p.Release.Env["OROBOX_DEPLOY_REF"]; got != "abc123" {
		t.Errorf("OROBOX_DEPLOY_REF = %q, want %q", got, "abc123")
	}
	// Senza --cache-scope lo scope segue il ref effettivo.
	if p.CacheScope != "abc123" {
		t.Errorf("CacheScope = %q, want %q", p.CacheScope, "abc123")
	}
}

func TestPlanCacheScopeIsIndependentOfRef(t *testing.T) {
	p := NewWithOverrides(testConf("6.1", true), testStage(), "repo", Overrides{
		Ref:            "abc123",
		CacheScope:     "feature/x",
		BaseCacheScope: "main",
	})

	if p.Ref != "abc123" {
		t.Errorf("Ref = %q", p.Ref)
	}
	if p.CacheScope != "feature/x" {
		t.Errorf("CacheScope = %q", p.CacheScope)
	}
	if p.BaseCacheScope != "main" {
		t.Errorf("BaseCacheScope = %q", p.BaseCacheScope)
	}
	// volumeSuffix sanifica: "/" non è ammesso in un nome di volume.
	if got := p.QA.Services[0].DataCache; got != "orobox-qa-db-6.1-feature-x" {
		t.Errorf("QA data cache = %q", got)
	}
	if got := p.QA.Caches[0].Volume; got != "orobox-qa-cache-6.1-feature-x" {
		t.Errorf("QA cache volume = %q", got)
	}
}

func TestPlanBaseCacheScopeEqualToScopeIsDropped(t *testing.T) {
	p := NewWithOverrides(testConf("6.1", true), testStage(), "repo", Overrides{
		CacheScope:     "main",
		BaseCacheScope: "main",
	})

	if p.BaseCacheScope != "" {
		t.Errorf("BaseCacheScope = %q, want empty when it equals the scope", p.BaseCacheScope)
	}
}

func functionalStage() config.StageConfig {
	stage := testStage()
	stage.TestSuites = []string{config.TestSuiteUnit, config.TestSuiteFunctional}
	return stage
}

func TestTestCachesMountBaseDumpWhenScopesDiffer(t *testing.T) {
	stage := functionalStage()
	p := NewWithOverrides(testConf("6.1", true), stage, "repo", Overrides{
		CacheScope:     "feature/x",
		BaseCacheScope: "main",
	})

	if len(p.Test.Caches) != 2 {
		t.Fatalf("Test.Caches = %d, want 2", len(p.Test.Caches))
	}
	if got := p.Test.Caches[0]; got.Path != "/cache/test-db" || got.Volume != "orobox-test-dbdump-6.1-feature-x" {
		t.Errorf("branch cache = %+v", got)
	}
	if got := p.Test.Caches[1]; got.Path != "/cache/test-db-base" || got.Volume != "orobox-test-dbdump-6.1-main" {
		t.Errorf("base cache = %+v", got)
	}
}

func TestTestCachesMountOnlyTheBranchDumpWithoutABase(t *testing.T) {
	p := NewWithOverrides(testConf("6.1", true), functionalStage(), "repo", Overrides{CacheScope: "main"})

	if len(p.Test.Caches) != 1 {
		t.Fatalf("Test.Caches = %d, want 1", len(p.Test.Caches))
	}
}

func TestUnitOnlyStageMountsNoDatabaseCache(t *testing.T) {
	p := NewWithOverrides(testConf("6.1", true), testStage(), "repo", Overrides{
		CacheScope:     "feature/x",
		BaseCacheScope: "main",
	})

	if len(p.Test.Caches) != 0 {
		t.Errorf("Test.Caches = %d, want 0 for a unit-only stage", len(p.Test.Caches))
	}
}

func TestTestDatabaseLadderHasThreeRungs(t *testing.T) {
	script := joined(NewWithOverrides(testConf("6.1", true), functionalStage(), "repo", Overrides{
		CacheScope:     "feature/x",
		BaseCacheScope: "main",
	}).Test.Commands)

	for _, want := range []string{
		"Restoring the cached test database.",
		"Seeding the test database from the base scope dump.",
		"Applying the migrations this scope adds on top of the base dump.",
		"php bin/console oro:platform:update --force --env=test --timeout=0",
		"Installing Oro for the test suites and caching the result.",
		"php bin/console oro:install --no-interaction --env=test --skip-translations",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("test script is missing %q", want)
		}
	}

	// Il gradino di base legge dal proprio volume e scrive sempre su quello del ramo:
	// una scrittura sul volume di base contaminerebbe ogni ramo che ne discende.
	if strings.Contains(script, "gzip -c > /cache/test-db-base") {
		t.Error("the ladder writes into the base cache directory")
	}
}

func TestQaWarmupUpdatesInsteadOfReinstallingWhenStale(t *testing.T) {
	qa := joined(New(testConf("6.1", true), testStage(), "repo").QA.Commands)

	for _, want := range []string{
		"Reusing the cached Oro install and warmed test cache.",
		"The cached install is stale: applying the pending migrations instead of reinstalling.",
		"php bin/console oro:platform:update --force --env=test --timeout=0",
		"falling back to a full install",
		"Rebuilding the QA cache: installing Oro and warming the test cache.",
	} {
		if !strings.Contains(qa, want) {
			t.Errorf("QA script is missing %q", want)
		}
	}
}

func TestEveryRestoreRungRebuildsTheCaches(t *testing.T) {
	script := joined(NewWithOverrides(testConf("6.1", true), functionalStage(), "repo", Overrides{
		CacheScope:     "feature/x",
		BaseCacheScope: "main",
	}).Test.Commands)

	// A dump carries the schema and the oro_entity_config rows, but none of the generated extend
	// classes that make those fields reachable — so a rung that restores one and stops leaves the
	// kernel blind to every extend field. oro:platform:update regenerates them; a bare
	// cache:warmup does not, because it boots on top of the empty stubs Oro generates to let the
	// boot succeed at all.
	if !strings.Contains(script, "oro:platform:update --force --env=test --timeout=0 --skip-download-translations --skip-translations") {
		t.Errorf("the rebuild does not run oro:platform:update with the skip flags: %s", script)
	}

	// Count invocations, not the shell function definitions that precede them.
	restores := strings.Count(script, "\n  restore_dump ")
	rebuilds := strings.Count(script, "\n  rebuild\n")
	if restores == 0 {
		t.Fatalf("the ladder never restores a dump: %s", script)
	}
	if restores != rebuilds {
		t.Errorf("%d restore(s) but %d rebuild(s): every restored schema must be rebuilt: %s",
			restores, rebuilds, script)
	}
}

func TestUnitOnlyStageNeitherInstallsNorRebuilds(t *testing.T) {
	script := joined(New(testConf("6.1", true), testStage(), "repo").Test.Commands)

	// A unit-only stage installs no database and boots no kernel; rebuilding would be dead time.
	for _, forbidden := range []string{"cache:warmup", "oro:platform:update", "restore_dump"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("unit-only stage must not run %q: %s", forbidden, script)
		}
	}
}

func TestPlanCarriesTheStageSuitesAndDatabaseDecision(t *testing.T) {
	stage := testStage()
	stage.TestSuites = []string{"unit", "functional"}

	p := New(testConf("6.1", false), stage, "repo")

	if !reflect.DeepEqual(p.Suites, []string{"unit", "functional"}) {
		t.Errorf("Suites = %v, want the stage's list", p.Suites)
	}
	if !p.PreparesTestDatabase {
		t.Error("a stage running functional tests must prepare the database")
	}

	unitOnly := New(testConf("6.1", false), testStage(), "repo")
	if unitOnly.PreparesTestDatabase {
		t.Error("a unit-only stage must not pay for a database install")
	}
}

func TestNewChecksBuildsACheckOnlyPlan(t *testing.T) {
	p := NewChecks(testConf("6.1", false), ChecksOptions{
		ProjectDir: "/home/dev/shop",
		CacheScope: "feature-x",
		RunTest:    true,
	})

	if !p.SkipRelease {
		t.Error("a checks plan must never release")
	}
	if !p.SkipQA || p.SkipTest {
		t.Error("RunTest alone must run the test step and skip QA")
	}
	if p.BuildsArtifacts() {
		t.Error("a checks plan must not build the tarballs: nothing consumes them")
	}
	if p.Source.Kind != SourceHost || p.Source.Dir != "/home/dev/shop" {
		t.Errorf("Source = %+v, want the host project directory", p.Source)
	}
	if !p.PreparesTestDatabase {
		t.Error("orobox test always prepares the database, like the compose engine does")
	}
	if p.CacheScope != "feature-x" {
		t.Errorf("CacheScope = %q", p.CacheScope)
	}
	if p.Stage.Name != "" {
		t.Error("a checks plan must not invent a stage")
	}
}

func TestNewChecksTestCommandsHonourSuitesAndFilter(t *testing.T) {
	all := NewChecks(testConf("6.1", false), ChecksOptions{ProjectDir: "/p", RunTest: true})
	if got := joined(all.Test.Commands); !strings.Contains(got, "php bin/simple-phpunit") {
		t.Errorf("no PHPUnit invocation:\n%s", got)
	}
	if got := joined(all.Test.Commands); strings.Contains(got, "--testsuite") {
		t.Errorf("without suites PHPUnit must run everything in phpunit.xml:\n%s", got)
	}

	filtered := NewChecks(testConf("6.1", false), ChecksOptions{
		ProjectDir: "/p", RunTest: true, Suites: []string{"unit"}, Filter: "CalculatorTest",
	})
	got := joined(filtered.Test.Commands)
	if !strings.Contains(got, "--testsuite unit") {
		t.Errorf("missing --testsuite unit:\n%s", got)
	}
	if !strings.Contains(got, "--filter CalculatorTest") {
		t.Errorf("missing --filter:\n%s", got)
	}
}

func TestChecksPlanInReportModeWritesReportsAndSwallowsFailures(t *testing.T) {
	qa := NewChecks(testConf("6.1", false), ChecksOptions{ProjectDir: "/p", RunQA: true, Report: qatools.ReportGitLab})
	qaCommands := joined(qa.QA.Commands)
	if !strings.Contains(qaCommands, QAReportDir()+"/"+qatools.StatusFile) {
		t.Errorf("the QA step does not record its status:\n%s", qaCommands)
	}
	if !strings.Contains(qaCommands, "exit 0") {
		t.Errorf("the QA step must not fail, or its reports cannot be exported:\n%s", qaCommands)
	}

	test := NewChecks(testConf("6.1", false), ChecksOptions{ProjectDir: "/p", RunTest: true, Report: qatools.ReportGitLab})
	testCommands := joined(test.Test.Commands)
	if !strings.Contains(testCommands, "--log-junit "+TestReportDir()) {
		t.Errorf("PHPUnit does not write a JUnit log:\n%s", testCommands)
	}
	if !strings.Contains(testCommands, TestReportDir()+"/"+qatools.StatusFile) {
		t.Errorf("the test step does not record its status:\n%s", testCommands)
	}
}

func TestDeployPlanWithoutReportIsUnchanged(t *testing.T) {
	p := New(testConf("6.1", false), testStage(), "repo")

	if strings.Contains(joined(p.QA.Commands), "status=") {
		t.Errorf("a deploy plan without --report must keep the fail-fast QA script:\n%s", joined(p.QA.Commands))
	}
	if strings.Contains(joined(p.Test.Commands), "--log-junit") {
		t.Errorf("a deploy plan without --report must not log JUnit:\n%s", joined(p.Test.Commands))
	}
	if p.Source.Kind != SourceGit {
		t.Error("deploy defaults to cloning")
	}
}

func TestNewChecksWithoutADeploySection(t *testing.T) {
	conf := testConf("6.1", false)
	conf.Deploy = nil

	p := NewChecks(conf, ChecksOptions{ProjectDir: "/p", RunQA: true})

	if p == nil || len(p.QA.Commands) == 0 {
		t.Fatal("a project with no deploy section must still be checkable")
	}
	if !p.BuildsAssets() {
		t.Error("with no configuration saying otherwise, assets are built in the pipeline")
	}
}

func TestSourceSubdirIsOnlyAppliedToAClone(t *testing.T) {
	conf := testConf("6.1", false)
	conf.Deploy.SourceDir = "b2b"

	cloned := New(conf, testStage(), "repo")
	if cloned.sourceSubdir() != "b2b" {
		t.Errorf("a clone starts at the repository root, so the application subdirectory must be selected: %q", cloned.sourceSubdir())
	}

	checks := NewChecks(conf, ChecksOptions{ProjectDir: "/repo/b2b", RunQA: true})
	if got := checks.sourceSubdir(); got != "" {
		t.Errorf("a host directory is already the application root, so selecting %q inside it would look for b2b/b2b", got)
	}
}

func TestChecksPlanRunsOneInvocationPerSuite(t *testing.T) {
	p := NewChecks(testConf("6.1", false), ChecksOptions{
		ProjectDir: "/p", RunTest: true, Suites: []string{"unit", "functional"}, Report: qatools.ReportGitLab,
	})

	commands := joined(p.Test.Commands)
	for _, want := range []string{
		"--testsuite unit --log-junit " + TestReportDir() + "/junit-unit.xml",
		"--testsuite functional --log-junit " + TestReportDir() + "/junit-functional.xml",
	} {
		if !strings.Contains(commands, want) {
			t.Errorf("missing %q:\n%s", want, commands)
		}
	}
}
