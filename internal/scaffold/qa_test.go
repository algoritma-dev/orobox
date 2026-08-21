package scaffold

import (
	"os"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/spf13/viper"
)

// useRealTemplates points the package at the repository's own templates, two levels up, so the
// tests fail when the shipped PHP changes shape rather than when a fixture does.
func useRealTemplates(t *testing.T) {
	t.Helper()
	old := Templates
	Templates = os.DirFS("../..")
	t.Cleanup(func() { Templates = old })
}

func TestQaStubsForProjectSkipTheJSConfigs(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	var paths []string
	for _, artifact := range QaStubs(config.InstallTypeProject) {
		paths = append(paths, artifact.RelPath)
		if artifact.Ownership != WriteOnce {
			t.Errorf("%s is not WriteOnce", artifact.RelPath)
		}
	}

	for _, want := range []string{"phpstan.neon", "rector.php", ".php-cs-fixer.dist.php", ".twig-cs-fixer.php"} {
		if !containsString(paths, want) {
			t.Errorf("project stubs are missing %s, got %v", want, paths)
		}
	}
	// The JS base configs live at OroRoot, which for a project install *is* the source root, so a
	// stub there would overwrite Oro's own file instead of layering on it.
	for _, forbidden := range []string{".eslintrc.yml", ".stylelintrc.yml", ".stylelintrc-css.yml"} {
		if containsString(paths, forbidden) {
			t.Errorf("project stubs include %s, which would overwrite OroCommerce's own config", forbidden)
		}
	}
	// The ignore files are chosen, not merged: a stub would replace Oro's ignore list wholesale.
	for _, forbidden := range []string{".eslintignore", ".stylelintignore", ".stylelintignore-css"} {
		if containsString(paths, forbidden) {
			t.Errorf("stubs include the ignore file %s", forbidden)
		}
	}
}

func TestQaStubsForBundleIncludeTheJSConfigs(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	var paths []string
	for _, artifact := range QaStubs(config.InstallTypeBundle) {
		paths = append(paths, artifact.RelPath)
	}

	for _, want := range []string{".eslintrc.yml", ".stylelintrc.yml", ".stylelintrc-css.yml"} {
		if !containsString(paths, want) {
			t.Errorf("bundle stubs are missing %s, got %v", want, paths)
		}
	}
}

func TestQaStubsHonourDisabledTools(t *testing.T) {
	viper.Reset()
	viper.Set("test.qa.rector", false)
	viper.Set("test.qa.eslint", false)
	t.Cleanup(viper.Reset)

	var paths []string
	for _, artifact := range QaStubs(config.InstallTypeBundle) {
		paths = append(paths, artifact.RelPath)
	}

	if containsString(paths, "rector.php") {
		t.Error("a disabled Rector still produced rector.php")
	}
	if containsString(paths, ".eslintrc.yml") {
		t.Error("a disabled ESLint still produced .eslintrc.yml")
	}
	if !containsString(paths, "phpstan.neon") {
		t.Errorf("disabling two tools dropped phpstan.neon, got %v", paths)
	}
}

func TestRenderQaStubsForProject(t *testing.T) {
	useRealTemplates(t)
	viper.Reset()
	t.Cleanup(viper.Reset)

	data := QaStubDataFor(config.InstallTypeProject)

	rendered := map[string]string{}
	for _, artifact := range QaStubs(config.InstallTypeProject) {
		out, err := Render(artifact.TemplatePath, data)
		if err != nil {
			t.Fatalf("Render(%s) error = %v", artifact.TemplatePath, err)
		}
		body := string(out)
		if strings.Contains(body, leftDelim) || strings.Contains(body, rightDelim) {
			t.Errorf("%s still contains Go template delimiters", artifact.RelPath)
		}
		rendered[artifact.RelPath] = body
	}

	// The finder is the analysed tree, and for a project that tree is src/ — not the checkout
	// root, which also holds vendor/.
	fixer := rendered[".php-cs-fixer.dist.php"]
	if !strings.Contains(fixer, "__DIR__ . '/src'") {
		t.Errorf(".php-cs-fixer.dist.php does not scope the finder to src/:\n%s", fixer)
	}
	if !strings.Contains(fixer, "->setFinder($finder)") {
		t.Error(".php-cs-fixer.dist.php does not set the finder on the config")
	}

	twig := rendered[".twig-cs-fixer.php"]
	for _, want := range []string{"__DIR__ . '/templates'", "__DIR__ . '/src'", "addStandard(new TwigCsFixer())", "return $config;"} {
		if !strings.Contains(twig, want) {
			t.Errorf(".twig-cs-fixer.php is missing %q", want)
		}
	}

	// rectorMerge requires a callable; anything else throws at QA time.
	if rector := rendered["rector.php"]; !strings.Contains(rector, "return static function (RectorConfig $rectorConfig): void {") {
		t.Errorf("rector.php does not return a closure:\n%s", rector)
	}

	// An empty NEON key is null, which PHPStan rejects; the explicit empty list merges as a no-op.
	if neon := rendered["phpstan.neon"]; !strings.Contains(neon, "ignoreErrors: []") {
		t.Errorf("phpstan.neon does not declare an explicit empty ignoreErrors:\n%s", neon)
	}
}

func TestRenderQaStubsForBundle(t *testing.T) {
	useRealTemplates(t)
	viper.Reset()
	t.Cleanup(viper.Reset)

	data := QaStubDataFor(config.InstallTypeBundle)

	fixer, err := Render("templates/qa/php-cs-fixer.dist.php.tmpl", data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := string(fixer)
	// A bundle's whole checkout is the source, so the finder is __DIR__ with the installed trees
	// excluded.
	if !strings.Contains(body, "->in(__DIR__)") {
		t.Errorf("bundle .php-cs-fixer.dist.php does not scan __DIR__:\n%s", body)
	}
	if !strings.Contains(body, "->exclude(['vendor', 'node_modules'])") {
		t.Errorf("bundle .php-cs-fixer.dist.php does not exclude the installed trees:\n%s", body)
	}

	twig, err := Render("templates/qa/twig-cs-fixer.php.tmpl", data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(string(twig), "$finder->in([__DIR__]);") {
		t.Errorf("bundle .twig-cs-fixer.php does not scan __DIR__:\n%s", twig)
	}
	if !strings.Contains(string(twig), "$finder->exclude(['vendor', 'node_modules']);") {
		t.Errorf("bundle .twig-cs-fixer.php does not exclude the installed trees:\n%s", twig)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}
