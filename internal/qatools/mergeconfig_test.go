package qatools

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
)

// bundleRoot is a source root that differs from OroRoot, which is what a bundle install has and
// what makes a merge of the JS configs possible at all.
const bundleRoot = config.OroRootDir + "/bundles/acme"

// decodeMerged pulls the base64 document out of a setup line and decodes it, so the tests assert
// on the rendered config rather than on its encoding.
func decodeMerged(t *testing.T, setup string) string {
	t.Helper()

	match := regexp.MustCompile(`base64 -d`).FindStringIndex(setup)
	if match == nil {
		t.Fatalf("setup line writes no base64 document: %s", setup)
	}
	quoted := regexp.MustCompile(`'([A-Za-z0-9+/=]+)'`).FindStringSubmatch(setup[:match[0]])
	if quoted == nil {
		t.Fatalf("setup line holds no quoted payload: %s", setup)
	}
	decoded, err := base64.StdEncoding.DecodeString(quoted[1])
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	return string(decoded)
}

func TestMergedConfigPrefersTheMergeOverEitherHalf(t *testing.T) {
	ref := mergedConfig("/src/root", config.QaToolsDir, "phpstan.neon", neonMerge)

	base := config.QaToolsDir + "/phpstan.neon"
	project := "/src/root/phpstan.neon"
	merged := mergedDir + "/phpstan.neon"

	// All three outcomes have to be reachable from the one expression: the container is the only
	// place that knows whether the project ships a config, and a base config only exists once
	// the tools have been installed.
	for _, want := range []string{
		"[ -f " + base + " ] && [ -f " + project + " ]; then echo " + merged,
		"elif [ -f " + project + " ]; then echo " + project,
		"else echo " + base,
	} {
		if !strings.Contains(ref.Path, want) {
			t.Errorf("config expression missing %q: %s", want, ref.Path)
		}
	}

	// The merged document is written only when there is something to merge, so neither half is
	// shadowed by a stale merge from an earlier run.
	if !strings.HasPrefix(ref.Setup, "if [ -f "+base+" ] && [ -f "+project+" ]; then mkdir -p "+mergedDir) {
		t.Errorf("setup line is not guarded by both halves existing: %s", ref.Setup)
	}
	if !strings.Contains(ref.Setup, "> "+merged) {
		t.Errorf("setup line does not write the merged config: %s", ref.Setup)
	}
}

func TestMergedConfigSkipsTheMergeWhenThereIsNothingToMerge(t *testing.T) {
	// A project install roots its sources at OroRoot, so the JS tools' two halves are one file.
	ref := mergedConfig(config.OroRootDir, config.OroRootDir, ".eslintrc.yml", yamlExtendsMerge)
	if ref.Path != config.OroRootDir+"/.eslintrc.yml" || ref.Setup != "" {
		t.Errorf("identical halves must resolve to the plain file, got %+v", ref)
	}

	// The ignore files keep the older project-or-base fallback: their patterns resolve against
	// the directory of the file holding them, so a merged copy elsewhere re-anchors them.
	ignore := mergedConfig(bundleRoot, config.OroRootDir, ".eslintignore", nil)
	if ignore.Setup != "" {
		t.Errorf("an unmergeable config must generate no setup line: %s", ignore.Setup)
	}
	for _, want := range []string{bundleRoot + "/.eslintignore", config.OroRootDir + "/.eslintignore"} {
		if !strings.Contains(ignore.Path, want) {
			t.Errorf("ignore fallback missing %q: %s", want, ignore.Path)
		}
	}
}

func TestMergedDocumentsLayerProjectOverBase(t *testing.T) {
	tools := Tools(ToolsOptions{SourceRoot: bundleRoot, AnalyzePath: bundleRoot, Mode: ModeCheck})

	for _, tc := range []struct {
		tool string
		base string
		want []string
	}{
		{
			tool: "phpstan",
			base: config.QaToolsDir + "/phpstan.neon",
			want: []string{"includes:", config.QaToolsDir + "/phpstan.neon", bundleRoot + "/phpstan.neon"},
		},
		{
			tool: "eslint",
			base: config.OroRootDir + "/.eslintrc.yml",
			want: []string{"extends:", config.OroRootDir + "/.eslintrc.yml", bundleRoot + "/.eslintrc.yml"},
		},
		{
			tool: "stylelint",
			base: config.OroRootDir + "/.stylelintrc.yml",
			want: []string{"extends:", config.OroRootDir + "/.stylelintrc.yml", bundleRoot + "/.stylelintrc.yml"},
		},
		{
			tool: "stylelint-css",
			base: config.OroRootDir + "/.stylelintrc-css.yml",
			want: []string{"extends:", config.OroRootDir + "/.stylelintrc-css.yml", bundleRoot + "/.stylelintrc-css.yml"},
		},
		{
			tool: "rector",
			base: config.QaToolsDir + "/rector.php",
			want: []string{"RectorConfig $rectorConfig", "is_callable($config)", "cannot merge the Rector config"},
		},
		{
			tool: "php-cs-fixer",
			base: config.QaToolsDir + "/.php-cs-fixer.dist.php",
			want: []string{"ConfigInterface", "array_merge($base->getRules(), [", "], $project->getRules())", "setRiskyAllowed"},
		},
		{
			tool: "twig-cs-fixer",
			base: config.QaToolsDir + "/.twig-cs-fixer.php",
			want: []string{"new Ruleset()", "$rules[$rule::class] = $rule;", "setRuleset($ruleset)"},
		},
	} {
		doc := decodeMerged(t, toolByName(t, tools, tc.tool).Setup)

		for _, want := range tc.want {
			if !strings.Contains(doc, want) {
				t.Errorf("%s merged config missing %q:\n%s", tc.tool, want, doc)
			}
		}

		// Precedence is positional in every one of these documents: the base is applied first and
		// the project's file last, so the project wins whatever the two disagree on.
		baseAt := strings.Index(doc, tc.base)
		projectAt := strings.Index(doc, bundleRoot)
		if baseAt < 0 || projectAt < 0 || baseAt > projectAt {
			t.Errorf("%s merged config does not apply the base before the project:\n%s", tc.tool, doc)
		}
	}
}

func TestPhpstanSetupWritesTheMergedConfigBeforeWarmingTheCache(t *testing.T) {
	setup := toolByName(t, Tools(ToolsOptions{SourceRoot: bundleRoot, AnalyzePath: bundleRoot, Env: EnvDev, Mode: ModeCheck}), "phpstan").Setup

	merge := strings.Index(setup, mergedDir+"/phpstan.neon")
	warmup := strings.Index(setup, "cache:warmup")
	if merge < 0 || warmup < 0 || merge > warmup {
		t.Fatalf("phpstan setup must write the merged config before the warmup: %s", setup)
	}
	// Chained with && so a failed write stops the run instead of analysing against a stale config.
	if !strings.Contains(setup, "fi && ") {
		t.Errorf("phpstan setup lines are not chained: %s", setup)
	}
}

// TestMergedSetupIsValidShell runs the generated setup lines through the shell, both with and
// without a project config in place, because they end up inside `if <setup>; then` in
// ReportScript and a nested compound command there is easy to get wrong.
func TestMergedSetupIsValidShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}

	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	project := filepath.Join(dir, "project")
	for _, d := range []string{base, project} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "phpstan.neon"), []byte("parameters:\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ref := mergedConfig(project, base, "phpstan.neon", neonMerge)
	// mergedConfig addresses the real QA directory, which does not exist here.
	script := strings.ReplaceAll(ref.Setup, mergedDir, filepath.Join(dir, "merged"))
	path := strings.ReplaceAll(ref.Path, mergedDir, filepath.Join(dir, "merged"))

	run := func(t *testing.T) string {
		t.Helper()
		// The nested form ReportScript produces, so the test covers that shape too.
		out, err := exec.Command(sh, "-c", "if "+script+"; then echo "+path+"; else echo SETUP-FAILED; fi").Output()
		if err != nil {
			t.Fatalf("setup line is not valid shell: %v", err)
		}
		return strings.TrimSpace(string(out))
	}

	if got := run(t); got != filepath.Join(base, "phpstan.neon") {
		t.Errorf("with no project config the base one must be used, got %q", got)
	}

	if err := os.WriteFile(filepath.Join(project, "phpstan.neon"), []byte("parameters:\n    level: 8\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mergedFile := filepath.Join(dir, "merged", "phpstan.neon")
	if got := run(t); got != mergedFile {
		t.Errorf("with both configs present the merge must be used, got %q", got)
	}
	written, err := os.ReadFile(mergedFile)
	if err != nil {
		t.Fatalf("merged config was not written: %v", err)
	}
	if !strings.Contains(string(written), filepath.Join(project, "phpstan.neon")) {
		t.Errorf("merged config does not include the project's: %s", written)
	}
}

// TestMergedPHPWrappersAreValidPHP lints the three PHP wrappers. They are strings in Go, so a
// syntax error in one only surfaces when a developer runs the tool in the container.
func TestMergedPHPWrappersAreValidPHP(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php not available")
	}

	for name, render := range map[string]mergeRenderer{
		"rector.php":             rectorMerge,
		".php-cs-fixer.dist.php": phpCSFixerMerge,
		".twig-cs-fixer.php":     twigCSFixerMerge,
	} {
		file := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(file, []byte(render("/base/"+name, "/project/"+name)), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(php, "-l", file).CombinedOutput(); err != nil {
			t.Errorf("%s wrapper is not valid PHP: %v\n%s", name, err, out)
		}
	}
}

// TestRectorAlwaysUsesTheGeneratedWrapper covers the regression the skip list exists for: with the
// older project-or-base fallback, a checkout without its own rector.php ran the base config
// directly and Rector reported OroCommerce's generated kernel on every run — which is exactly what
// a project looks like before `orobox qa-init`, and what the pipeline engine always looks like.
func TestRectorAlwaysUsesTheGeneratedWrapper(t *testing.T) {
	ref := rectorConfigRef(bundleRoot)

	if ref.Path != mergedDir+"/rector.php" {
		t.Errorf("rector config = %q, want the generated wrapper at %q", ref.Path, mergedDir+"/rector.php")
	}
	// Unconditional: no `if [ -f ... ]` guard may gate the write, or the fallback is back.
	if strings.Contains(ref.Setup, "if [ -f ") {
		t.Errorf("the wrapper must be written unconditionally: %s", ref.Setup)
	}
	if !strings.Contains(ref.Setup, "> "+mergedDir+"/rector.php") {
		t.Errorf("setup line does not write the wrapper: %s", ref.Setup)
	}

	doc := decodeMerged(t, ref.Setup)
	// Either half may be absent — the base one before the tools are installed, the project one in
	// a checkout that never ran qa-init — so neither may be required.
	if !strings.Contains(doc, "if (!is_file($file)) {") {
		t.Errorf("the wrapper requires both halves to exist:\n%s", doc)
	}
	for _, want := range oroGeneratedSources() {
		if !strings.Contains(doc, "$rectorConfig->skip([") || !strings.Contains(doc, "'"+want+"'") {
			t.Errorf("the wrapper does not skip %s:\n%s", want, doc)
		}
	}
}

// TestPhpCSFixerMergeDisablesThePHPUnitAttributeRules covers the regression in issue #11: the
// shared standard's @PHPUnit100Migration:risky set rewrote `@dataProvider` annotations into PHPUnit
// 10 attributes and removed the annotation, on applications that pin PHPUnit 9.6 and read neither.
// The overrides have to sit between the two sides — after the base so the standard cannot switch
// them on, before the project so a project on PHPUnit 10 can switch them back.
func TestPhpCSFixerMergeDisablesThePHPUnitAttributeRules(t *testing.T) {
	doc := phpCSFixerMerge(config.QaToolsDir+"/.php-cs-fixer.dist.php", bundleRoot+"/.php-cs-fixer.dist.php")

	for _, rule := range phpUnitAttributeRules() {
		if !strings.Contains(doc, "'"+rule+"' => false") {
			t.Errorf("the merged config does not disable %s:\n%s", rule, doc)
		}
	}

	want := "array_merge($base->getRules(), [" + phpDisabledRules(phpUnitAttributeRules()) + "], $project->getRules())"
	if !strings.Contains(doc, want) {
		t.Errorf("the overrides are not layered between the base and the project rules, want %q:\n%s", want, doc)
	}
}

// TestPhpCSFixerMergeExcludesTheGeneratedSources is the PHP-CS-Fixer half of the same exclusion:
// the project config is what points the finder at src/, so it is also what walks into the kernel
// OroCommerce generated.
func TestPhpCSFixerMergeExcludesTheGeneratedSources(t *testing.T) {
	doc := phpCSFixerMerge(config.QaToolsDir+"/.php-cs-fixer.dist.php", bundleRoot+"/.php-cs-fixer.dist.php")

	if !strings.Contains(doc, "$finder instanceof Finder") {
		t.Errorf("the finder narrowing is not guarded by its type:\n%s", doc)
	}
	for _, want := range oroGeneratedSources() {
		if !strings.Contains(doc, "'"+want+"'") {
			t.Errorf("the merged config does not exclude %s:\n%s", want, doc)
		}
	}
}
