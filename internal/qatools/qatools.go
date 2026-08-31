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
	// OroVersion is the line the tools run against. It decides which stylelint packages were
	// installed, and therefore which formatter module stylelint is pointed at; see
	// config.GetQaStylelint. An empty value resolves to the same default GetVersionsForOro uses.
	OroVersion string
	// Baseline is the container path PHPStan writes a generated baseline to. Empty for a normal
	// analysis; when set, PHPStan records what it finds instead of failing on it, so the only tool
	// the caller should keep in the list is PHPStan itself — the others have no baseline.
	Baseline string
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
	// SkipUnless is an optional shell test guarding the whole invocation. When it fails the tool
	// is not run and is recorded as a pass with an empty report, because there is nothing for it
	// to check: the tools it guards are configured by a file OroCommerce ships, and not every Oro
	// version ships every one of them. Only knowable inside the container, which is why it is a
	// shell test and not a bool.
	SkipUnless string
}

// reportEnvByTool lists the tools whose GitLab formatter writes the document itself, to the file
// named by an environment variable, and keeps the human-readable output on stdout. Everything else
// writes the document to stdout, so a redirect is all it needs — the four PHP tools because their
// formatters do, and the two stylelint tools because the formatter they are given is Orobox's own;
// see stylelintformatter.go.
var reportEnvByTool = map[string]string{
	"eslint": "ESLINT_CODE_QUALITY_REPORT",
}

// BinaryPaths maps tool names to the binaries the tools are installed as.
//
// The two linters are OroCommerce's own installation, not a QA-local one. Their configuration is
// OroCommerce's too — .eslintrc.yml and .stylelintrc.yml at the application root, extending
// packages the application's package.json declares — so the only linter whose version is
// guaranteed to satisfy that configuration is the one the application installed for
// `npm run eslint-oro`. A second copy in the QA namespace has to guess the pins instead, and the
// guess is what kept breaking: the ruleset OroCommerce ships evolves per line, and a QA eslint or
// stylelint from another major exits non-zero with an empty report rather than with findings.
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
	phpstanConfig := phpstanConfigRef(sourceRoot, analyzePath)
	rectorConfig := rectorConfigRef(sourceRoot)
	phpCSFixerConfig := mergedConfig(sourceRoot, qaDir, ".php-cs-fixer.dist.php", phpCSFixerMerge)
	twigCSFixerConfig := mergedConfig(sourceRoot, qaDir, ".twig-cs-fixer.php", twigCSFixerMerge)
	eslintConfig := mergedConfig(sourceRoot, oroRoot, ".eslintrc.yml", yamlExtendsMerge)
	eslintIgnore := mergedConfig(sourceRoot, oroRoot, ".eslintignore", nil)
	stylelintConfig := mergedConfig(sourceRoot, oroRoot, ".stylelintrc.yml", yamlExtendsMerge)
	stylelintIgnore := mergedConfig(sourceRoot, oroRoot, ".stylelintignore", nil)
	stylelintCSSConfig := mergedConfig(sourceRoot, oroRoot, ".stylelintrc-css.yml", yamlExtendsMerge)
	stylelintCSSIgnore := mergedConfig(sourceRoot, oroRoot, ".stylelintignore-css", nil)
	stylelintFormatter := stylelintFormatterRef()

	rectorArgs := []string{oroRoot + "/bin/rector", "process", "--config=" + rectorConfig.Path}
	phpCSFixerArgs := []string{oroRoot + "/bin/php-cs-fixer", "fix", "--config=" + phpCSFixerConfig.Path}
	// No positional path for twig-cs-fixer: a CLI path would override the config's Finder,
	// which is what discovers bundle templates under src/**/Resources/views.
	twigArgs := []string{oroRoot + "/bin/twig-cs-fixer", "lint", "--config=" + twigCSFixerConfig.Path}
	// The linters are OroCommerce's own binaries, addressed by the absolute path the application
	// installed them at, never through `npx`. npx resolves a bare tool name against the working
	// directory's node_modules and downloads the newest release when it finds nothing there —
	// which on a bundle install is the common case, since the tools run from the source root under
	// <OroRoot>/bundles. That download fetches ESLint 10 and Stylelint 17, both of which require
	// Node >= 20 and abort with EBADENGINE on the image's Node 18.
	//
	// Why OroCommerce's copy and not a QA-local one: see BinaryPaths. The configuration the two
	// linters are handed is OroCommerce's, so the linter has to be the one that configuration was
	// written for.
	//
	// NODE_PATH covers everything the linters resolve by bare name from a config file. Their own
	// `--resolve-plugins-relative-to` (ESLint) covers plugins only; a shareable config named in an
	// `extends` list is resolved relative to the directory of the file naming it, and the file
	// naming them is OroCommerce's — .eslintrc.yml at OroRoot extends `google`, .stylelintrc.yml
	// extends `@oroinc/oro-stylelint-config`. The application's node_modules comes first because
	// those packages are its own; the QA node_modules stays on the path behind it for what only
	// the QA namespace installs, the two GitLab formatters.
	nodePath := "NODE_PATH=" + oroRoot + "/node_modules:" + qaDir + "/node_modules"
	eslintArgs := []string{
		nodePath,
		oroRoot + "/node_modules/.bin/eslint",
		"--resolve-plugins-relative-to", oroRoot + "/node_modules",
		"--config", eslintConfig.Path, "--ignore-path", eslintIgnore.Path,
		"--quiet", "--no-error-on-unmatched-pattern",
	}
	stylelintBin := oroRoot + "/node_modules/.bin/stylelint"
	stylelintArgs := []string{nodePath, stylelintBin, scssTarget, "--config", stylelintConfig.Path, "--ignore-path", stylelintIgnore.Path, "--quiet", "--allow-empty-input"}
	stylelintCSSArgs := []string{nodePath, stylelintBin, cssTarget, "--config", stylelintCSSConfig.Path, "--ignore-path", stylelintCSSIgnore.Path, "--quiet", "--allow-empty-input"}

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

	// The formatter file is only written when a report is asked for: without one stylelint prints
	// its own human-readable output and there is nothing to format.
	var stylelintFormatterSetup string

	// Each tool's own GitLab reporter, so nothing needs converting anywhere — Rector excepted; see
	// below. The flag names differ because their CLIs do; the document they produce is the same
	// CodeClimate subset.
	var phpstanExtraArgs []string
	if opts.Report == ReportGitLab {
		phpstanExtraArgs = []string{"--error-format=gitlab"}
		// Rector reports its own JSON, not GitLab's, and report.MergeCodeQuality converts it.
		//
		// Its `gitlab` formatter cannot be used even though it exists. Rector warns that the
		// format is deprecated and will be removed in the next minor version, and that Symfony
		// warning block is written to stdout — the same stream the formatter `echo`s the document
		// to, and the stream the report file captures. The merge then rejected the whole run with
		// "invalid character 'W' looking for beginning of value", the 'W' of "[WARNING]".
		//
		// Silencing the warning is not an option either: `process` defines its own option set with
		// no verbosity flag, so `--quiet` aborts the command with "The "--quiet" option does not
		// exist" and Rector never runs at all. The warning is raised only for the `github` and
		// `gitlab` formats, so `json` — which is also what survives the removal — avoids it.
		rectorArgs = append(rectorArgs, "--output-format=json")
		phpCSFixerArgs = append(phpCSFixerArgs, "--format=gitlab")
		twigArgs = append(twigArgs, "--report=gitlab")
		// The formatter is addressed by absolute path: ESLint 8 resolves a bare --format=gitlab
		// against lib/cli-engine/formatters inside its own installation and dies with
		// "Cannot find module".
		eslintArgs = append(eslintArgs, "--format="+qaDir+"/node_modules/eslint-formatter-gitlab")
		// Stylelint's formatter is Orobox's own file rather than a published package, because the
		// stylelint that loads it is OroCommerce's and its major is not knowable here; see
		// stylelintformatter.go. It prints the document to stdout, so the two stylelint tools are
		// reported by redirect like the PHP tools.
		stylelintFormatterSetup = stylelintFormatter.Setup
		stylelintArgs = append(stylelintArgs, "--custom-formatter="+stylelintFormatter.Path)
		stylelintCSSArgs = append(stylelintCSSArgs, "--custom-formatter="+stylelintFormatter.Path)
	}

	// Generating a baseline replaces PHPStan's reporting rather than adding to it: the run's whole
	// output is the baseline file, so the error format is irrelevant and the two flags are refused
	// together by the command layer.
	//
	// --allow-empty-baseline is what keeps a clean tree from failing the command: without it PHPStan
	// exits non-zero with "Baseline could not be generated" when it finds nothing to record, and an
	// empty baseline is the correct answer in that case.
	//
	// A baseline already active through the project config does not have to be excluded here.
	// PHPStan drops the file it is about to generate from its own includes, so a regenerated
	// baseline lists what the tree reports today rather than coming back empty.
	if opts.Baseline != "" {
		phpstanExtraArgs = []string{"--generate-baseline=" + opts.Baseline, "--allow-empty-baseline"}
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
			Args: append([]string{SharedAutoloadPrependEnv + "=1", oroRoot + "/bin/phpstan", "analyze", analyzePath, "--configuration=" + phpstanConfig.Path, "--autoload-file=" + oroRoot + "/vendor/autoload.php", "--memory-limit=-1", "--no-progress"}, phpstanExtraArgs...),
		},
		{Name: "rector", Args: rectorArgs, WorkDir: oroRoot, Setup: rectorConfig.Setup},
		{Name: "php-cs-fixer", Args: phpCSFixerArgs, Setup: phpCSFixerConfig.Setup},
		{Name: "twig-cs-fixer", Args: twigArgs, Setup: twigCSFixerConfig.Setup},
		// The three JS tools are guarded on their configuration existing. OroCommerce generates
		// those files at the application root and does not ship the same set in every version —
		// .stylelintrc-css.yml arrived in 7.0 — and a project install has no stub to fall back
		// on, since a stub written there would overwrite Oro's own file (see scaffold.QaStubs).
		// Without the guard stylelint is handed a --config that names nothing, dies with an
		// uncaught error and writes no report, which grades as a tool that could not run.
		{Name: "eslint", Args: eslintArgs, Setup: joinSetup(binaryGuard("eslint"), eslintPluginLinks(), eslintConfig.Setup), SkipUnless: configExists(eslintConfig)},
		{Name: "stylelint", Args: stylelintArgs, Setup: joinSetup(binaryGuard("stylelint"), stylelintFormatterSetup, stylelintConfig.Setup), SkipUnless: configExists(stylelintConfig)},
		{Name: "stylelint-css", Args: stylelintCSSArgs, Setup: joinSetup(binaryGuard("stylelint-css"), stylelintFormatterSetup, stylelintCSSConfig.Setup), SkipUnless: configExists(stylelintCSSConfig)},
	}

	if opts.Report != ReportNone {
		for i := range tools {
			tools[i].ReportFile = opts.ReportDir + "/" + tools[i].Name + ".json"
			tools[i].ReportEnv = reportEnvByTool[tools[i].Name]
		}
	}

	return tools
}

// binaryGuard fails a JS tool with a readable reason when OroCommerce's linter is not there.
//
// The two linters are the application's own installation, populated by the asset install that
// `oro:install` runs, so a container that never installed the assets has no node_modules at all.
// Without the guard the shell reports a bare "not found" and exit 127 against a path nothing in
// the output explains. It is a Setup line and not a SkipUnless, because a missing linter is a tool
// that could not run — not one this Oro version has nothing to check with.
func binaryGuard(tool string) string {
	return fmt.Sprintf(`{ [ -x %s ] || { echo "orobox: %s is missing. The QA linters run OroCommerce's own installation; build the assets (oro:assets:install) so it exists."; false; }; }`,
		BinaryPaths[tool], BinaryPaths[tool])
}

// oroEslintPluginGaps are the ESLint plugins OroCommerce's configuration references but its
// generated package.json does not always install. Anything the application does ship is used from
// its own tree; these are only the fallbacks.
var oroEslintPluginGaps = []string{"eslint-plugin-no-jquery", "eslint-plugin-import", "eslint-plugin-oro"}

// eslintPluginLinks makes the gap fillers reachable from the tree ESLint resolves plugins in.
//
// A plugin is not resolved the way a shareable config is. `extends` goes through Node's own
// resolution, so NODE_PATH's second entry — the QA tools directory — covers it. A plugin goes
// through --resolve-plugins-relative-to, which is one directory and one directory only, and it has
// to be the application's node_modules so the plugins OroCommerce does install are its own
// versions. A package the application is missing is then simply not findable, and ESLint stops.
//
// Linking is what bridges the two without a second install: the QA copy is symlinked into the
// application's node_modules under the name ESLint looks for, and only when the application has
// nothing there. Nothing is written to the application's package.json — which matters on a project
// install, where <OroRoot> is the developer's own bind-mounted repository and node_modules is the
// one directory in it that is generated rather than authored.
func eslintPluginLinks() string {
	var b strings.Builder
	b.WriteString("{ for plugin in " + strings.Join(oroEslintPluginGaps, " ") + "; do ")
	fmt.Fprintf(&b, `[ -e %s/node_modules/"$plugin" ] && continue; `, config.OroRootDir)
	fmt.Fprintf(&b, `[ -d %s/node_modules/"$plugin" ] || continue; `, config.QaToolsDir)
	fmt.Fprintf(&b, `ln -sfn %s/node_modules/"$plugin" %s/node_modules/"$plugin"; `, config.QaToolsDir, config.OroRootDir)
	// The loop's own status is whatever the last iteration left behind, and a `continue` on the
	// last plugin would make the whole Setup line look like a failure. `true` states the outcome:
	// a plugin that could not be linked is ESLint's to report, with the name in it.
	b.WriteString("done; true; }")
	return b.String()
}

// configExists is the shell test behind Tool.SkipUnless: whether the tool has a configuration at
// all is a question only the container can answer, so it is asked as a shell test.
//
// It tests the configuration's inputs (configRef.Sources), never its resolved Path. Path is what
// the tool reads, and on a bundle install with both halves present that is the merged file — a file
// configRef.Setup writes, and Setup runs inside this guard (see ReportScript). Guarding on Path
// there asked whether a file existed that only the guarded line would ever create, so ESLint and
// Stylelint skipped every run on a bundle checkout while the project layout, whose two halves are
// the same file and never merge, was unaffected.
func configExists(ref configRef) string {
	tests := make([]string, 0, len(ref.Sources))
	for _, file := range ref.Sources {
		tests = append(tests, fmt.Sprintf("[ -f %s ]", file))
	}
	// Either half is enough: with one of them the tool has a configuration, and Path resolves to
	// whichever is there.
	return "{ " + strings.Join(tests, " || ") + "; }"
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
		if t.SkipUnless != "" {
			cmd = fmt.Sprintf("{ if %s; then %s; else echo 'skipped: %s has no configuration in this OroCommerce version'; fi; }",
				t.SkipUnless, cmd, t.Name)
		}
		b.WriteString(cmd)
	}
	return b.String()
}

// StatusFile is the file, inside the report directory, that ReportScript writes the aggregate
// outcome to: "0" when every tool was clean, "1" when at least one reported violations or failed.
const StatusFile = ".status"

// ToolStatusFile is the file, inside the report directory, that ReportScript writes one tool's own
// exit code to.
//
// The aggregate StatusFile only says that something went wrong. Read together with that tool's
// report, a per-tool code separates the two ways a tool exits non-zero: findings — a non-zero code
// next to a report holding issues — from a tool that could not run at all, which exits non-zero
// with an empty report. The E2E suite grades the QA step on exactly that distinction.
func ToolStatusFile(tool string) string { return StatusFile + "-" + tool }

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
//
// Alongside the aggregate StatusFile, every tool's own exit code is recorded in its
// ToolStatusFile. The aggregate is what turns back into the command's exit code; the per-tool
// codes are what let a caller tell a tool that reported findings from one that could not run.
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
		run := fmt.Sprintf("%s > %s", cmd, t.ReportFile)
		if t.ReportEnv != "" {
			run = fmt.Sprintf("%s=%s %s", t.ReportEnv, t.ReportFile, cmd)
		}

		if t.Setup != "" {
			// A failed setup is a failed tool: PHPStan cannot analyse without its warmed cache, and
			// running it anyway would write a report full of bootstrap errors. `false` gives the
			// line the exit code the tool itself would have had, so the per-tool status still says
			// "this tool did not pass".
			run = fmt.Sprintf("if %s; then %s; else false; fi", t.Setup, run)
		}

		// A skipped tool still writes the empty document the caller expects, because a missing
		// report file is how a tool that could not run at all is told apart from a clean one.
		if t.SkipUnless != "" {
			run = fmt.Sprintf("if %s; then %s; else echo 'skipped: %s has no configuration in this OroCommerce version'; printf '[]' > %s; fi",
				t.SkipUnless, run, t.Name, t.ReportFile)
		}

		fmt.Fprintf(&b, "%s\ncode=$?\n", run)
		fmt.Fprintf(&b, "printf '%%s' \"$code\" > %s/%s\n", reportDir, ToolStatusFile(t.Name))
		b.WriteString("[ \"$code\" = 0 ] || status=1\n")
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

	// The linters themselves are not installed here, and neither are the packages their
	// configuration extends. Both come from OroCommerce's own node_modules — see BinaryPaths —
	// which is also where `npm run eslint-oro` gets them, so the QA run resolves the same ruleset
	// the application does instead of a second, guessed-at set of pins.
	//
	// What is left is the one thing the application has no reason to ship: the GitLab Code Quality
	// formatters. They are installed unconditionally rather than only when a report is asked for,
	// because requiring them lazily would make --report=gitlab fail at runtime on every
	// environment initialised before the flag existed.
	//
	// The ^5.1.0 pin on the ESLint formatter stays: 6 and later dropped the ESLint 8 formatter
	// signature, and OroCommerce's supported lines still install ESLint 8.
	//
	// Alongside it go the gap fillers: the packages OroCommerce's own .eslintrc.yml names but its
	// generated package.json does not install. That gap is real and it is ESLint's hard stop, not a
	// warning — with the linters moved to the application's tree, the QA run died on
	//
	//	ESLint couldn't find the plugin "eslint-plugin-no-jquery".
	//
	// before linting a single file. They are pinned to the constraints OroCommerce's own generated
	// package.json declares where it declares them at all, and they are only ever reached when the
	// application's tree does not carry its own copy; see eslintPluginLinks and the NODE_PATH
	// fallback in Tools.
	if needsEslint {
		plan.JSPackages = append(plan.JSPackages,
			"eslint-formatter-gitlab@^5.1.0",
			"eslint-config-google@~0.14.0", "eslint-plugin-oro@~0.0.3",
			"eslint-plugin-no-jquery", "eslint-plugin-import")
	}
	// Stylelint's formatter is not installed at all: it is a file Orobox writes, because no
	// published formatter works across the stylelint majors OroCommerce installs. See
	// stylelintformatter.go.

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

// JSInstallCommand returns the shell line that installs the JS tools into the QA tools directory.
//
// What it installs is the GitLab formatters and nothing else; the linters are OroCommerce's own
// installation, see BinaryPaths.
//
// The manifest it writes first is what pins the install location. npm and pnpm do not install into
// the working directory, they install into the nearest package root — the first directory up the
// tree holding a package.json (or a node_modules) — and OroCommerce generates a package.json at
// the application root. Without a manifest of its own, `cd vendor-bin/qa && npm install` therefore
// writes into <OroRoot>/node_modules on a developer's stack while writing into vendor-bin/qa in a
// pipeline checkout, where the application root has neither file. Two things break on the first of
// those: the --format / --custom-formatter paths in Tools name the QA tree, so the formatter is not
// where the linters are told to look, and the QA packages end up added to the application's own
// dependency tree, which is exactly the coupling this change exists to remove.
//
// An existing manifest is left alone: a project that committed vendor-bin/qa/package.json pinned
// the tool versions there on purpose, exactly as ComposerInstallCommand treats the Composer one.
func JSInstallCommand(plan InstallPlan) string {
	qaDir := config.QaToolsDir
	manifest := qaDir + "/package.json"

	install := fmt.Sprintf("cd %s && %s %s %s", qaDir, plan.JSManager, plan.JSInstallArg, plan.JSSaveDevFlag)
	if plan.JSManager == "pnpm" {
		// A manifest is enough for npm, and not enough for pnpm: pnpm keeps one lockfile and one
		// virtual store per *workspace*, resolved by walking up from the working directory, and
		// OroCommerce's application root is what it finds. Left alone it writes the packages into
		// <OroRoot>/node_modules/.pnpm and leaves vendor-bin/qa/node_modules holding nothing but
		// symlinks into it — which is also why it then refused the install outright with
		// ERR_PNPM_ADDING_TO_ROOT, the error --ignore-workspace-root-check used to silence here.
		//
		// Those symlinks are what broke Oro 7.0, the only line pnpm installs: the QA step's Oro
		// install runs after this one and rewrites the application's own node_modules, pruning
		// every virtual-store entry its package.json does not name. The QA symlinks then point at
		// nothing, and eslint, stylelint and stylelint-css fail with MODULE_NOT_FOUND before they
		// can write a report, so `orobox qa` reports all three as tools that could not run.
		//
		// --ignore-workspace makes the QA tools dir its own project, which puts the lockfile
		// there; --virtual-store-dir puts the packages themselves there, which is the half the
		// .bin symlinks actually resolve through. Both are pinned rather than relying on the
		// first to imply the second.
		install += " --ignore-workspace --virtual-store-dir=" + qaDir + "/node_modules/.pnpm"
	}

	install += " " + strings.Join(plan.JSPackages, " ")

	// A node_modules that is already there is reused, and when it cannot be it is rebuilt rather
	// than reported. pnpm refuses to touch a tree linked from a different store than the one it is
	// configured with — ERR_PNPM_UNEXPECTED_STORE — and that is the normal state here: the runtime
	// image bakes its QA tree against a store inside the image, while the pipeline points pnpm at
	// its own cache volume. The same refusal comes from a pnpm major change. Nothing in this
	// directory is precious — it holds the two GitLab formatters and a lockfile for them — so the
	// answer is to drop it and install again, which costs seconds.
	//
	// The retry is on any failure, not on that message: a store mismatch is only one of the ways a
	// half-written tree stops an install, and both attempts print their own reason, so a genuine
	// failure (an unreachable registry) still ends the command with its own error and status.
	return fmt.Sprintf(`# Installing the QA JS packages: the GitLab Code Quality formatters
set -e
mkdir -p %[1]s
[ -f %[2]s ] || printf '%%s' '{"name":"orobox-qa-tools","private":true}' > %[2]s
if ! (%[3]s); then
  echo 'The QA JS install failed; rebuilding %[1]s/node_modules from scratch and retrying.'
  rm -rf %[1]s/node_modules
  (%[3]s)
fi`, qaDir, manifest, install)
}

// OroLinterInstallCommand returns the shell line that makes sure OroCommerce's own node_modules
// holds the linters `tools` needs. It is empty when none of the tools comes from there.
//
// The linters are the application's installation (see BinaryPaths), and in a pipeline container
// that tree exists only if something in the run created it. `oro:install` does, through its asset
// install — but the QA step reaches an installed Oro three different ways, and two of them skip it:
// a cached install is reused without reinstalling anything, and a seeded one is restored from a
// database dump and reconciled with oro:platform:update. On those two paths the linters would be
// missing and all three JS tools would be reported as tools that could not run.
//
// So the tree is installed here rather than assumed, and only when it is not already there — the
// full-install path leaves it in place, and this then costs one `test`.
//
// `<manager> install` is the whole cost in the normal case: OroCommerce's package.json is what
// declares the linters, their shareable configs and their plugins, so installing it is enough and
// webpack never has to run. The console fallback is for the case where that file is not there yet;
// only `oro:assets:install` knows how to produce it, and it builds the assets on the way.
//
// pnpm is asked not to insist on the lockfile: the manifest can be regenerated by the asset
// install while a committed pnpm-lock.yaml is not, and a mismatch there would abort the step over
// a lockfile no linter reads.
func OroLinterInstallCommand(manager string, tools []Tool) string {
	oroRoot := config.OroRootDir

	var checks []string
	seen := map[string]bool{}
	for _, t := range tools {
		bin, ok := BinaryPaths[t.Name]
		if !ok || !strings.HasPrefix(bin, oroRoot+"/node_modules/") || seen[bin] {
			continue
		}
		seen[bin] = true
		checks = append(checks, fmt.Sprintf("[ -x %s ]", bin))
	}
	if len(checks) == 0 {
		return ""
	}

	install := "(cd " + oroRoot + " && " + manager + " install --no-audit --no-fund)"
	if manager == "pnpm" {
		base := "(cd " + oroRoot + " && pnpm install --no-frozen-lockfile"
		// pnpm refuses a node_modules linked from a store other than the one it is configured
		// with (ERR_PNPM_UNEXPECTED_STORE), which is the normal state in the pipeline: the image
		// baked its tree against a store inside the image, and the run points pnpm at a cache
		// volume. --force is pnpm's own answer to that — refetch and relink instead of refusing —
		// and it is only paid when the plain install has already failed.
		install = base + ") || " + base + " --force)"
	}

	script := fmt.Sprintf(`# Preparing OroCommerce's node_modules: the QA linters are its own installation
set -e
if %[1]s; then
  echo 'Reusing the linters OroCommerce already installed.'
elif [ -f %[2]s/package.json ]; then
  echo 'Installing OroCommerce'"'"'s JS dependencies: the QA linters come from them.'
  %[3]s
else
  echo 'OroCommerce has no package.json yet: installing the assets, which generates it.'
  php bin/console oro:assets:install --env=%[4]s --no-interaction
fi`, strings.Join(checks, " && "), oroRoot, install, EnvTest)

	if add := oroMissingPackagesCommand(manager, tools); add != "" {
		script += "\n" + add
	}

	return script
}

// oroConfigOnlyPackage is a package OroCommerce's own configuration names and the package.json it
// generates does not declare. Config and Token are what decide whether the line needs it at all:
// the file at the application root, and the string that file uses to name the package.
type oroConfigOnlyPackage struct {
	Name   string
	Config string
	Token  string
}

// oroConfigOnlyPackages lists those packages per tool. They are installed on top of OroCommerce's
// manifest, because installing that manifest alone cannot satisfy a configuration written against
// more than it.
//
// ESLint's no-jquery plugin is the one entry. Every other package the two linters' configurations
// name is declared by the manifest on every supported line — eslint-config-google, eslint-plugin-oro
// and @oroinc/oro-stylelint-config on all of them, eslint-plugin-import on 7.0, which is also the
// only line whose .eslintrc.yml names `import`. no-jquery is the exception: 6.0, 6.1 and 7.0 all
// extend "plugin:no-jquery/deprecated" and list no-jquery under `plugins`, and none of them declare
// eslint-plugin-no-jquery. Without it ESLint stops before linting anything:
//
//	ESLint couldn't find the plugin "eslint-plugin-no-jquery".
//
// The package is left unpinned: it declares eslint >= 8 as a peer, so the resolved version fits
// whichever ESLint major the Oro line installed.
var oroConfigOnlyPackages = map[string][]oroConfigOnlyPackage{
	"eslint": {{Name: "eslint-plugin-no-jquery", Config: ".eslintrc.yml", Token: "no-jquery"}},
}

// oroMissingPackagesCommand adds the oroConfigOnlyPackages the enabled tools need to OroCommerce's
// node_modules, and only when the line actually needs them.
//
// It is a step of its own rather than part of the install above, because the install is skipped
// whenever the binaries are present — a full install, a cached tree — and that is precisely the run
// where the package is missing. The packages have to land in the application's tree and nowhere
// else: ESLint resolves plugins through --resolve-plugins-relative-to, which Tools points at that
// directory, so a copy in the QA namespace is not on the search path at all.
//
// The configuration is what is asked, not the Oro version: 5.1's .eslintrc.yml does not name
// no-jquery and must not pay for it, and a line that starts or stops naming a package needs no
// change here. Only knowable inside the container, so it is a grep and not a version check.
func oroMissingPackagesCommand(manager string, tools []Tool) string {
	oroRoot := config.OroRootDir

	var packages []oroConfigOnlyPackage
	seen := map[string]bool{}
	for _, t := range tools {
		for _, pkg := range oroConfigOnlyPackages[t.Name] {
			if !seen[pkg.Name] {
				seen[pkg.Name] = true
				packages = append(packages, pkg)
			}
		}
	}
	if len(packages) == 0 {
		return ""
	}

	// pnpm treats the application root as a workspace root and refuses to add to it unless told
	// the addition is deliberate (ERR_PNPM_ADDING_TO_ROOT). Unlike the QA namespace this tree is
	// the workspace, so the check is waived rather than the workspace ignored.
	add := "cd " + oroRoot + " && " + manager + " install --save-dev"
	if manager == "pnpm" {
		add = "cd " + oroRoot + " && pnpm add -D --ignore-workspace-root-check"
	}

	var blocks []string
	for _, pkg := range packages {
		blocks = append(blocks, fmt.Sprintf(`if ! grep -q '%[1]s' %[2]s/%[3]s 2>/dev/null; then
  echo 'OroCommerce'"'"'s %[3]s does not name %[4]s on this line: nothing to add.'
elif [ -d %[2]s/node_modules/%[4]s ]; then
  echo '%[4]s is already installed.'
else
  echo 'Adding %[4]s: OroCommerce'"'"'s %[3]s names it and its package.json does not declare it.'
  (%[5]s %[4]s)
fi`, pkg.Token, oroRoot, pkg.Config, pkg.Name, add))
	}

	return strings.Join(blocks, "\n")
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

    // The constraint covers the patch line the application is on rather than the one version it
    // ships. An exact "6.4.16" makes every QA package that wants a later patch of the same line
    // unsatisfiable — php-cs-fixer requires symfony/options-resolver ^6.4.24 — and Composer's
    // answer to an unsatisfiable requirement is not an error but an older release of whatever
    // asked for it, which is how the QA tree silently ended up on an algoritma release whose
    // plugin no longer writes the shared ruleset. Symfony keeps its patch releases BC, so
    // claiming the line rather than the point release is what keeps the tools current.
    $manifest['replace'][$name] = preg_match('/^(\d+\.\d+)\./', $version, $line) === 1
        ? $line[1] . '.*'
        : $version;

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
//
// The mkdir is the consequence of that same "can legitimately not exist": phpstan.neon lists the
// directory under scanDirectories, and PHPStan refuses to start when a scanned directory is
// missing ("Scanned directory /var/www/oro/var/cache/test/Symfony/Config does not exist."), which
// is how a warm cache without dumped configs used to fail the whole QA set. An empty directory
// contributes no symbols, so creating it changes nothing but the startup check.
func phpstanCacheWarmup(env Env) string {
	return fmt.Sprintf("{ [ -f %s ] || ORO_DEBUG=1 php %s/bin/console cache:warmup --env=%s; } && mkdir -p %s",
		ContainerXMLPath(env), config.OroRootDir, env.orDefault(), SymfonyConfigDir(env))
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

// baseConfigGenerators pairs each base configuration file with the algoritma/php-coding-standards
// command that writes it, in the order the tools run.
var baseConfigGenerators = []struct {
	Tool    string
	File    string
	Command string
}{
	{"phpstan", "phpstan.neon", "algoritma-phpstan-create-config"},
	{"rector", "rector.php", "algoritma-rector-create-config"},
	{"php-cs-fixer", ".php-cs-fixer.dist.php", "algoritma-cs-create-config"},
}

// BaseConfigScript writes the base configurations algoritma/php-coding-standards ships, for the
// enabled tools that do not have one yet.
//
// The package generates them from a Composer plugin listening on its own post-package-install
// event, and that event is not a contract: the releases pinned to an older Symfony line — which is
// what the shared-vendor `replace` list resolves to on Oro 6.0, where the application ships
// symfony/options-resolver 6.4.16 and the newer php-cs-fixer requires ^6.4.24 — install without
// ever writing a file. The base standard is then silently absent, and PHPStan is handed a
// --configuration that does not exist ("Project config file at path ... does not exist"), or, on a
// checkout that commits its own phpstan.neon, quietly analyses without the shared ruleset.
//
// The same package also exposes the generation as ordinary Composer commands, and those are the
// same across the 3.0 line, so they are what this asks for. Each one is run from the QA directory
// because the writers take relative paths, and only when its file is missing: a committed or
// already generated configuration is never overwritten.
//
// A failed generation warns instead of failing the step. The tools' configs are independent, and
// with a project configuration in place the run is degraded rather than broken — while a hard
// failure here would take down a QA install that only wanted the other tools.
func BaseConfigScript() string {
	qa := config.QaToolsDir

	lines := []string{"# Writing the base QA configurations the coding standard ships"}
	for _, gen := range baseConfigGenerators {
		if !config.IsQaToolEnabled(gen.Tool) {
			continue
		}
		file := qa + "/" + gen.File
		lines = append(lines,
			fmt.Sprintf("[ -f %s ] || (cd %s && composer %s --no-interaction) || true", file, qa, gen.Command),
			fmt.Sprintf(`[ -f %[1]s ] || echo "orobox: algoritma/php-coding-standards did not write %[1]s, so %[2]s runs without the shared standard." >&2`, file, gen.Tool),
		)
	}

	if len(lines) == 1 {
		return ""
	}

	return strings.Join(lines, "\n")
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
