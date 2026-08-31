package qatools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/spf13/viper"
)

func toolByName(t *testing.T, tools []Tool, name string) Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return Tool{}
}

func TestToolsFixMode(t *testing.T) {
	tools := Tools(ToolsOptions{SourceRoot: config.OroRootDir, AnalyzePath: config.OroRootDir + "/src", Mode: ModeFix})

	for _, name := range []string{"rector", "php-cs-fixer", "twig-cs-fixer", "eslint", "stylelint", "stylelint-css"} {
		args := strings.Join(toolByName(t, tools, name).Args, " ")
		if name == "rector" || name == "php-cs-fixer" {
			// These two mutate by default, so fix mode is simply the absence of --dry-run.
			if strings.Contains(args, "--dry-run") {
				t.Errorf("%s in fix mode contains --dry-run: %s", name, args)
			}
			continue
		}
		if !strings.Contains(args, "--fix") {
			t.Errorf("%s in fix mode is missing --fix: %s", name, args)
		}
	}
}

func TestToolsCheckMode(t *testing.T) {
	tools := Tools(ToolsOptions{SourceRoot: config.OroRootDir, AnalyzePath: config.OroRootDir + "/src", Mode: ModeCheck})

	for _, tool := range tools {
		args := strings.Join(tool.Args, " ")
		if strings.Contains(args, "--fix") {
			t.Errorf("%s in check mode still passes --fix: %s", tool.Name, args)
		}
	}

	if args := strings.Join(toolByName(t, tools, "rector").Args, " "); !strings.Contains(args, "--dry-run") {
		t.Errorf("rector in check mode is missing --dry-run: %s", args)
	}

	phpCS := strings.Join(toolByName(t, tools, "php-cs-fixer").Args, " ")
	for _, want := range []string{"--dry-run", "--diff"} {
		if !strings.Contains(phpCS, want) {
			t.Errorf("php-cs-fixer in check mode is missing %s: %s", want, phpCS)
		}
	}
}

func TestToolsAreOrderedAndSelfContained(t *testing.T) {
	tools := Tools(ToolsOptions{SourceRoot: "/src/root", AnalyzePath: "/src/root/src", Mode: ModeCheck})

	want := []string{"phpstan", "rector", "php-cs-fixer", "twig-cs-fixer", "eslint", "stylelint", "stylelint-css"}
	if len(tools) != len(want) {
		t.Fatalf("got %d tools, want %d", len(tools), len(want))
	}
	for i, name := range want {
		if tools[i].Name != name {
			t.Errorf("tool %d = %q, want %q", i, tools[i].Name, name)
		}
	}

	// The project's own config must be preferred over the generated one for every tool.
	phpstan := strings.Join(toolByName(t, tools, "phpstan").Args, " ")
	if !strings.Contains(phpstan, "/src/root/phpstan.neon") || !strings.Contains(phpstan, config.QaToolsDir+"/phpstan.neon") {
		t.Errorf("phpstan args do not implement the project-first config fallback: %s", phpstan)
	}
	if !strings.Contains(phpstan, "/src/root/src") {
		t.Errorf("phpstan does not analyze the requested path: %s", phpstan)
	}
	// PHPStan analyses in parallel workers sized from the core count. A worker that hits PHP's
	// memory_limit dies and hands back a garbled response, which surfaces as an "Internal error"
	// naming whatever file was in flight — a crash that depends on the machine, not the code.
	// --no-progress keeps the bar out of a CI log, which has no terminal to redraw.
	for _, want := range []string{"--memory-limit=-1", "--no-progress"} {
		if !strings.Contains(phpstan, want) {
			t.Errorf("phpstan args missing %q: %s", want, phpstan)
		}
	}

	// Rector resolves its config relative to the app root, so it must run from there.
	if wd := toolByName(t, tools, "rector").WorkDir; wd != config.OroRootDir {
		t.Errorf("rector WorkDir = %q, want %q", wd, config.OroRootDir)
	}
}

func TestPhpstanWarmsTheDebugCacheOfItsEnv(t *testing.T) {
	// The pipeline analyses the test install; a developer's stack has dev installed instead, and
	// the dumped container's filename carries the environment, so both halves move together.
	for _, tc := range []struct {
		env     Env
		warmup  string
		xmlFile string
	}{
		{EnvTest, "cache:warmup --env=test", "/var/cache/test/AppKernelTestDebugContainer.xml"},
		{EnvDev, "cache:warmup --env=dev", "/var/cache/dev/AppKernelDevDebugContainer.xml"},
	} {
		setup := toolByName(t, Tools(ToolsOptions{SourceRoot: "/src/root", AnalyzePath: "/src/root/src", Env: tc.env, Mode: ModeCheck}), "phpstan").Setup

		for _, want := range []string{
			"[ -f " + ContainerXMLPath(tc.env) + " ] ||",
			"ORO_DEBUG=1 php " + config.OroRootDir + "/bin/console " + tc.warmup,
		} {
			if !strings.Contains(setup, want) {
				t.Errorf("%s: phpstan setup missing %q: %s", tc.env, want, setup)
			}
		}

		// The dumped Symfony config directory is not part of the gate: an install can end with
		// a container and no Symfony/Config, and checking it there warms on every run.
		if strings.Contains(setup, "[ -f "+SymfonyConfigDir(tc.env)+" ]") {
			t.Errorf("%s: phpstan warmup gated on the Symfony config dir: %s", tc.env, setup)
		}

		// It is created instead, because phpstan.neon scans it and PHPStan refuses to start on a
		// missing scanDirectories entry ("Scanned directory ... does not exist"), which failed the
		// whole QA set on an install whose bundles dumped no configs.
		if !strings.Contains(setup, "mkdir -p "+SymfonyConfigDir(tc.env)) {
			t.Errorf("%s: phpstan setup must create the scanned Symfony config dir: %s", tc.env, setup)
		}

		if got := ContainerXMLPath(tc.env); got != config.OroRootDir+tc.xmlFile {
			t.Errorf("%s: ContainerXMLPath = %q, want %q", tc.env, got, config.OroRootDir+tc.xmlFile)
		}
	}

	// An unset environment is the pipeline's test one: that is what every caller meant before
	// the environment became selectable.
	if CacheDir("") != CacheDir(EnvTest) {
		t.Errorf("the zero Env must fall back to test, got %q", CacheDir(""))
	}
}

func TestScript(t *testing.T) {
	script := Script([]Tool{
		{Name: "phpstan", Args: []string{"bin/phpstan", "analyze"}, Setup: "warmup"},
		{Name: "rector", Args: []string{"bin/rector", "process"}, WorkDir: "/var/www/oro"},
	})

	if !strings.Contains(script, "echo '--- Running phpstan ---' && warmup && bin/phpstan analyze && ") {
		t.Errorf("script does not announce, set up and chain phpstan: %s", script)
	}
	if !strings.Contains(script, "(cd /var/www/oro && bin/rector process)") {
		t.Errorf("script does not honor the work dir: %s", script)
	}
}

func TestNewInstallPlan(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	plan := NewInstallPlan("6.1")
	if !plan.NeedsComposerTools || !plan.NeedsJSTools {
		t.Fatalf("all tools default to enabled, got composer=%v js=%v", plan.NeedsComposerTools, plan.NeedsJSTools)
	}
	if plan.JSManager != "npm" {
		t.Errorf("JSManager for 6.1 = %q, want npm", plan.JSManager)
	}
	joined := strings.Join(plan.ComposerPackages, " ")
	for _, want := range []string{"algoritma/php-coding-standards:*", "vincentlanglet/twig-cs-fixer:*", "symfony/console:^6.4"} {
		if !strings.Contains(joined, want) {
			t.Errorf("composer packages missing %q: %s", want, joined)
		}
	}

	// Oro 7.0 ships pnpm, which needs a different add command and flag.
	if plan := NewInstallPlan("7.0"); plan.JSManager != "pnpm" || plan.JSInstallArg != "add" || plan.JSSaveDevFlag != "-D" {
		t.Errorf("7.0 JS plan = %q %q %q, want pnpm add -D", plan.JSManager, plan.JSInstallArg, plan.JSSaveDevFlag)
	}

	viper.Set("test.qa.phpstan", false)
	viper.Set("test.qa.rector", false)
	viper.Set("test.qa.php_cs_fixer", false)
	viper.Set("test.qa.twig_cs_fixer", false)
	viper.Set("test.qa.eslint", false)
	viper.Set("test.qa.stylelint", false)

	if plan := NewInstallPlan("6.1"); plan.NeedsComposerTools || plan.NeedsJSTools || len(plan.ComposerPackages) > 0 {
		t.Errorf("everything disabled should install nothing, got %+v", plan)
	}
}

func TestConfigScripts(t *testing.T) {
	phpstan := PhpstanConfigScript(EnvDev)
	for _, want := range []string{
		config.QaToolsDir + "/phpstan.neon",
		"consoleApplicationLoader: " + config.QaToolsDir + "/tests/console-application.php",
		"base64 -d",
	} {
		if !strings.Contains(phpstan, want) {
			t.Errorf("phpstan script missing %q", want)
		}
	}

	twig := TwigConfigScript()
	if !strings.Contains(twig, "[ -f "+config.QaToolsDir+"/.twig-cs-fixer.php ] ||") {
		t.Errorf("twig script must not overwrite an existing config: %s", twig)
	}
}

// TestBaseConfigScript covers the case that produced "Project config file at path
// vendor-bin/qa/phpstan.neon does not exist": the coding standard's Composer plugin installs
// without writing its own configurations, so the QA install has to ask for them explicitly.
func TestBaseConfigScript(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	script := BaseConfigScript()
	for _, want := range []string{
		"[ -f " + config.QaToolsDir + "/phpstan.neon ] || (cd " + config.QaToolsDir + " && composer algoritma-phpstan-create-config --no-interaction)",
		"composer algoritma-rector-create-config",
		"composer algoritma-cs-create-config",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("base config script missing %q:\n%s", want, script)
		}
	}

	// A generation that fails leaves the run degraded, not broken: the tools' configurations are
	// independent, and a project config can still carry the analysis.
	if !strings.Contains(script, "|| true") {
		t.Errorf("a failed generation must not fail the step: %s", script)
	}
	if !strings.Contains(script, "did not write") {
		t.Errorf("a missing base config must be reported: %s", script)
	}

	// Disabled tools ask for nothing, and with every PHP tool off there is nothing to write.
	viper.Set("test.qa.rector", false)
	if script := BaseConfigScript(); strings.Contains(script, "algoritma-rector-create-config") {
		t.Errorf("rector is disabled: %s", script)
	}

	viper.Set("test.qa.phpstan", false)
	viper.Set("test.qa.php_cs_fixer", false)
	if script := BaseConfigScript(); script != "" {
		t.Errorf("no PHP tool enabled should write nothing, got %q", script)
	}
}

func TestComposerInstallCommand(t *testing.T) {
	cmd := ComposerInstallCommand([]string{"algoritma/php-coding-standards:*", "symfony/console:^6.4"})

	for _, want := range []string{
		config.QaToolsDir + "/composer.json",
		"composer bin qa install --no-interaction --no-progress",
		"composer bin qa require --dev -W --no-interaction $MISSING",
		`explode(" ", "algoritma/php-coding-standards:* symfony/console:^6.4")`,
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q: %s", want, cmd)
		}
	}

	// Requiring is what the undeclared packages branch does, so it must be the then-branch:
	// installing a manifest that does not declare them yet leaves their binaries missing.
	require := strings.Index(cmd, "composer bin qa require")
	install := strings.Index(cmd, "composer bin qa install")
	if install < 0 || require < 0 || require > install {
		t.Errorf("require must be the then-branch, install the fallback: %s", cmd)
	}
}

// TestComposerInstallCommandDetectsUndeclaredPackages runs the generated detection snippet
// through php against a manifest that declares only some of the packages: a tool enabled after
// the QA directory was first scaffolded must still be required into it, while the packages the
// manifest pins are left alone.
func TestComposerInstallCommandDetectsUndeclaredPackages(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php not available")
	}

	dir := t.TempDir()
	manifest := filepath.Join(dir, "composer.json")
	// symfony/service-contracts sits in `replace`, the way SharedVendorScript leaves it: requiring
	// it back would reinstall the duplicate copy that fatals PHPStan.
	if err := os.WriteFile(manifest, []byte(`{"name":"orobox/qa-tools","require-dev":{"algoritma/php-coding-standards":"^3.0","symfony/console":"^6.4"},`+
		`"replace":{"symfony/service-contracts":"3.7.1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := ComposerInstallCommand([]string{"algoritma/php-coding-standards:*", "symfony/console:^6.4", "symfony/service-contracts:^3.0", "vincentlanglet/twig-cs-fixer:*"})
	cmd = strings.Replace(cmd, config.QaToolsDir+"/composer.json", manifest, 1)
	// Only the detection snippet is executed: composer is not available here, and the shell
	// branch it feeds is covered above.
	snippet := cmd[strings.Index(cmd, "php -r "):strings.Index(cmd, `)"; if`)]

	out, err := exec.Command("sh", "-c", snippet).Output()
	if err != nil {
		t.Fatalf("running detection snippet with %s: %v", php, err)
	}
	if got := strings.TrimSpace(string(out)); got != "vincentlanglet/twig-cs-fixer:*" {
		t.Errorf("missing packages = %q, want only the undeclared twig-cs-fixer", got)
	}
}

func TestToolsReportModeAddsTheGitLabFlag(t *testing.T) {
	tools := Tools(ToolsOptions{
		SourceRoot:  config.OroRootDir,
		AnalyzePath: config.OroRootDir + "/src",
		Mode:        ModeCheck,
		Report:      ReportGitLab,
		ReportDir:   "/reports/qa",
		OroVersion:  "6.0",
	})

	// Stylelint's formatter is named down to the module file: stylelint 16 imports it as an ES
	// module, and a directory import fails with ERR_UNSUPPORTED_DIR_IMPORT.
	stylelintFormatter := "--custom-formatter=" + config.QaToolsDir + "/node_modules/stylelint-formatter-gitlab/index.js"

	want := map[string]string{
		"phpstan": "--error-format=gitlab",
		// Rector reports its own JSON, which report.MergeCodeQuality converts; see the test below.
		"rector":        "--output-format=json",
		"php-cs-fixer":  "--format=gitlab",
		"twig-cs-fixer": "--report=gitlab",
		// ESLint 8 resolves a bare --format=gitlab against its own installation and fails, so the
		// formatter is addressed by absolute path.
		"eslint":        "--format=" + config.QaToolsDir + "/node_modules/eslint-formatter-gitlab",
		"stylelint":     stylelintFormatter,
		"stylelint-css": stylelintFormatter,
	}

	for name, flag := range want {
		args := strings.Join(toolByName(t, tools, name).Args, " ")
		if !strings.Contains(args, flag) {
			t.Errorf("%s in report mode is missing %s: %s", name, flag, args)
		}
	}
}

func TestToolsReportModeUsesAnEnvVarForTheJSTools(t *testing.T) {
	tools := Tools(ToolsOptions{
		SourceRoot:  config.OroRootDir,
		AnalyzePath: config.OroRootDir + "/src",
		Mode:        ModeCheck,
		Report:      ReportGitLab,
		ReportDir:   "/reports/qa",
	})

	want := map[string]string{
		"eslint":        "ESLINT_CODE_QUALITY_REPORT",
		"stylelint":     "STYLELINT_CODE_QUALITY_REPORT",
		"stylelint-css": "STYLELINT_CODE_QUALITY_REPORT",
	}
	for name, variable := range want {
		if got := toolByName(t, tools, name).ReportEnv; got != variable {
			t.Errorf("%s ReportEnv = %q, want %q: the formatter writes the file itself", name, got, variable)
		}
	}

	for _, name := range []string{"phpstan", "rector", "php-cs-fixer", "twig-cs-fixer"} {
		if got := toolByName(t, tools, name).ReportEnv; got != "" {
			t.Errorf("%s ReportEnv = %q, want empty: it writes to stdout", name, got)
		}
	}
}

func TestToolsReportModeAssignsOneFilePerTool(t *testing.T) {
	tools := Tools(ToolsOptions{
		SourceRoot:  config.OroRootDir,
		AnalyzePath: config.OroRootDir + "/src",
		Mode:        ModeCheck,
		Report:      ReportGitLab,
		ReportDir:   "/reports/qa",
	})

	seen := map[string]bool{}
	for _, tool := range tools {
		if tool.ReportFile == "" {
			t.Errorf("%s has no ReportFile in report mode", tool.Name)
			continue
		}
		if !strings.HasPrefix(tool.ReportFile, "/reports/qa/") {
			t.Errorf("%s writes outside the report directory: %s", tool.Name, tool.ReportFile)
		}
		if seen[tool.ReportFile] {
			t.Errorf("%s reuses the report file %s; one tool would overwrite the other", tool.Name, tool.ReportFile)
		}
		seen[tool.ReportFile] = true
	}
}

func TestToolsWithoutReportAreUnchanged(t *testing.T) {
	tools := Tools(ToolsOptions{
		SourceRoot:  config.OroRootDir,
		AnalyzePath: config.OroRootDir + "/src",
		Mode:        ModeFix,
	})

	for _, tool := range tools {
		args := strings.Join(tool.Args, " ")
		if strings.Contains(args, "gitlab") {
			t.Errorf("%s asks for a report without one being requested: %s", tool.Name, args)
		}
		if tool.ReportFile != "" {
			t.Errorf("%s has ReportFile %q without a report being requested", tool.Name, tool.ReportFile)
		}
	}
}

// TestReportScriptSkipsAToolWhoseConfigurationIsMissing covers the JS tools on an Oro version
// that does not ship their configuration: .stylelintrc-css.yml only exists from 7.0, and a
// stylelint handed a --config naming nothing exits non-zero with no report at all, which the
// grading reads as a tool that could not run.
func TestReportScriptSkipsAToolWhoseConfigurationIsMissing(t *testing.T) {
	tools := Tools(ToolsOptions{
		SourceRoot: config.OroRootDir,
		Mode:       ModeCheck,
		Report:     ReportGitLab,
		ReportDir:  "/reports/qa",
		OroVersion: "6.0",
	})

	for _, name := range []string{"eslint", "stylelint", "stylelint-css"} {
		tool := toolByName(t, tools, name)
		if tool.SkipUnless == "" {
			t.Errorf("%s runs unguarded, so a missing configuration fails it instead of skipping it", name)
			continue
		}
		script := ReportScript([]Tool{tool}, "/reports/qa")
		// The skip still writes the empty document: a report file that is not there is how the
		// caller tells a tool that never ran from one that found nothing.
		if !strings.Contains(script, "printf '[]' > /reports/qa/"+name+".json") {
			t.Errorf("the %s skip writes no empty report:\n%s", name, script)
		}
		if !strings.Contains(script, tool.SkipUnless) {
			t.Errorf("the %s guard is missing from the script:\n%s", name, script)
		}
	}

	// PHPStan is configured by a file orobox itself scaffolds, so it is never skipped.
	if guard := toolByName(t, tools, "phpstan").SkipUnless; guard != "" {
		t.Errorf("phpstan carries a skip guard %q; its configuration is orobox's own", guard)
	}
}

func TestInstallPlanAlwaysCarriesTheGitLabFormatters(t *testing.T) {
	viper.Set("test.qa.eslint", true)
	viper.Set("test.qa.stylelint", true)
	defer func() {
		viper.Set("test.qa.eslint", nil)
		viper.Set("test.qa.stylelint", nil)
	}()

	plan := NewInstallPlan("6.1")
	packages := strings.Join(plan.JSPackages, " ")

	for _, pkg := range []string{"eslint-formatter-gitlab@^5.1.0", "@studiometa/stylelint-formatter-gitlab@^1.1.1"} {
		if !strings.Contains(packages, pkg) {
			t.Errorf("%s is missing from the JS packages: %s", pkg, packages)
		}
	}
}

// TestInstallPlanPicksTheStylelintFormatterForTheLinesStylelintMajor covers what is left of the
// per-line stylelint decision now that stylelint itself comes from OroCommerce: the two stylelint
// majors load a custom formatter differently — 15 `require()`s it, 16 `import()`s it — so the
// formatter still has to match the major the Oro line installed, or the report comes back empty.
func TestInstallPlanPicksTheStylelintFormatterForTheLinesStylelintMajor(t *testing.T) {
	viper.Set("test.qa.stylelint", true)
	defer viper.Set("test.qa.stylelint", nil)

	for _, tc := range []struct {
		version   string
		formatter string
	}{
		{version: "7.0", formatter: "@studiometa/stylelint-formatter-gitlab@^1.1.1"},
		{version: "6.1", formatter: "@studiometa/stylelint-formatter-gitlab@^1.1.1"},
		{version: "6.0", formatter: "stylelint-formatter-gitlab"},
		{version: "5.1", formatter: "stylelint-formatter-gitlab"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			packages := NewInstallPlan(tc.version).JSPackages
			if !slices.Contains(packages, tc.formatter) {
				t.Errorf("Oro %s installs %v, want %s among them", tc.version, packages, tc.formatter)
			}
		})
	}
}

// TestInstallPlanLeavesTheLintersToOroCommerce is the pin problem stated as a test. The linters are
// configured by files OroCommerce generates at the application root, extending packages its own
// package.json declares, so a QA-local eslint or stylelint has to guess a version that satisfies a
// ruleset it does not own — and a wrong guess exits non-zero with an empty report instead of with
// findings. The binaries come from <OroRoot>/node_modules, see BinaryPaths.
func TestInstallPlanLeavesTheLintersToOroCommerce(t *testing.T) {
	viper.Set("test.qa.eslint", true)
	viper.Set("test.qa.stylelint", true)
	defer func() {
		viper.Set("test.qa.eslint", nil)
		viper.Set("test.qa.stylelint", nil)
	}()

	packages := strings.Join(NewInstallPlan("6.1").JSPackages, " ")

	// Stylelint's side needs nothing else: its shareable config is one OroCommerce installs, and
	// the QA run resolves it from the application's tree.
	for _, pkg := range []string{"eslint@", "stylelint@", "@oroinc/oro-stylelint-config"} {
		if strings.Contains(packages, pkg) {
			t.Errorf("%s is installed in the QA tools dir; the linters and their configs are OroCommerce's: %s", pkg, packages)
		}
	}
}

// TestInstallPlanFillsTheGapsOroCommercesEslintrcLeaves covers what the move to OroCommerce's
// binaries broke: its generated .eslintrc.yml names plugins its generated package.json does not
// install, and ESLint stops at the first one it cannot find —
//
//	ESLint couldn't find the plugin "eslint-plugin-no-jquery".
//
// so the QA step reported eslint as a tool that could not run.
func TestInstallPlanFillsTheGapsOroCommercesEslintrcLeaves(t *testing.T) {
	viper.Set("test.qa.eslint", true)
	defer viper.Set("test.qa.eslint", nil)

	packages := strings.Join(NewInstallPlan("7.0").JSPackages, " ")

	for _, pkg := range []string{"eslint-config-google@~0.14.0", "eslint-plugin-oro@~0.0.3", "eslint-plugin-no-jquery", "eslint-plugin-import"} {
		if !strings.Contains(packages, pkg) {
			t.Errorf("%s is missing from the JS packages: %s", pkg, packages)
		}
	}
}

// TestEslintPluginLinksBridgeTheResolutionFlag pins the mechanism. A shareable config named in
// `extends` goes through Node's own resolution, which NODE_PATH's second entry covers; a plugin goes
// through --resolve-plugins-relative-to, which is a single directory and has to be the
// application's, so a package the application is missing is unreachable there. The gap fillers are
// therefore linked in, and only where the application has nothing of its own.
func TestEslintPluginLinksBridgeTheResolutionFlag(t *testing.T) {
	setup := toolByName(t, Tools(ToolsOptions{SourceRoot: bundleRoot, AnalyzePath: bundleRoot}), "eslint").Setup

	if out, err := exec.Command("sh", "-n", "-c", setup).CombinedOutput(); err != nil {
		t.Fatalf("the eslint setup is not valid shell: %v\n%s\n%s", err, out, setup)
	}
	for _, plugin := range oroEslintPluginGaps {
		if !strings.Contains(setup, plugin) {
			t.Errorf("%s is never linked into the tree ESLint resolves plugins in: %s", plugin, setup)
		}
	}
	// The application's own copy always wins, and a gap filler that was never installed is not
	// linked to nothing.
	if !strings.Contains(setup, `[ -e `+config.OroRootDir+`/node_modules/"$plugin" ] && continue`) {
		t.Errorf("the link would override the application's own plugin: %s", setup)
	}
	if !strings.Contains(setup, `[ -d `+config.QaToolsDir+`/node_modules/"$plugin" ] || continue`) {
		t.Errorf("the link is not guarded on the QA copy existing: %s", setup)
	}
	// Nothing may write to the application's package.json: on a project install <OroRoot> is the
	// developer's own bind-mounted repository.
	for _, forbidden := range []string{"pnpm add", "npm install", "package.json"} {
		if strings.Contains(setup, forbidden) {
			t.Errorf("the eslint setup reaches for %q instead of linking: %s", forbidden, setup)
		}
	}
}

func TestJSInstallCommandPinsTheInstallToTheQaToolsDir(t *testing.T) {
	viper.Set("test.qa.eslint", true)
	viper.Set("test.qa.stylelint", true)
	defer func() {
		viper.Set("test.qa.eslint", nil)
		viper.Set("test.qa.stylelint", nil)
	}()

	command := JSInstallCommand(NewInstallPlan("6.1"))

	// The manifest is the whole point: without one in the QA tools directory the package manager
	// installs into the nearest ancestor that has one — the application root — and the binaries
	// BinaryPaths names never appear.
	if !strings.Contains(command, config.QaToolsDir+"/package.json") {
		t.Errorf("the install does not write a manifest in the QA tools dir: %s", command)
	}
	if !strings.Contains(command, "cd "+config.QaToolsDir+" && npm install --save-dev") {
		t.Errorf("the install does not run in the QA tools dir: %s", command)
	}
	if !strings.Contains(command, "eslint-formatter-gitlab@^5.1.0") {
		t.Errorf("the install does not carry the planned packages: %s", command)
	}
}

// TestJSToolsRunOroCommercesOwnLinters pins both halves of the decision: the binary is the
// application's, and so is the tree the linters resolve bare names against. A QA-local linter has
// to guess a version for a ruleset OroCommerce owns; the application's copy is the one that
// ruleset was written for. The QA node_modules stays second on NODE_PATH for the formatters, the
// only JS packages the QA namespace still installs.
func TestJSToolsRunOroCommercesOwnLinters(t *testing.T) {
	tools := Tools(ToolsOptions{SourceRoot: bundleRoot, AnalyzePath: bundleRoot})

	wantNodePath := "NODE_PATH=" + config.OroRootDir + "/node_modules:" + config.QaToolsDir + "/node_modules"
	for _, name := range []string{"eslint", "stylelint", "stylelint-css"} {
		tool := toolByName(t, tools, name)
		if tool.Args[0] != wantNodePath {
			t.Errorf("%s NODE_PATH = %q, want %q", name, tool.Args[0], wantNodePath)
		}
		if want := config.OroRootDir + "/node_modules/.bin/"; !strings.Contains(tool.Args[1], want) {
			t.Errorf("%s is not invoked through OroCommerce's node_modules: %v", name, tool.Args)
		}
		if !strings.Contains(strings.Join(tool.Args, " "), BinaryPaths[name]) {
			t.Errorf("%s is not invoked through %s: %v", name, BinaryPaths[name], tool.Args)
		}
		// A missing linter is a tool that could not run, and the reason has to reach the log: the
		// binary appears with the application's node_modules, which the asset install populates.
		if !strings.Contains(tool.Setup, BinaryPaths[name]) {
			t.Errorf("%s does not guard on its binary existing: %q", name, tool.Setup)
		}
	}
}

// TestEslintResolvesPluginsFromOroCommercesTree covers the flag NODE_PATH does not: ESLint resolves
// plugins named in a config through --resolve-plugins-relative-to, and the plugins the generated
// .eslintrc.yml names (eslint-plugin-oro, eslint-plugin-no-jquery) are the application's.
func TestEslintResolvesPluginsFromOroCommercesTree(t *testing.T) {
	tools := Tools(ToolsOptions{SourceRoot: bundleRoot, AnalyzePath: bundleRoot})

	args := strings.Join(toolByName(t, tools, "eslint").Args, " ")
	if want := "--resolve-plugins-relative-to " + config.OroRootDir + "/node_modules"; !strings.Contains(args, want) {
		t.Errorf("eslint is missing %s: %s", want, args)
	}
}

func TestReportScriptRunsEveryToolAndRecordsTheStatus(t *testing.T) {
	tools := []Tool{
		{Name: "phpstan", Args: []string{"phpstan", "analyze"}, Setup: "warmup", ReportFile: "/reports/qa/phpstan.json"},
		{Name: "rector", Args: []string{"rector", "process"}, WorkDir: "/var/www/oro", ReportFile: "/reports/qa/rector.json"},
	}

	script := ReportScript(tools, "/reports/qa")

	// No line may invoke two tools: chaining them is what lets one tool's findings silence the
	// next one's. The && inside rector's `cd` subshell is not a chain and must not trip this.
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(line, "phpstan analyze") && strings.Contains(line, "rector process") {
			t.Errorf("two tools share one line, so the first failure hides the second:\n%s", line)
		}
	}
	for _, want := range []string{
		"mkdir -p /reports/qa",
		"> /reports/qa/phpstan.json",
		"> /reports/qa/rector.json",
		"(cd /var/www/oro && rector process)",
		"status=1",
		"/reports/qa/" + StatusFile,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script is missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "warmup > /reports/qa/phpstan.json") {
		t.Error("the setup line must not be redirected into the report file: its output is not JSON")
	}
}

func TestReportScriptPassesTheReportFileAsAnEnvVarWhenTheToolWritesItItself(t *testing.T) {
	script := ReportScript([]Tool{
		{Name: "eslint", Args: []string{"npx", "eslint", "src"}, ReportFile: "/reports/qa/eslint.json", ReportEnv: "ESLINT_CODE_QUALITY_REPORT"},
	}, "/reports/qa")

	if !strings.Contains(script, "ESLINT_CODE_QUALITY_REPORT=/reports/qa/eslint.json npx eslint src") {
		t.Errorf("the formatter's output file must be passed in the environment:\n%s", script)
	}
	if strings.Contains(script, "> /reports/qa/eslint.json") {
		t.Errorf("stdout must not be redirected for a tool that writes the report itself; that would capture its human output:\n%s", script)
	}
}

func TestReportScriptNeverFails(t *testing.T) {
	script := ReportScript([]Tool{
		{Name: "phpstan", Args: []string{"phpstan", "analyze"}, ReportFile: "/reports/qa/phpstan.json"},
	}, "/reports/qa")

	if !strings.HasSuffix(strings.TrimSpace(script), "exit 0") {
		t.Errorf("the script must end on exit 0 so the container stays readable when tools find violations:\n%s", script)
	}
}

func TestScriptIsUnchangedWithoutAReport(t *testing.T) {
	script := Script([]Tool{
		{Name: "phpstan", Args: []string{"phpstan", "analyze"}},
		{Name: "rector", Args: []string{"rector", "process"}},
	})

	if !strings.Contains(script, "&&") {
		t.Errorf("Script must keep chaining with && so a failure still stops the run:\n%s", script)
	}
	if strings.Contains(script, "status=") {
		t.Errorf("Script must not grow the report mode's status handling:\n%s", script)
	}
}

func TestSharedVendorScriptPreparesTheNamespaceBeforePatchingIt(t *testing.T) {
	script := SharedVendorScript()

	// The manifest has to exist before the patch reads it, and the bootstrap before Composer
	// dumps an autoloader that includes it.
	steps := []string{
		config.QaToolsDir + "/composer.json",
		config.QaToolsDir + "/" + SharedAutoloadRelPath,
		"php /tmp/orobox-qa-manifest.php",
	}
	at := -1
	for _, step := range steps {
		next := strings.Index(script, step)
		if next < 0 {
			t.Fatalf("script missing %q: %s", step, script)
		}
		if next < at {
			t.Errorf("script step %q is out of order: %s", step, script)
		}
		at = next
	}

	// A namespace that already carries a committed manifest keeps it: the manifest pins the tool
	// versions on purpose, and the patch only edits what it has to.
	if !strings.Contains(script, `[ -f `+config.QaToolsDir+`/composer.json ] || printf '{"name":"orobox/qa-tools"}'`) {
		t.Errorf("script overwrites an existing manifest: %s", script)
	}
}

// TestSharedAutoloadBootstrapTakesBackTheLoaderOrder guards the two lines the whole shared tree
// rests on: Composer prepends its loader, and which end it really belongs at depends on the tool.
func TestSharedAutoloadBootstrapTakesBackTheLoaderOrder(t *testing.T) {
	for _, want := range []string{
		config.OroRootDir + "/vendor/autoload.php",
		"if (!is_file($autoload)) {",
		"spl_autoload_unregister([$loader, 'loadClass']);",
		"$loader->register(getenv('" + SharedAutoloadPrependEnv + "') === '1');",
	} {
		if !strings.Contains(sharedAutoloadPHP, want) {
			t.Errorf("bootstrap missing %q: %s", want, sharedAutoloadPHP)
		}
	}
}

// TestOnlyPhpstanPutsTheApplicationTreeFirst pins the asymmetry down. PHPStan boots the kernel, so
// it needs the application's copy of every class the dumped container inline-requires; the other
// tools never load that container and would lose their newer dependencies to it.
func TestOnlyPhpstanPutsTheApplicationTreeFirst(t *testing.T) {
	for _, tool := range Tools(ToolsOptions{SourceRoot: "/src/root", AnalyzePath: "/src/root/src", Mode: ModeCheck}) {
		args := strings.Join(tool.Args, " ")
		prepends := strings.Contains(args, SharedAutoloadPrependEnv+"=1")

		if tool.Name == "phpstan" {
			if !prepends {
				t.Errorf("phpstan does not put the application tree first: %s", args)
			}
			// The variable has to lead the command line, or the shell reads it as the binary.
			if tool.Args[0] != SharedAutoloadPrependEnv+"=1" {
				t.Errorf("the environment variable must be the first word: %s", args)
			}
			continue
		}
		if prepends {
			t.Errorf("%s must keep the QA tree first: %s", tool.Name, args)
		}
	}
}

func TestComposerInstallCommandReResolvesOnceAfterTheManifestChanged(t *testing.T) {
	cmd := ComposerInstallCommand([]string{"algoritma/php-coding-standards:*"})
	marker := config.QaToolsDir + "/" + ManifestDirtyFile

	for _, want := range []string{
		"elif [ -f " + marker + " ]; then composer bin qa update -W --no-interaction --no-progress",
		"rm -f " + marker,
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q: %s", want, cmd)
		}
	}

	// A stale lock is only a reason to re-resolve, never a reason to skip the plain install: the
	// marker branch has to sit between the require branch and the install fallback.
	update := strings.Index(cmd, "composer bin qa update")
	install := strings.Index(cmd, "composer bin qa install")
	if update < 0 || install < 0 || update > install {
		t.Errorf("update must precede the install fallback: %s", cmd)
	}
}

// TestManifestPatchHandsSharedPackagesToTheApplicationTree runs the generated patch through php
// against a manifest shaped like the ones already committed in projects: the shared packages are
// pinned in require-dev, which is what installs the duplicate copy PHPStan then fatals on.
func TestManifestPatchHandsSharedPackagesToTheApplicationTree(t *testing.T) {
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("php not available")
	}

	dir := t.TempDir()
	qaDir := filepath.Join(dir, "vendor-bin", "qa")
	installedPath := filepath.Join(dir, "vendor", "composer", "installed.json")
	for _, d := range []string{qaDir, filepath.Dir(installedPath)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	manifestPath := filepath.Join(qaDir, "composer.json")
	manifest := `{"name":"orobox/qa-tools","require-dev":{"algoritma/php-coding-standards":"*",` +
		`"symfony/console":"^6.4","symfony/service-contracts":"^3.0","psr/log":"^2"}}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	// psr/log is deliberately absent: a package the application does not ship has to keep its
	// pinned requirement, because the QA namespace is then the only place it can come from.
	installed := `{"packages":[{"name":"symfony/console","version":"v6.4.43"},` +
		`{"name":"symfony/service-contracts","version":"v3.7.1"}]}`
	if err := os.WriteFile(installedPath, []byte(installed), 0o644); err != nil {
		t.Fatal(err)
	}

	// QaToolsDir is replaced first: it has OroRootDir as its prefix.
	patch := strings.ReplaceAll(manifestPatchPHP(), config.QaToolsDir, qaDir)
	patch = strings.ReplaceAll(patch, config.OroRootDir, dir)
	patchPath := filepath.Join(dir, "patch.php")
	if err := os.WriteFile(patchPath, []byte(patch), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func() {
		out, err := exec.Command("php", patchPath).CombinedOutput()
		if err != nil {
			t.Fatalf("running the manifest patch: %v: %s", err, out)
		}
	}
	run()

	var patched struct {
		Replace    map[string]string `json:"replace"`
		RequireDev map[string]string `json:"require-dev"`
		Autoload   struct {
			Files []string `json:"files"`
		} `json:"autoload"`
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &patched); err != nil {
		t.Fatalf("patched manifest is not valid JSON: %v: %s", err, raw)
	}

	// The constraint is the patch line the application ships, not the point release: an exact
	// version makes a QA package that wants a later patch of the same line unsatisfiable, and
	// Composer resolves that by silently installing an older release of the package that asked.
	for name, want := range map[string]string{"symfony/console": "6.4.*", "symfony/service-contracts": "3.7.*"} {
		if got := patched.Replace[name]; got != want {
			t.Errorf("replace[%s] = %q, want %q", name, got, want)
		}
		if _, still := patched.RequireDev[name]; still {
			t.Errorf("%s is still required: a replaced package must not be installed twice", name)
		}
	}
	if _, ok := patched.Replace["psr/log"]; ok {
		t.Error("psr/log is not installed in the application tree, so it must keep its requirement")
	}
	if got := patched.RequireDev["psr/log"]; got != "^2" {
		t.Errorf("require-dev[psr/log] = %q, want the pin to survive", got)
	}
	if got := patched.RequireDev["algoritma/php-coding-standards"]; got != "*" {
		t.Errorf("require-dev[algoritma/php-coding-standards] = %q, want the tool requirement untouched", got)
	}
	if len(patched.Autoload.Files) != 1 || patched.Autoload.Files[0] != SharedAutoloadRelPath {
		t.Errorf("autoload.files = %v, want only %q", patched.Autoload.Files, SharedAutoloadRelPath)
	}

	marker := filepath.Join(qaDir, ManifestDirtyFile)
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the patch changed the manifest but wrote no marker, so the stale lock would fail the install: %v", err)
	}

	// Idempotence: a second run has nothing to change, so it must not ask for another
	// re-resolution — that is what keeps every later `orobox qa-init` lock-driven.
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	run()
	if _, err := os.Stat(marker); err == nil {
		t.Error("a no-op patch still marked the manifest dirty")
	}
}

// TestKernelLoadersBootTheEnvironmentBeforeTheKernel guards the reason PHPStan could reach the
// wrong database: `bin/console` boots the dotenv files through symfony/runtime, and a loader that
// instantiates the kernel itself has to do the same or every %env(...)% falls back to its default.
func TestKernelLoadersBootTheEnvironmentBeforeTheKernel(t *testing.T) {
	loaders := map[string]string{
		"console-application.php": consoleApplicationLoaderPHP(EnvDev),
		"object-manager.php":      objectManagerLoaderPHP(EnvTest),
	}

	for name, php := range loaders {
		for _, want := range []string{
			// The file to read is the project's, not Symfony's default: Oro renames it.
			`$composer['extra']['runtime'] ?? []`,
			`$runtime['dotenv_path'] ?? '.env'`,
			`$runtime['env_var_name'] ?? 'APP_ENV'`,
			`$runtime['debug_var_name'] ?? 'APP_DEBUG'`,
			`->bootEnv($dotenvPath)`,
			// A project without symfony/dotenv, or without the file, must still analyse.
			`is_file($dotenvPath) && class_exists(\Symfony\Component\Dotenv\Dotenv::class)`,
		} {
			if !strings.Contains(php, want) {
				t.Errorf("%s missing %q: %s", name, want, php)
			}
		}

		boot := strings.Index(php, "bootEnv(")
		kernel := strings.Index(php, "new "+oroKernelClass+"(")
		if boot < 0 || kernel < 0 || boot > kernel {
			t.Errorf("%s instantiates the kernel before the environment is booted: %s", name, php)
		}
	}

	// The environment the kernel boots still has to match the warmed cache PHPStan reads.
	if !strings.Contains(loaders["object-manager.php"], `new `+oroKernelClass+`('test', true)`) {
		t.Error("object-manager.php does not boot the environment it was generated for")
	}
}

func TestReportScriptRecordsEachToolsOwnExitCode(t *testing.T) {
	script := ReportScript([]Tool{
		{Name: "phpstan", Args: []string{"phpstan", "analyze"}, Setup: "warmup", ReportFile: "/reports/qa/phpstan.json"},
		{Name: "rector", Args: []string{"rector", "process"}, ReportFile: "/reports/qa/rector.json"},
	}, "/reports/qa")

	// The per-tool code is what separates "found violations" from "could not run": both exit
	// non-zero, and only the report next to the code tells them apart.
	for _, want := range []string{
		`"$code" > /reports/qa/` + ToolStatusFile("phpstan"),
		`"$code" > /reports/qa/` + ToolStatusFile("rector"),
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script is missing %q:\n%s", want, script)
		}
	}

	// A tool whose setup failed must still be recorded as failed, not skipped silently.
	if !strings.Contains(script, "if warmup; then") || !strings.Contains(script, "else false; fi") {
		t.Errorf("a failed setup must give the line a non-zero code of its own:\n%s", script)
	}
}

// TestJSInstallCommandKeepsThePnpmStoreInsideTheQaToolsDir covers the failure that made `orobox qa`
// report eslint, stylelint and stylelint-css as tools that could not run on Oro 7.0 — the only line
// installed with pnpm:
//
//	Error: Cannot find module '/var/www/oro/node_modules/.pnpm/eslint@8.57.1/node_modules/eslint/bin/eslint.js'
//
// pnpm resolves its lockfile and virtual store against the workspace root, not the working
// directory, and OroCommerce ships a package.json at the application root. So `cd vendor-bin/qa &&
// pnpm add` left only symlinks in vendor-bin/qa/node_modules pointing into <OroRoot>/node_modules,
// and anything that rewrote the application's own node_modules afterwards — the QA step's Oro
// install runs between the two — pruned the entries those symlinks named. The linters have since
// moved to OroCommerce's own installation, but the formatters still live here and are pruned the
// same way, which reports the same "could not run" for a linter that ran fine.
// TestOroLinterInstallCommandIsShellAndOnlyRunsWhenNeeded pins the three properties the pipeline
// relies on: it is valid POSIX shell (it is generated, not written by hand), it does nothing when
// the linters are already installed, and it is empty for a tool list that needs no node_modules.
// TestJSInstallCommandRebuildsANodeModulesItCannotUse covers the failure that stopped the pipeline's
// qa-tools layer outright:
//
//	ERR_PNPM_UNEXPECTED_STORE
//	The dependencies at ".../vendor-bin/qa/node_modules" are currently linked from the store at
//	"/var/www/oro/.pnpm-store/v10". pnpm now wants to use the store at "/cache/js/pnpm/v10".
//
// The runtime image bakes its QA tree against a store inside the image; the pipeline points pnpm at
// a cache volume. pnpm refuses rather than relinking, and the directory holds nothing worth saving,
// so the install drops it and runs again.
func TestJSInstallCommandRebuildsANodeModulesItCannotUse(t *testing.T) {
	viper.Set("test.qa.eslint", true)
	viper.Set("test.qa.stylelint", true)
	defer func() {
		viper.Set("test.qa.eslint", nil)
		viper.Set("test.qa.stylelint", nil)
	}()

	command := JSInstallCommand(NewInstallPlan("7.0"))

	if out, err := exec.Command("sh", "-n", "-c", command).CombinedOutput(); err != nil {
		t.Errorf("the generated command is not valid shell: %v\n%s\n%s", err, out, command)
	}
	if !strings.Contains(command, "rm -rf "+config.QaToolsDir+"/node_modules") {
		t.Errorf("a node_modules pnpm refuses is never rebuilt:\n%s", command)
	}
	// The rebuild is the fallback, not the rule: a reusable tree must be installed into as it is.
	if first, retry := strings.Index(command, "pnpm add"), strings.Index(command, "rm -rf "+config.QaToolsDir+"/node_modules"); first < 0 || retry < first {
		t.Errorf("the install wipes node_modules before trying to use it:\n%s", command)
	}
	if strings.Count(command, "pnpm add") != 2 {
		t.Errorf("the install must try once and retry once, got %d attempts:\n%s", strings.Count(command, "pnpm add"), command)
	}
}

// TestOroLinterInstallCommandFallsBackToAForcedPnpmInstall covers the same store refusal on the
// application's own tree, where the answer is pnpm's --force — refetch and relink — rather than
// deleting a node_modules the whole application is served from.
func TestOroLinterInstallCommandFallsBackToAForcedPnpmInstall(t *testing.T) {
	tools := Tools(ToolsOptions{SourceRoot: bundleRoot, AnalyzePath: bundleRoot})

	command := OroLinterInstallCommand("pnpm", tools)
	if !strings.Contains(command, "pnpm install --no-frozen-lockfile --force") {
		t.Errorf("a store mismatch on OroCommerce's tree has no way out:\n%s", command)
	}
	if strings.Contains(command, "rm -rf "+config.OroRootDir+"/node_modules") {
		t.Errorf("the application's node_modules must not be deleted:\n%s", command)
	}
}

func TestOroLinterInstallCommandIsShellAndOnlyRunsWhenNeeded(t *testing.T) {
	tools := Tools(ToolsOptions{SourceRoot: bundleRoot, AnalyzePath: bundleRoot})

	command := OroLinterInstallCommand("npm", tools)
	if command == "" {
		t.Fatal("the full tool list needs OroCommerce's node_modules, got no command")
	}
	if out, err := exec.Command("sh", "-n", "-c", command).CombinedOutput(); err != nil {
		t.Errorf("the generated command is not valid shell: %v\n%s\n%s", err, out, command)
	}

	// One `test` is the whole cost when the tree is already there, which is the full-install path.
	if !strings.Contains(command, "[ -x "+BinaryPaths["eslint"]+" ] && [ -x "+BinaryPaths["stylelint"]+" ]") {
		t.Errorf("the install is not guarded on the binaries already existing:\n%s", command)
	}
	if !strings.Contains(command, "cd "+config.OroRootDir+" && npm install") {
		t.Errorf("the install does not run against OroCommerce's manifest:\n%s", command)
	}
	// pnpm gets the manifest the asset install regenerates, against a lockfile it does not.
	if pnpm := OroLinterInstallCommand("pnpm", tools); !strings.Contains(pnpm, "pnpm install --no-frozen-lockfile") {
		t.Errorf("pnpm would abort on a stale lockfile:\n%s", pnpm)
	}

	// PHP tools resolve to binaries outside node_modules, so a PHP-only list has nothing to
	// prepare and must not pay for an npm install.
	if got := OroLinterInstallCommand("npm", []Tool{toolByName(t, tools, "phpstan")}); got != "" {
		t.Errorf("a PHP-only tool list prepares node_modules: %q", got)
	}
}

func TestJSInstallCommandKeepsThePnpmStoreInsideTheQaToolsDir(t *testing.T) {
	viper.Set("test.qa.eslint", true)
	viper.Set("test.qa.stylelint", true)
	defer func() {
		viper.Set("test.qa.eslint", nil)
		viper.Set("test.qa.stylelint", nil)
	}()

	plan := NewInstallPlan("7.0")
	if plan.JSManager != "pnpm" {
		t.Fatalf("7.0 is expected to install with pnpm, got %q", plan.JSManager)
	}

	command := JSInstallCommand(plan)

	// The trailing space matters: --ignore-workspace-root-check, which this replaces, contains the
	// flag name as a prefix and would satisfy a bare substring check while changing nothing.
	if !strings.Contains(command, "--ignore-workspace ") {
		t.Errorf("the install still resolves the application root as its workspace: %s", command)
	}
	// Belt and braces: --ignore-workspace decides where the lockfile goes, this decides where the
	// packages themselves land, and only the second one is what the .bin symlinks point into.
	if !strings.Contains(command, "--virtual-store-dir="+config.QaToolsDir+"/node_modules/.pnpm") {
		t.Errorf("the virtual store is not pinned inside the QA tools dir: %s", command)
	}
}

// TestOroLinterInstallCommandAddsThePluginsOroDeclaresButDoesNotShip covers the ESLint failure that
// followed switching the linters to OroCommerce's own installation:
//
//	ESLint couldn't find the plugin "eslint-plugin-no-jquery".
//	(The package "eslint-plugin-no-jquery" was not found when loaded as a Node module from the
//	directory "/var/www/oro/node_modules".)
//
// It is an upstream gap, not a missing install: OroCommerce's .eslintrc.yml extends
// "plugin:no-jquery/deprecated" and lists no-jquery under `plugins`, while the package.json it
// generates — the file `<manager> install` here works from — never declares
// eslint-plugin-no-jquery. Installing that manifest therefore cannot satisfy the configuration
// written against it, so the packages it omits are added on top of it.
//
// The plugin has to land in the application's node_modules and nowhere else: ESLint resolves
// plugins through --resolve-plugins-relative-to, which Tools points at that one directory, so a
// copy in the QA namespace would not be found.
func TestOroLinterInstallCommandAddsThePluginsOroDeclaresButDoesNotShip(t *testing.T) {
	tools := Tools(ToolsOptions{SourceRoot: bundleRoot, AnalyzePath: bundleRoot})

	for _, manager := range []string{"npm", "pnpm"} {
		command := OroLinterInstallCommand(manager, tools)

		if out, err := exec.Command("sh", "-n", "-c", command).CombinedOutput(); err != nil {
			t.Errorf("%s: the generated command is not valid shell: %v\n%s\n%s", manager, err, out, command)
		}
		// Guarded on the package, not on the manifest install having run: the binaries can be
		// there already — a full install, a cached tree — and that path skips the install
		// entirely, which is exactly the run that failed.
		if !strings.Contains(command, "[ -d "+config.OroRootDir+"/node_modules/eslint-plugin-no-jquery ]") {
			t.Errorf("%s: the missing plugin is never checked for:\n%s", manager, command)
		}
		// And guarded on the configuration naming it: 5.1's .eslintrc.yml does not, and adding a
		// plugin no configuration references would change the application's manifest for nothing.
		if !strings.Contains(command, "grep -q 'no-jquery' "+config.OroRootDir+"/.eslintrc.yml") {
			t.Errorf("%s: the plugin is installed without asking whether the line needs it:\n%s", manager, command)
		}
		if !strings.Contains(command, config.OroRootDir+" && "+manager+" add") &&
			!strings.Contains(command, config.OroRootDir+" && "+manager+" install --save-dev") {
			t.Errorf("%s: the missing plugin is never installed:\n%s", manager, command)
		}
	}

	// A tool list without ESLint has no configuration naming the plugin, so it must not pay for it.
	stylelintOnly := []Tool{toolByName(t, tools, "stylelint")}
	if got := OroLinterInstallCommand("npm", stylelintOnly); strings.Contains(got, "no-jquery") {
		t.Errorf("a stylelint-only list installs an ESLint plugin:\n%s", got)
	}
}

// TestRectorReportsItsOwnJSONRatherThanGitLabs covers the failure that made the whole QA run
// unreadable even though every tool had run:
//
//	the rector report is not valid GitLab Code Quality JSON: invalid character 'W' looking for
//	beginning of value
//
// The 'W' is the "[WARNING]" of the Symfony block Rector prints for its deprecated `gitlab` output
// format, on the same stdout the report file captures. It cannot be silenced — `process` defines no
// verbosity option, so `--quiet` aborts the command with "The "--quiet" option does not exist" and
// Rector never runs — and the format is being removed in the next minor anyway. The `json` format
// raises no warning and Rector forces its console output quiet for it, so the file holds the
// document alone. report.MergeCodeQuality converts it.
func TestRectorReportsItsOwnJSONRatherThanGitLabs(t *testing.T) {
	gitlab := Tools(ToolsOptions{SourceRoot: bundleRoot, AnalyzePath: bundleRoot, Report: ReportGitLab})
	rector := strings.Join(toolByName(t, gitlab, "rector").Args, " ")

	if !strings.Contains(rector, "--output-format=json") {
		t.Errorf("rector does not report the format it can write cleanly: %s", rector)
	}
	if strings.Contains(rector, "--output-format=gitlab") {
		t.Errorf("rector's deprecation warning still reaches the report file: %s", rector)
	}
	if strings.Contains(rector, "--quiet") {
		t.Errorf("rector is handed an option its process command does not define: %s", rector)
	}

	// Without a report the console output is what the user reads, and Rector's own format is what
	// says anything readable there.
	plain := strings.Join(toolByName(t, Tools(ToolsOptions{SourceRoot: bundleRoot, AnalyzePath: bundleRoot}), "rector").Args, " ")
	if strings.Contains(plain, "--output-format") {
		t.Errorf("rector reports a machine format to the terminal: %s", plain)
	}
}
