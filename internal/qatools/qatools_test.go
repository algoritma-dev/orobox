package qatools

import (
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
			ContainerXMLPath(tc.env),
			SymfonyConfigDir(tc.env),
			"ORO_DEBUG=1 php " + config.OroRootDir + "/bin/console " + tc.warmup,
		} {
			if !strings.Contains(setup, want) {
				t.Errorf("%s: phpstan setup missing %q: %s", tc.env, want, setup)
			}
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

func TestComposerInstallCommand(t *testing.T) {
	cmd := ComposerInstallCommand([]string{"algoritma/php-coding-standards:*", "symfony/console:^6.4"})

	for _, want := range []string{
		config.QaToolsDir + "/composer.json",
		"composer bin qa install --no-interaction --no-progress",
		"composer bin qa require --dev -W --no-interaction algoritma/php-coding-standards:* symfony/console:^6.4",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q: %s", want, cmd)
		}
	}

	// A committed manifest must win, otherwise `require pkg:*` rewrites the pinned versions.
	install := strings.Index(cmd, "composer bin qa install")
	require := strings.Index(cmd, "composer bin qa require")
	if install < 0 || require < 0 || install > require {
		t.Errorf("install must be the then-branch, require the fallback: %s", cmd)
	}
}

func TestToolsReportModeAddsTheGitLabFlag(t *testing.T) {
	tools := Tools(ToolsOptions{
		SourceRoot:  config.OroRootDir,
		AnalyzePath: config.OroRootDir + "/src",
		Mode:        ModeCheck,
		Report:      ReportGitLab,
		ReportDir:   "/reports/qa",
	})

	want := map[string]string{
		"phpstan":       "--error-format=gitlab",
		"rector":        "--output-format=gitlab",
		"php-cs-fixer":  "--format=gitlab",
		"twig-cs-fixer": "--report=gitlab",
		// ESLint 8 resolves a bare --format=gitlab against its own installation and fails, so the
		// formatter is addressed by absolute path.
		"eslint":        "--format=" + config.QaToolsDir + "/node_modules/eslint-formatter-gitlab",
		"stylelint":     "--custom-formatter=" + config.QaToolsDir + "/node_modules/stylelint-formatter-gitlab",
		"stylelint-css": "--custom-formatter=" + config.QaToolsDir + "/node_modules/stylelint-formatter-gitlab",
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

func TestInstallPlanAlwaysCarriesTheGitLabFormatters(t *testing.T) {
	viper.Set("test.qa.eslint", true)
	viper.Set("test.qa.stylelint", true)
	defer func() {
		viper.Set("test.qa.eslint", nil)
		viper.Set("test.qa.stylelint", nil)
	}()

	plan := NewInstallPlan("6.1")
	packages := strings.Join(plan.JSPackages, " ")

	for _, pkg := range []string{"eslint-formatter-gitlab@^5.1.0", "stylelint-formatter-gitlab"} {
		if !strings.Contains(packages, pkg) {
			t.Errorf("%s is missing from the JS packages: %s", pkg, packages)
		}
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
