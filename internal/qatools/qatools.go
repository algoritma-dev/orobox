// Package qatools describes how Orobox installs and runs the QA tool set. Both
// `orobox qa` / `orobox qa-init` and the deploy pipeline consume it, so a clean pipeline
// checkout is analysed exactly the way a developer's environment is.
package qatools

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
)

// Mode selects whether the tools may rewrite the sources.
type Mode int

const (
	// ModeFix lets the tools apply their fixes, which is what a developer wants locally.
	ModeFix Mode = iota
	// ModeCheck only reports violations. The deploy pipeline builds from a git ref inside a
	// container, so a fix there would be discarded and could hide a real violation.
	ModeCheck
)

// Env is the Symfony environment the QA tools boot under. It decides which cache directory
// PHPStan reads, which dumped container has to be warmed there and which database the warmup
// queries, so it has to name an environment that is actually installed.
type Env string

const (
	// EnvTest is what the deploy pipeline uses: it installs Oro in test, the same install the
	// functional tests run against, so a single database serves both steps.
	EnvTest Env = "test"
	// EnvDev is what a developer's stack has: dev exists from the first `orobox install`, while
	// the test database only appears after `orobox test-init`.
	EnvDev Env = "dev"
)

// orDefault keeps the zero value usable: an unset Env means the pipeline's test environment,
// which is what every caller wanted before the environment became selectable.
func (e Env) orDefault() Env {
	if e == "" {
		return EnvTest
	}
	return e
}

// title is the environment as Symfony spells it inside the dumped container's filename
// (<KernelClass><Env>DebugContainer.xml).
func (e Env) title() string {
	name := string(e.orDefault())
	return strings.ToUpper(name[:1]) + name[1:]
}

// Report selects the machine-readable format the tools emit alongside their work. It is separate
// from Mode because the two are independent: a developer fixing locally can still want a report,
// and the pipeline's check-only run is useful without one.
type Report int

const (
	// ReportNone leaves every tool on its human-readable output.
	ReportNone Report = iota
	// ReportGitLab asks each tool for GitLab Code Quality JSON. Every tool in the set speaks it
	// natively — the two JS tools through formatter packages NewInstallPlan installs — so nothing
	// is converted anywhere.
	ReportGitLab
)

// ToolsOptions is everything Tools needs to render the tool set. SourceRoot is where project-local
// configs are looked up, AnalyzePath is the tree PHPStan analyses, Env is the Symfony environment
// PHPStan's cache is warmed in (test when unset), and ReportDir is the directory inside the
// container each tool's report is written to — required when Report is not ReportNone, ignored
// otherwise.
type ToolsOptions struct {
	SourceRoot  string
	AnalyzePath string
	Env         Env
	Mode        Mode
	Report      Report
	ReportDir   string
}

// Tool is a single QA tool invocation.
type Tool struct {
	Name    string
	Args    []string
	WorkDir string // optional override; the caller's working directory is used when empty
	// Setup is an optional shell line run in the same shell right before Args, for state the
	// tool needs but does not create itself.
	Setup string
	// ReportFile is where this tool's machine-readable output ends up. Empty unless a report was
	// requested. It is not part of Args because how the file is produced — a stdout redirect or an
	// environment variable — is a property of the shell line ReportScript builds.
	ReportFile string
	// ReportEnv names the environment variable that tells the tool where to write its report, for
	// the tools that write it themselves rather than to stdout. Empty for the rest.
	ReportEnv string
}

// reportEnvByTool lists the tools whose GitLab formatter writes the document itself, to the file
// named by an environment variable, and keeps the human-readable output on stdout. The four PHP
// tools write the document to stdout instead, so a redirect is all they need.
var reportEnvByTool = map[string]string{
	"eslint":        "ESLINT_CODE_QUALITY_REPORT",
	"stylelint":     "STYLELINT_CODE_QUALITY_REPORT",
	"stylelint-css": "STYLELINT_CODE_QUALITY_REPORT",
}

// BinaryPaths maps tool names to the binaries the tools are installed as.
var BinaryPaths = map[string]string{
	"phpstan":       config.OroRootDir + "/bin/phpstan",
	"rector":        config.OroRootDir + "/bin/rector",
	"php-cs-fixer":  config.OroRootDir + "/bin/php-cs-fixer",
	"twig-cs-fixer": config.OroRootDir + "/bin/twig-cs-fixer",
	"eslint":        config.OroRootDir + "/node_modules/.bin/eslint",
	"stylelint":     config.OroRootDir + "/node_modules/.bin/stylelint",
	"stylelint-css": config.OroRootDir + "/node_modules/.bin/stylelint",
}

// Recursive globs, single-quoted so POSIX sh (no globstar) forwards the raw `**` to the
// tool, which expands it. Matches bundle and project layouts:
// src/<Org>/Bundle/<X>Bundle/Resources/public/... in both install types.
const (
	jsTarget   = "'src/**/Resources/public/**/*.js'"
	scssTarget = "'src/**/Resources/public/**/*.{scss,less,sass,html}'"
	cssTarget  = "'src/**/Resources/public/**/*.css'"
)

// configFallback builds a shell expression that prefers the project's own config file and
// falls back to the one generated in the isolated tools directory.
func configFallback(sourceRoot, fallbackDir, file string) string {
	return fmt.Sprintf("$([ -f %s/%s ] && echo %s/%s || echo %s/%s)", sourceRoot, file, sourceRoot, file, fallbackDir, file)
}

// Tools returns every known tool invocation for the given source root, in run order.
// Callers filter by name.
func Tools(opts ToolsOptions) []Tool {
	sourceRoot := opts.SourceRoot
	analyzePath := opts.AnalyzePath
	mode := opts.Mode
	qaDir := config.QaToolsDir
	oroRoot := config.OroRootDir

	phpstanConfig := configFallback(sourceRoot, qaDir, "phpstan.neon")
	rectorConfig := configFallback(sourceRoot, qaDir, "rector.php")
	phpCSFixerConfig := configFallback(sourceRoot, qaDir, ".php-cs-fixer.dist.php")
	twigCSFixerConfig := configFallback(sourceRoot, qaDir, ".twig-cs-fixer.php")
	eslintConfig := configFallback(sourceRoot, oroRoot, ".eslintrc.yml")
	eslintIgnore := configFallback(sourceRoot, oroRoot, ".eslintignore")
	stylelintConfig := configFallback(sourceRoot, oroRoot, ".stylelintrc.yml")
	stylelintIgnore := configFallback(sourceRoot, oroRoot, ".stylelintignore")
	stylelintCSSConfig := configFallback(sourceRoot, oroRoot, ".stylelintrc-css.yml")
	stylelintCSSIgnore := configFallback(sourceRoot, oroRoot, ".stylelintignore-css")

	rectorArgs := []string{oroRoot + "/bin/rector", "process", "--config=" + rectorConfig}
	phpCSFixerArgs := []string{oroRoot + "/bin/php-cs-fixer", "fix", "--config=" + phpCSFixerConfig}
	// No positional path for twig-cs-fixer: a CLI path would override the config's Finder,
	// which is what discovers bundle templates under src/**/Resources/views.
	twigArgs := []string{oroRoot + "/bin/twig-cs-fixer", "lint", "--config=" + twigCSFixerConfig}
	eslintArgs := []string{
		"npx", "--yes", "eslint",
		"--resolve-plugins-relative-to", qaDir + "/node_modules",
		"--config", eslintConfig, "--ignore-path", eslintIgnore,
		"--quiet", "--no-error-on-unmatched-pattern",
	}
	stylelintArgs := []string{"npx", "--yes", "stylelint", scssTarget, "--config", stylelintConfig, "--ignore-path", stylelintIgnore, "--quiet", "--allow-empty-input"}
	stylelintCSSArgs := []string{"npx", "--yes", "stylelint", cssTarget, "--config", stylelintCSSConfig, "--ignore-path", stylelintCSSIgnore, "--quiet", "--allow-empty-input"}

	switch mode {
	case ModeFix:
		rectorArgs = insertAfter(rectorArgs, 2)
		phpCSFixerArgs = insertAfter(phpCSFixerArgs, 2)
		twigArgs = insertAfter(twigArgs, 2, "--fix")
		eslintArgs = append(eslintArgs, "--fix")
		stylelintArgs = append(stylelintArgs, "--fix")
		stylelintCSSArgs = append(stylelintCSSArgs, "--fix")
	case ModeCheck:
		rectorArgs = insertAfter(rectorArgs, 2, "--dry-run")
		phpCSFixerArgs = insertAfter(phpCSFixerArgs, 2, "--dry-run", "--diff")
	}

	eslintArgs = append(eslintArgs, jsTarget)

	// Each tool's own GitLab reporter, so nothing needs converting anywhere. The flag names differ
	// because their CLIs do; the document they produce is the same CodeClimate subset.
	var phpstanReportArgs []string
	if opts.Report == ReportGitLab {
		phpstanReportArgs = []string{"--error-format=gitlab"}
		rectorArgs = append(rectorArgs, "--output-format=gitlab")
		phpCSFixerArgs = append(phpCSFixerArgs, "--format=gitlab")
		twigArgs = append(twigArgs, "--report=gitlab")
		// The formatter is addressed by absolute path: ESLint 8 resolves a bare --format=gitlab
		// against lib/cli-engine/formatters inside its own installation and dies with
		// "Cannot find module".
		eslintArgs = append(eslintArgs, "--format="+qaDir+"/node_modules/eslint-formatter-gitlab")
		stylelintFormatter := "--custom-formatter=" + qaDir + "/node_modules/stylelint-formatter-gitlab"
		stylelintArgs = append(stylelintArgs, stylelintFormatter)
		stylelintCSSArgs = append(stylelintCSSArgs, stylelintFormatter)
	}

	tools := []Tool{
		{
			Name:  "phpstan",
			Setup: phpstanCacheWarmup(opts.Env),
			// --memory-limit=-1 is not a convenience. PHPStan analyses in parallel worker
			// processes sized from the core count, and a worker that reaches PHP's memory_limit
			// dies and returns a garbled response. That surfaces as `Internal error: Call to a
			// member function set() on int` against whichever file was in flight, so the failure
			// names an innocent file and reproduces only on machines with enough cores to run
			// the workers thin — it passes locally and fails in CI, or the reverse.
			//
			// --no-progress because a CI log has no terminal to redraw: the bar arrives as
			// hundreds of block characters wrapped around the real output.
			Args: append([]string{oroRoot + "/bin/phpstan", "analyze", analyzePath, "--configuration=" + phpstanConfig, "--autoload-file=" + oroRoot + "/vendor/autoload.php", "--memory-limit=-1", "--no-progress"}, phpstanReportArgs...),
		},
		{Name: "rector", Args: rectorArgs, WorkDir: oroRoot},
		{Name: "php-cs-fixer", Args: phpCSFixerArgs},
		{Name: "twig-cs-fixer", Args: twigArgs},
		{Name: "eslint", Args: eslintArgs},
		{Name: "stylelint", Args: stylelintArgs},
		{Name: "stylelint-css", Args: stylelintCSSArgs},
	}

	if opts.Report != ReportNone {
		for i := range tools {
			tools[i].ReportFile = opts.ReportDir + "/" + tools[i].Name + ".json"
			tools[i].ReportEnv = reportEnvByTool[tools[i].Name]
		}
	}

	return tools
}

// insertAfter splits args at index and inserts extra, keeping a fresh backing array so the
// callers' slices never alias.
func insertAfter(args []string, index int, extra ...string) []string {
	out := make([]string, 0, len(args)+len(extra))
	out = append(out, args[:index]...)
	out = append(out, extra...)
	return append(out, args[index:]...)
}

// Script renders a tool list as one shell line, echoing each tool name before running it.
func Script(tools []Tool) string {
	var b strings.Builder
	for i, t := range tools {
		if i > 0 {
			b.WriteString(" && ")
		}
		b.WriteString(fmt.Sprintf("echo '--- Running %s ---' && ", t.Name))
		if t.Setup != "" {
			b.WriteString(t.Setup + " && ")
		}
		cmd := strings.Join(t.Args, " ")
		if t.WorkDir != "" {
			cmd = fmt.Sprintf("(cd %s && %s)", t.WorkDir, cmd)
		}
		b.WriteString(cmd)
	}
	return b.String()
}

// StatusFile is the file, inside the report directory, that ReportScript writes the aggregate
// outcome to: "0" when every tool was clean, "1" when at least one reported violations or failed.
const StatusFile = ".status"

// ReportScript renders a tool list as one shell line that runs every tool, sends each one's report
// to its own file, and never fails.
//
// Two things differ from Script, and both are forced by what a report is for.
//
// The tools are not chained with &&: a Code Quality report listing only PHPStan's findings because
// Rector never ran is worse than no report at all, so every tool runs whatever the previous one
// concluded. Every tool in the set exits non-zero on findings, so with && the first one with
// something to say would silence all the others.
//
// The script always exits 0. A failed WithExec makes a Dagger container unreadable, and finding
// violations is precisely the case where the report has to be extracted — so the outcome travels
// in StatusFile instead of in the exit code, and the caller exports the reports first and decides
// pass or fail afterwards.
//
// Setup lines run undirected because their output is not JSON — PHPStan's cache warmup prints a
// Symfony console banner that would corrupt the file — and stderr is left alone throughout so
// warnings still reach the step log.
func ReportScript(tools []Tool, reportDir string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "mkdir -p %s\nstatus=0\n", reportDir)

	for _, t := range tools {
		fmt.Fprintf(&b, "echo '--- Running %s ---'\n", t.Name)

		cmd := strings.Join(t.Args, " ")
		if t.WorkDir != "" {
			cmd = fmt.Sprintf("(cd %s && %s)", t.WorkDir, cmd)
		}

		// A tool with ReportEnv writes the document itself and prints its usual summary to stdout,
		// so redirecting would both capture the wrong bytes and hide output worth reading.
		run := fmt.Sprintf("%s > %s || status=1", cmd, t.ReportFile)
		if t.ReportEnv != "" {
			run = fmt.Sprintf("%s=%s %s || status=1", t.ReportEnv, t.ReportFile, cmd)
		}

		if t.Setup != "" {
			// A failed setup is a failed tool: PHPStan cannot analyse without its warmed cache, and
			// running it anyway would write a report full of bootstrap errors.
			fmt.Fprintf(&b, "if %s; then %s; else status=1; fi\n", t.Setup, run)
			continue
		}
		fmt.Fprintf(&b, "%s\n", run)
	}

	fmt.Fprintf(&b, "printf '%%s' \"$status\" > %s/%s\n", reportDir, StatusFile)
	b.WriteString("exit 0")

	return b.String()
}

// InstallPlan is everything needed to populate the isolated QA tools directory.
type InstallPlan struct {
	NeedsComposerTools bool
	NeedsJSTools       bool
	NeedsPhpstan       bool
	NeedsTwigCS        bool
	ComposerPackages   []string
	JSManager          string
	JSInstallArg       string
	JSSaveDevFlag      string
	JSPackages         []string
}

// NewInstallPlan resolves which packages to install for the enabled tools. `oroVersion`
// drives both the Symfony pins for the PHP tools and the JS package manager.
func NewInstallPlan(oroVersion string) InstallPlan {
	needsPhpCodingStandards := config.IsQaToolEnabled("phpstan") || config.IsQaToolEnabled("rector") || config.IsQaToolEnabled("php-cs-fixer")
	plan := InstallPlan{
		NeedsPhpstan: config.IsQaToolEnabled("phpstan"),
		NeedsTwigCS:  config.IsQaToolEnabled("twig-cs-fixer"),
	}
	needsEslint := config.IsQaToolEnabled("eslint")
	needsStylelint := config.IsQaToolEnabled("stylelint")

	plan.NeedsComposerTools = needsPhpCodingStandards || plan.NeedsTwigCS
	plan.NeedsJSTools = needsEslint || needsStylelint

	if needsPhpCodingStandards {
		plan.ComposerPackages = append(plan.ComposerPackages,
			"phpstan/phpstan-symfony", "phpstan/phpstan-phpunit", "phpstan/phpstan-doctrine", "algoritma/php-coding-standards:*")
		// Pin the tools' Symfony components to Oro's line so PHPStan can co-load Oro's
		// classes with them without fatal signature mismatches.
		plan.ComposerPackages = append(plan.ComposerPackages, config.GetQaSymfonyConstraints(oroVersion)...)
	}
	if plan.NeedsTwigCS {
		plan.ComposerPackages = append(plan.ComposerPackages, "vincentlanglet/twig-cs-fixer:*")
	}

	versions := config.GetVersionsForOro(oroVersion)
	plan.JSManager, plan.JSInstallArg, plan.JSSaveDevFlag = "npm", "install", "--save-dev"
	if versions.PNPM != "" {
		plan.JSManager, plan.JSInstallArg, plan.JSSaveDevFlag = "pnpm", "add", "-D"
	}

	// The GitLab formatters ship separately from the linters and are installed unconditionally:
	// requiring them only when a report is asked for would make --report=gitlab fail at runtime on
	// every environment initialised before the flag existed.
	//
	// The ^5.1.0 pin is not cosmetic. eslint-formatter-gitlab 6 and later declare a peer dependency
	// on eslint >= 9 while this list pins eslint to ^8.57.0, so the unpinned package makes the whole
	// npm install fail with ERESOLVE — taking the other JS tools down with it.
	if needsEslint {
		plan.JSPackages = append(plan.JSPackages, "eslint@^8.57.0", "eslint-plugin-no-jquery", "eslint-plugin-import", "eslint-formatter-gitlab@^5.1.0")
	}
	if needsStylelint {
		plan.JSPackages = append(plan.JSPackages, "stylelint@^15.11.0", "@oroinc/oro-stylelint-config", "stylelint-formatter-gitlab")
	}

	return plan
}

// ComposerInstallCommand returns the shell line that populates the QA tools directory.
//
// A project that commits vendor-bin/qa/composer.json (and its lock) has pinned the tool
// versions on purpose, so re-requiring the packages there would silently undo the pin:
// `require pkg:*` rewrites the constraint to the newest release. That directory is
// installed as-is instead, which is also what reproduces the developer's versions in the
// pipeline. Only a directory Orobox itself has just scaffolded — no require section — gets
// the packages required into it.
//
// The check is `php -r` rather than a grep because the QA composer.json is committed and can
// legitimately carry a `config` or `extra` section without any requirement.
func ComposerInstallCommand(packages []string) string {
	manifest := config.QaToolsDir + "/composer.json"
	hasRequirements := fmt.Sprintf(
		`php -r '$m = json_decode(@file_get_contents(%q), true) ?: []; exit(($m["require"] ?? []) || ($m["require-dev"] ?? []) ? 0 : 1);'`,
		manifest)

	// `yes` is wrapped in `|| true` because the steps run under `bash -o pipefail`: as soon as
	// composer stops reading, `yes` dies of SIGPIPE with 141 and the whole command would be
	// reported as failed even though the install succeeded. -W lets the Symfony pins downgrade
	// transitively-locked packages on re-runs.
	require := "(yes y || true) | composer bin qa require --dev -W --no-interaction " + strings.Join(packages, " ")

	return fmt.Sprintf("if %s; then composer bin qa install --no-interaction --no-progress; else %s; fi",
		hasRequirements, require)
}

// oroKernelClass is the OroCommerce application kernel. The Symfony container is dumped to
// <KernelClass><Env>DebugContainer.xml, so this also drives the containerXmlPath filename
// PHPStan expects.
const oroKernelClass = "AppKernel"

// CacheDir is the application cache directory PHPStan's Oro bootstrap reads from, for the given
// environment. The pipeline analyses against test — the environment the functional tests install,
// so a single installed database serves both steps — while a developer's stack analyses against
// dev, which is the environment their `orobox install` left behind. The deploy pipeline persists
// the directory between runs, so it is exported rather than inlined.
func CacheDir(env Env) string {
	return CacheVolumeDir() + "/" + string(env.orDefault())
}

// CacheVolumeDir is the directory a pipeline mounts the persistent QA cache volume on. It is
// var/cache rather than CacheDir itself because oro:install finishes with a cache:clear, and
// clearing the test cache removes the var/cache/test directory: on a mount point that rmdir
// fails with "Failed to remove directory ...: Resource busy" and takes the install down with it.
// One level up, the directory Symfony deletes is an ordinary directory inside the volume.
func CacheVolumeDir() string {
	return config.OroRootDir + "/var/cache"
}

// ContainerXMLPath and SymfonyConfigDir are the two debug cache artifacts PHPStan's Oro
// bootstrap requires; the same values are written into phpstan.neon by PhpstanConfigScript.
func ContainerXMLPath(env Env) string {
	return fmt.Sprintf("%s/%s%sDebugContainer.xml", CacheDir(env), oroKernelClass, env.title())
}

// SymfonyConfigDir is the dumped Symfony config directory PHPStan's Oro bootstrap requires.
func SymfonyConfigDir(env Env) string {
	return CacheDir(env) + "/Symfony/Config"
}

// phpstanCacheWarmup warms env's debug cache before PHPStan runs. The bootstrap shipped by
// algoritma/php-coding-standards aborts when the dumped container is missing, which is always
// the case on a clean pipeline checkout and after `orobox clean` locally. Warming is skipped
// when both artifacts are already there, so a normal local run pays nothing.
//
// Debug is forced on regardless of the ORO_DEBUG the QA step runs under, because the dumped
// container PHPStan reads only exists in a debug cache. Warming queries Oro's config tables, so
// it needs env's database installed: in the pipeline that is the QA step's own oro:install
// --env=test, locally it is the dev install `orobox install` already performed.
func phpstanCacheWarmup(env Env) string {
	return fmt.Sprintf("{ [ -f %s ] && [ -d %s ]; } || ORO_DEBUG=1 php %s/bin/console cache:warmup --env=%s",
		ContainerXMLPath(env), SymfonyConfigDir(env), config.OroRootDir, env.orDefault())
}

// consoleApplicationLoaderPHP boots the real Oro kernel so phpstan-symfony can enumerate
// console commands. It uses OroRoot's autoloader (not the isolated QA one, which lacks the
// application classes). It boots env to match the containerXmlPath below: that is the cache the
// QA step warms, so the loader and the dumped container have to agree.
func consoleApplicationLoaderPHP(env Env) string {
	return `<?php

declare(strict_types=1);

use Symfony\Bundle\FrameworkBundle\Console\Application;

require '` + config.OroRootDir + `/vendor/autoload.php';
require_once '` + config.OroRootDir + `/src/` + oroKernelClass + `.php';

$kernel = new ` + oroKernelClass + `('` + string(env.orDefault()) + `', true);

return new Application($kernel);
`
}

// objectManagerLoaderPHP returns Oro's Doctrine ObjectManager for phpstan-doctrine,
// replacing the throwing stub the plugin generates. Boots env for the same reason as the
// console loader above.
func objectManagerLoaderPHP(env Env) string {
	return `<?php

declare(strict_types=1);

require '` + config.OroRootDir + `/vendor/autoload.php';
require_once '` + config.OroRootDir + `/src/` + oroKernelClass + `.php';

$kernel = new ` + oroKernelClass + `('` + string(env.orDefault()) + `', true);
$kernel->boot();

return $kernel->getContainer()->get('doctrine')->getManager();
`
}

// twigCsFixerConfigPHP is the default config for vincentlanglet/twig-cs-fixer. new Config()
// already applies the bundled TwigCsFixer standard ruleset, so this is a minimal,
// override-friendly starting point.
const twigCsFixerConfigPHP = `<?php

$ruleset = new TwigCsFixer\Ruleset\Ruleset();
$ruleset->addStandard(new TwigCsFixer\Standard\TwigCsFixer());

$finder = new TwigCsFixer\File\Finder();
$finder->in(['` + config.OroRootDir + `/templates', '` + config.OroRootDir + `/src']);
$finder->exclude(['` + config.OroRootDir + `/vendor', '` + config.OroRootDir + `/vendor-bin']);

$config = new TwigCsFixer\Config\Config();
$config->setRuleset($ruleset);
$config->allowNonFixableRules();
$config->setFinder($finder);

return $config;
`

// TwigConfigScript returns a shell script that drops the default .twig-cs-fixer.php into the
// QA tools directory when none exists. It never overwrites, keeping re-runs and manual edits
// safe.
func TwigConfigScript() string {
	cfg := config.QaToolsDir + "/.twig-cs-fixer.php"
	b64 := base64.StdEncoding.EncodeToString([]byte(twigCsFixerConfigPHP))
	return fmt.Sprintf("[ -f %[1]s ] || printf '%%s' '%[2]s' | base64 -d > %[1]s", cfg, b64)
}

// PhpstanConfigScript returns a shell script that rewrites the generated
// vendor-bin/qa/phpstan.neon so its paths resolve from OroRoot instead of the isolated QA
// directory, and installs Oro-aware bootstrap loaders.
//
// The algoritma/php-coding-standards plugin generates phpstan.neon assuming it lives at the
// application root, writing paths relative to it (and hardcoding Symfony's App_Kernel
// container filename). Because bamarni places the config in the isolated QA directory,
// PHPStan resolves those paths against it and fails ("Scanned file ... does not exist").
func PhpstanConfigScript(env Env) string {
	oro := config.OroRootDir
	qa := config.QaToolsDir
	neon := qa + "/phpstan.neon"

	consoleAbs := qa + "/tests/console-application.php"
	objAbs := qa + "/tests/object-manager.php"
	xmlAbs := ContainerXMLPath(env)
	scanDirAbs := SymfonyConfigDir(env)
	scanFileAbs := oro + "/vendor/symfony/dependency-injection/Loader/Configurator/ContainerConfigurator.php"

	b64Console := base64.StdEncoding.EncodeToString([]byte(consoleApplicationLoaderPHP(env)))
	b64Obj := base64.StdEncoding.EncodeToString([]byte(objectManagerLoaderPHP(env)))

	// Anchoring each replacement to its key/list-marker keeps this idempotent: once a value
	// is absolute it no longer matches the relative pattern on a re-run.
	//
	// The two cache paths match whatever value is there rather than the generated one, because
	// they carry the environment: a config written by a local run (dev) that is then re-run in
	// CI (test) has to be corrected, and an exact pattern would leave the stale path in place.
	return fmt.Sprintf(`set -e
[ -f %[1]s ] || exit 0
mkdir -p %[2]s/tests
sed -i \
 -e 's#consoleApplicationLoader: tests/console-application.php#consoleApplicationLoader: %[3]s#' \
 -e 's#objectManagerLoader: tests/object-manager.php#objectManagerLoader: %[4]s#' \
 -e 's#containerXmlPath: .*#containerXmlPath: %[5]s#' \
 -e 's#- .*var/cache/[^ ]*/Symfony/Config#- %[6]s#' \
 -e 's#- vendor/symfony/dependency-injection/Loader/Configurator/ContainerConfigurator.php#- %[7]s#' \
 %[1]s
printf '%%s' '%[8]s' | base64 -d > %[3]s
printf '%%s' '%[9]s' | base64 -d > %[4]s`,
		neon, qa, consoleAbs, objAbs, xmlAbs, scanDirAbs, scanFileAbs, b64Console, b64Obj)
}
