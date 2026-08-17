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
	tools := Tools(config.OroRootDir, config.OroRootDir+"/src", ModeFix)

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
	tools := Tools(config.OroRootDir, config.OroRootDir+"/src", ModeCheck)

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
	tools := Tools("/src/root", "/src/root/src", ModeCheck)

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

func TestPhpstanWarmsTheTestDebugCache(t *testing.T) {
	setup := toolByName(t, Tools("/src/root", "/src/root/src", ModeCheck), "phpstan").Setup

	for _, want := range []string{
		ContainerXMLPath(),
		SymfonyConfigDir(),
		"ORO_DEBUG=1 php " + config.OroRootDir + "/bin/console cache:warmup --env=test",
	} {
		if !strings.Contains(setup, want) {
			t.Errorf("phpstan setup missing %q: %s", want, setup)
		}
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
	phpstan := PhpstanConfigScript()
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
