package pipeline

import (
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

	for _, want := range []string{"composer install", "--no-dev", "--optimize-autoloader", "--classmap-authoritative"} {
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

func TestPlanQAStageIsCheckOnlyAndSelfInstalling(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	p := New(testConf("6.1", true), testStage(), "repo")
	qa := joined(p.QA.Commands)

	// A clean clone has no vendor-bin/qa, so the stage must install the tools itself.
	for _, want := range []string{"composer bin qa require", "allow-plugins.bamarni/composer-bin-plugin", "phpstan.neon"} {
		if !strings.Contains(qa, want) {
			t.Errorf("qa stage missing bootstrap step %q", want)
		}
	}

	// Every command runs under `bash -o pipefail`, where a bare `yes y |` turns the successful
	// install into exit code 141 as soon as composer stops reading its stdin.
	if strings.Contains(qa, "yes y | ") {
		t.Errorf("the QA install pipes `yes` without swallowing its SIGPIPE status: %s", qa)
	}

	// Nothing may mutate the sources.
	if strings.Contains(qa, "--fix") {
		t.Errorf("qa stage runs a tool with --fix: %s", qa)
	}
	for _, want := range []string{"--dry-run"} {
		if !strings.Contains(qa, want) {
			t.Errorf("qa stage is missing %q, so it is not check-only", want)
		}
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

	// oro:install accepts neither --skip-assets nor --timeout on any supported version.
	for _, forbidden := range []string{"--skip-assets", "--timeout"} {
		if strings.Contains(qa, forbidden) {
			t.Errorf("qa oro:install must not receive %q: %s", forbidden, qa)
		}
	}

	// The install has to precede the analysis, and the dev dependencies the test environment
	// needs have to precede the install.
	composer := strings.Index(qa, "composer install")
	install := strings.Index(qa, "oro:install")
	phpstan := strings.Index(qa, "bin/phpstan")
	if composer == -1 || install == -1 || phpstan == -1 || composer > install || install > phpstan {
		t.Errorf("qa stage order is composer=%d install=%d phpstan=%d: %s", composer, install, phpstan, qa)
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

func TestPlanTestStageSuites(t *testing.T) {
	unitOnly := New(testConf("6.1", true), testStage(), "repo")
	unit := joined(unitOnly.Test.Commands)
	if strings.Contains(unit, "oro:install") {
		t.Errorf("unit-only stage must not install the application: %s", unit)
	}
	if !strings.Contains(unit, "bin/simple-phpunit --testsuite unit") {
		t.Errorf("unit suite missing: %s", unit)
	}
	// PHPUnit itself is a require-dev package, absent from the --no-dev vendor tree the step
	// inherits, so the dev dependencies have to be installed before any suite runs.
	install := strings.Index(unit, "composer install")
	phpunit := strings.Index(unit, "bin/simple-phpunit")
	if install == -1 || install > phpunit {
		t.Errorf("test stage must install the dev dependencies before running PHPUnit: %s", unit)
	}
	if strings.Contains(unit, "composer install --no-dev") {
		t.Errorf("test stage must not install without dev dependencies: %s", unit)
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

	// oro:install accepts neither --skip-assets nor --timeout on any supported version.
	for _, forbidden := range []string{"--skip-assets", "--timeout"} {
		if strings.Contains(both, forbidden) {
			t.Errorf("oro:install must not receive %q: %s", forbidden, both)
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
