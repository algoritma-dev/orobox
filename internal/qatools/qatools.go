// Package qatools describes how Orobox installs and runs the QA tool set. Both
// `orobox qa` / `orobox qa-init` and the deploy pipeline consume it, so a clean pipeline
// checkout is analysed exactly the way a developer's environment is.
package qatools

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
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

// Tools returns every known tool invocation for the given source root, in run order.
// Callers filter by name.
func Tools(opts ToolsOptions) []Tool {
	sourceRoot := opts.SourceRoot
	analyzePath := opts.AnalyzePath
	mode := opts.Mode
	qaDir := config.QaToolsDir
	oroRoot := config.OroRootDir

	// A project's own config layers on top of the base one rather than replacing it; see
	// mergedConfig. The ignore files are the exception, and the one there explains why.
	phpstanConfig := mergedConfig(sourceRoot, qaDir, "phpstan.neon", neonMerge)
	rectorConfig := mergedConfig(sourceRoot, qaDir, "rector.php", rectorMerge)
	phpCSFixerConfig := mergedConfig(sourceRoot, qaDir, ".php-cs-fixer.dist.php", phpCSFixerMerge)
	twigCSFixerConfig := mergedConfig(sourceRoot, qaDir, ".twig-cs-fixer.php", twigCSFixerMerge)
	eslintConfig := mergedConfig(sourceRoot, oroRoot, ".eslintrc.yml", yamlExtendsMerge)
	eslintIgnore := mergedConfig(sourceRoot, oroRoot, ".eslintignore", nil)
	stylelintConfig := mergedConfig(sourceRoot, oroRoot, ".stylelintrc.yml", yamlExtendsMerge)
	stylelintIgnore := mergedConfig(sourceRoot, oroRoot, ".stylelintignore", nil)
	stylelintCSSConfig := mergedConfig(sourceRoot, oroRoot, ".stylelintrc-css.yml", yamlExtendsMerge)
	stylelintCSSIgnore := mergedConfig(sourceRoot, oroRoot, ".stylelintignore-css", nil)

	rectorArgs := []string{oroRoot + "/bin/rector", "process", "--config=" + rectorConfig.Path}
	phpCSFixerArgs := []string{oroRoot + "/bin/php-cs-fixer", "fix", "--config=" + phpCSFixerConfig.Path}
	// No positional path for twig-cs-fixer: a CLI path would override the config's Finder,
	// which is what discovers bundle templates under src/**/Resources/views.
	twigArgs := []string{oroRoot + "/bin/twig-cs-fixer", "lint", "--config=" + twigCSFixerConfig.Path}
	eslintArgs := []string{
		"npx", "--yes", "eslint",
		"--resolve-plugins-relative-to", qaDir + "/node_modules",
		"--config", eslintConfig.Path, "--ignore-path", eslintIgnore.Path,
		"--quiet", "--no-error-on-unmatched-pattern",
	}
	stylelintArgs := []string{"npx", "--yes", "stylelint", scssTarget, "--config", stylelintConfig.Path, "--ignore-path", stylelintIgnore.Path, "--quiet", "--allow-empty-input"}
	stylelintCSSArgs := []string{"npx", "--yes", "stylelint", cssTarget, "--config", stylelintCSSConfig.Path, "--ignore-path", stylelintCSSIgnore.Path, "--quiet", "--allow-empty-input"}

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
			Name: "phpstan",
			// The merged config is written before the cache is warmed: a failed write must stop
			// the tool, and the warmup is the more expensive of the two.
			Setup: joinSetup(phpstanConfig.Setup, phpstanCacheWarmup(opts.Env)),
			// --memory-limit=-1 is not a convenience. PHPStan analyses in parallel worker
			// processes sized from the core count, and a worker that reaches PHP's memory_limit
			// dies and returns a garbled response. That surfaces as `Internal error: Call to a
			// member function set() on int` against whichever file was in flight, so the failure
			// names an innocent file and reproduces only on machines with enough cores to run
			// the workers thin — it passes locally and fails in CI, or the reverse.
			//
			// --no-progress because a CI log has no terminal to redraw: the bar arrives as
			// hundreds of block characters wrapped around the real output.
			// The environment variable flips the shared-autoload bootstrap to put the
			// application's tree first; see SharedAutoloadPrependEnv. It reaches the parallel
			// workers because they are child processes of this one, and they are where the
			// kernel is actually booted.
			Args: append([]string{SharedAutoloadPrependEnv + "=1", oroRoot + "/bin/phpstan", "analyze", analyzePath, "--configuration=" + phpstanConfig.Path, "--autoload-file=" + oroRoot + "/vendor/autoload.php", "--memory-limit=-1", "--no-progress"}, phpstanReportArgs...),
		},
		{Name: "rector", Args: rectorArgs, WorkDir: oroRoot, Setup: rectorConfig.Setup},
		{Name: "php-cs-fixer", Args: phpCSFixerArgs, Setup: phpCSFixerConfig.Setup},
		{Name: "twig-cs-fixer", Args: twigArgs, Setup: twigCSFixerConfig.Setup},
		{Name: "eslint", Args: eslintArgs, Setup: eslintConfig.Setup},
		{Name: "stylelint", Args: stylelintArgs, Setup: stylelintConfig.Setup},
		{Name: "stylelint-css", Args: stylelintCSSArgs, Setup: stylelintCSSConfig.Setup},
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
// versions on purpose, so re-requiring a package already declared there would silently undo
// the pin: `require pkg:*` rewrites the constraint to the newest release. Declared packages
// are therefore installed as-is, which is also what reproduces the developer's versions in
// the pipeline.
//
// Only the packages the manifest does not declare yet are required into it. Branching on
// "the manifest has any requirement at all" instead would strand every tool enabled after
// the first initialization: the manifest scaffolded for PHPStan already has a require-dev
// section, so a later `twig_cs_fixer: true` would take the install branch, install nothing
// new, and leave `orobox qa` warning that the binary is missing on every run.
//
// The check is `php -r` rather than a grep because the QA composer.json is committed and can
// legitimately carry a `config` or `extra` section without any requirement.
func ComposerInstallCommand(packages []string) string {
	manifest := config.QaToolsDir + "/composer.json"

	// Package names are compared lowercased, the way composer normalizes them, and stripped of
	// their ":constraint" suffix so a manifest pinning "^3.0" still counts as declaring it.
	//
	// `replace` counts as declared too, and that is not a detail: SharedVendorScript moves the
	// shared packages from the requirements into `replace`, so without it every run would see
	// them as undeclared and require them straight back in — reinstalling the duplicate copy the
	// patch exists to remove.
	missing := fmt.Sprintf(
		`php -r '$m = json_decode(@file_get_contents(%q), true) ?: []; `+
			`$have = array_change_key_case(($m["require"] ?? []) + ($m["require-dev"] ?? []) + ($m["replace"] ?? [])); `+
			`$out = []; foreach (array_filter(explode(" ", %q)) as $p) { `+
			`if (!isset($have[strtolower(explode(":", $p)[0])])) { $out[] = $p; } } `+
			`echo implode(" ", $out);'`,
		manifest, strings.Join(packages, " "))

	// `yes` is wrapped in `|| true` because the steps run under `bash -o pipefail`: as soon as
	// composer stops reading, `yes` dies of SIGPIPE with 141 and the whole command would be
	// reported as failed even though the install succeeded. -W lets the Symfony pins downgrade
	// transitively-locked packages on re-runs.
	//
	// $MISSING is deliberately unquoted: it is a space-separated package list composer must see
	// as separate arguments. `require` installs the manifest's other packages along the way, so
	// the install branch is only for the case where nothing is missing.
	//
	// The manifest patch written by SharedVendorScript takes packages out of the QA
	// requirements, which leaves any committed lock file stale: `install` would then abort with
	// "the lock file is not up to date". The patch records that it changed something, and this
	// is the one run that re-resolves — afterwards the marker is gone and installs go back to
	// being lock-driven. The require branch subsumes it, because requiring re-resolves anyway.
	marker := config.QaToolsDir + "/" + ManifestDirtyFile

	// The marker is cleared by the resolution that consumed it, not afterwards: a failed
	// re-resolution has to stay marked so the next run retries it, and clearing it
	// unconditionally would also swallow the exit status the step is judged on.
	return fmt.Sprintf(
		`MISSING="$(%[1]s)"; if [ -n "$MISSING" ]; then (yes y || true) | composer bin qa require --dev -W --no-interaction $MISSING && rm -f %[2]s; `+
			`elif [ -f %[2]s ]; then composer bin qa update -W --no-interaction --no-progress && rm -f %[2]s; `+
			`else composer bin qa install --no-interaction --no-progress; fi`,
		missing, marker)
}

// ManifestDirtyFile is written into the QA namespace by SharedVendorScript when it changed the
// manifest, so ComposerInstallCommand knows the committed lock no longer matches and has to
// re-resolve once instead of failing on it.
const ManifestDirtyFile = ".orobox-manifest-dirty"

// SharedAutoloadRelPath is where SharedVendorScript writes the bootstrap that puts the
// application's autoloader behind the QA one. It is inside the QA namespace, under orobox/, the
// same convention the deploy recipe follows, and it is registered through the manifest's
// `autoload.files` so every tool gets it — not only PHPStan, which is the only one that takes
// an --autoload-file.
const SharedAutoloadRelPath = "orobox/oro-autoload.php"

// SharedAutoloadPrependEnv makes the bootstrap register the application's autoloader *first*
// instead of last. It is set for PHPStan and for nothing else, because PHPStan is the only tool
// that boots the application kernel: the dumped debug container inline-requires vendor files by
// absolute path, so every class the container touches has to come from the application's tree or
// the second copy fatals with "Cannot redeclare".
//
// The other tools never load that container. They keep the QA tree in front, which is what lets
// them use dependency versions the application's tree does not have — php-cs-fixer's
// sebastian/diff and PHPStan's phpdoc-parser are both several majors ahead of Oro's copies.
const SharedAutoloadPrependEnv = "OROBOX_QA_AUTOLOAD_PREPEND"

// sharedAutoloadPHP registers the application's autoloader alongside the QA one, in the order
// SharedAutoloadPrependEnv asks for.
//
// Composer's generated autoload.php always prepends its loader, so the order is taken back:
// unregistered, then re-registered where it belongs. Appended (the default) keeps the isolation
// the QA namespace exists for — the tools load their own dependencies from their own tree, and
// the application's tree only serves what the QA tree does not have. Prepended is the opposite
// trade, and only PHPStan needs it.
//
// It runs from the manifest's `autoload.files`, which is the earliest hook there is: Composer
// includes it while building the loader, before any tool code can autoload a shared class and
// pin the process to the wrong copy.
const sharedAutoloadPHP = `<?php

declare(strict_types=1);

/**
 * Written by orobox. Do not edit: every ` + "`orobox qa-init`" + ` rewrites it.
 *
 * The QA tools live in an isolated Composer tree, the application in its own. A package
 * installed in both can be compiled twice in one PHP process: the dumped Symfony debug
 * container inline-requires vendor files by absolute path, and include_once dedupes on the
 * path, not on the class name — so a second copy under another path fatals with
 * "Cannot redeclare". The shared packages are therefore installed once, in the application's
 * tree, and reached from here.
 */

$autoload = '` + config.OroRootDir + `/vendor/autoload.php';

// The QA namespace is populated before the application's vendor tree exists in the deploy
// pipeline's layer cache, and the tools have to keep working there.
if (!is_file($autoload)) {
    return;
}

$loader = require $autoload;

if ($loader instanceof \Composer\Autoload\ClassLoader) {
    spl_autoload_unregister([$loader, 'loadClass']);
    $loader->register(getenv('` + SharedAutoloadPrependEnv + `') === '1');
}
`

// manifestPatchPHP rewrites the QA manifest so the packages the application already ships are
// not installed a second time.
//
// Pinning them to the application's Symfony line — which is what GetQaSymfonyConstraints does —
// is not enough: two *identical* copies still fatal, because the dumped debug container
// inline-requires vendor files by path. Only one copy may exist, so each shared package the
// application actually ships is declared in `replace` (which tells Composer not to install it)
// and dropped from the QA requirements.
//
// The version comes from the application's installed.json rather than from a constant, because
// that is the copy the tools will really load. A package the application does not ship is left
// alone, keeping its pinned requirement.
func manifestPatchPHP() string {
	var names strings.Builder
	for i, name := range config.QaSharedPackages() {
		if i > 0 {
			names.WriteString(", ")
		}
		fmt.Fprintf(&names, "'%s'", name)
	}

	return fmt.Sprintf(`<?php

declare(strict_types=1);

$manifestPath = '%[1]s/composer.json';
$installedPath = '%[2]s/vendor/composer/installed.json';
$markerPath = '%[1]s/%[3]s';
$bootstrap = '%[4]s';
$shared = [%[5]s];

$manifest = json_decode((string) @file_get_contents($manifestPath), true);
if (!is_array($manifest)) {
    fwrite(STDERR, sprintf("orobox: %%s is not readable JSON\n", $manifestPath));
    exit(1);
}
$before = json_encode($manifest);

$provided = [];
$installed = json_decode((string) @file_get_contents($installedPath), true);
foreach ((is_array($installed) ? ($installed['packages'] ?? $installed) : []) as $package) {
    if (isset($package['name'], $package['version'])) {
        $provided[strtolower((string) $package['name'])] = ltrim((string) $package['version'], 'v');
    }
}

foreach ($shared as $name) {
    $version = $provided[strtolower($name)] ?? null;
    if ($version === null) {
        // Not in the application's tree: the QA namespace is the only place it can come from,
        // so it keeps its pinned requirement.
        continue;
    }

    $manifest['replace'][$name] = $version;

    foreach (['require', 'require-dev'] as $section) {
        unset($manifest[$section][$name]);
        if (isset($manifest[$section]) && [] === $manifest[$section]) {
            unset($manifest[$section]);
        }
    }
}

$files = $manifest['autoload']['files'] ?? [];
if (!in_array($bootstrap, $files, true)) {
    $files[] = $bootstrap;
}
$manifest['autoload']['files'] = array_values($files);

if (json_encode($manifest) === $before) {
    exit(0);
}

file_put_contents($manifestPath, json_encode($manifest, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES) . "\n");
file_put_contents($markerPath, '');
`, config.QaToolsDir, config.OroRootDir, ManifestDirtyFile, SharedAutoloadRelPath, names.String())
}

// SharedVendorScript prepares the QA namespace so the tools and the application share one copy
// of every package both trees need. It creates the namespace and its minimal manifest when they
// are missing, writes the autoload bootstrap, and patches the manifest.
//
// It has to run before Composer populates the namespace: a `replace` entry added afterwards
// would leave the duplicate copy on disk until the next resolution.
//
// The patch runs from a file rather than `php -r` because it is long enough that shell quoting
// would be the only thing likely to break it; /tmp keeps it out of the committed namespace.
func SharedVendorScript() string {
	qa := config.QaToolsDir
	patchPath := "/tmp/orobox-qa-manifest.php"

	return fmt.Sprintf(`set -e
mkdir -p %[1]s/%[2]s
[ -f %[1]s/composer.json ] || printf '{"name":"orobox/qa-tools"}' > %[1]s/composer.json
printf '%%s' '%[3]s' | base64 -d > %[1]s/%[4]s
printf '%%s' '%[5]s' | base64 -d > %[6]s
php %[6]s`,
		qa,
		filepath.Dir(SharedAutoloadRelPath),
		base64.StdEncoding.EncodeToString([]byte(sharedAutoloadPHP)),
		SharedAutoloadRelPath,
		base64.StdEncoding.EncodeToString([]byte(manifestPatchPHP())),
		patchPath)
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
// as soon as the dumped container is there, so a normal local run pays nothing.
//
// Only the container is checked. The other artifact PHPStan reads, the dumped Symfony config
// directory, is written for the framework bundles that ship one, so an install can legitimately
// end with a container and no Symfony/Config directory. Gating on both then warms the cache on
// every single run, which is the slowest step in the QA set.
//
// Debug is forced on regardless of the ORO_DEBUG the QA step runs under, because the dumped
// container PHPStan reads only exists in a debug cache. Warming queries Oro's config tables, so
// it needs env's database installed: in the pipeline that is the QA step's own oro:install
// --env=test, locally it is the dev install `orobox install` already performed.
func phpstanCacheWarmup(env Env) string {
	return fmt.Sprintf("[ -f %s ] || ORO_DEBUG=1 php %s/bin/console cache:warmup --env=%s",
		ContainerXMLPath(env), config.OroRootDir, env.orDefault())
}

// kernelBootstrapPHP is the preamble both PHPStan kernel loaders share: the application's
// autoloader, its environment, and its kernel class.
//
// The environment is the part that is easy to miss. `bin/console` never instantiates the kernel
// itself — it hands over to symfony/runtime, which boots the dotenv files first, so every
// %env(...)% placeholder in the application's configuration resolves. A loader that instantiates
// the kernel directly skips that, and the placeholders silently fall back to whatever default the
// configuration declares: Doctrine then connects to the default host instead of the database
// service and PHPStan reports "connection to server at 127.0.0.1 ... refused" against whichever
// file it happened to be analysing.
//
// Which file to boot is not guessable either — Oro renames it through composer.json's
// extra.runtime.dotenv_path (.env-app), and the environment and debug variables with it — so the
// runtime configuration is read from there, exactly like symfony/runtime does, with the framework
// defaults when a project declares none.
func kernelBootstrapPHP() string {
	oro := config.OroRootDir

	return `<?php

declare(strict_types=1);

require '` + oro + `/vendor/autoload.php';
require_once '` + oro + `/src/` + oroKernelClass + `.php';

$runtime = [];
$composer = json_decode((string) @file_get_contents('` + oro + `/composer.json'), true);
if (is_array($composer)) {
    $runtime = $composer['extra']['runtime'] ?? [];
}

$dotenvPath = '` + oro + `/' . ($runtime['dotenv_path'] ?? '.env');

if (is_file($dotenvPath) && class_exists(\Symfony\Component\Dotenv\Dotenv::class)) {
    // Existing variables win: the QA step runs inside the application container, whose own
    // environment is the more specific one.
    (new \Symfony\Component\Dotenv\Dotenv(
        $runtime['env_var_name'] ?? 'APP_ENV',
        $runtime['debug_var_name'] ?? 'APP_DEBUG',
    ))->bootEnv($dotenvPath);
}
`
}

// consoleApplicationLoaderPHP boots the real Oro kernel so phpstan-symfony can enumerate
// console commands. It uses OroRoot's autoloader (not the isolated QA one, which lacks the
// application classes). It boots env to match the containerXmlPath below: that is the cache the
// QA step warms, so the loader and the dumped container have to agree.
func consoleApplicationLoaderPHP(env Env) string {
	return kernelBootstrapPHP() + `
$kernel = new ` + oroKernelClass + `('` + string(env.orDefault()) + `', true);

return new \Symfony\Bundle\FrameworkBundle\Console\Application($kernel);
`
}

// objectManagerLoaderPHP returns Oro's Doctrine ObjectManager for phpstan-doctrine,
// replacing the throwing stub the plugin generates. Boots env for the same reason as the
// console loader above.
//
// This is the loader that needs the dotenv boot most: phpstan-doctrine asks the manager for the
// database platform, and Doctrine DBAL 3 opens a real connection to detect the server version
// unless the configuration pins it.
func objectManagerLoaderPHP(env Env) string {
	return kernelBootstrapPHP() + `
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
