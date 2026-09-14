package qatools

import (
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
)

const stagedRoot = config.OroRootDir + "/bundles/Acme"

func stagedTools(t *testing.T) []Tool {
	t.Helper()
	return Tools(ToolsOptions{SourceRoot: stagedRoot, AnalyzePath: stagedRoot, Mode: ModeCheck})
}

func namesOf(tools []Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// TestRestrictRoutesByExtension: each tool gets the staged files it can read, and only those.
func TestRestrictRoutesByExtension(t *testing.T) {
	files := []string{
		stagedRoot + "/src/Entity.php",
		stagedRoot + "/src/Resources/views/list.html.twig",
		stagedRoot + "/src/Resources/public/js/app.js",
		stagedRoot + "/src/Resources/public/scss/main.scss",
		stagedRoot + "/src/Resources/public/css/print.css",
		stagedRoot + "/README.md",
	}

	wants := map[string]string{
		"rector":        stagedRoot + "/src/Entity.php",
		"php-cs-fixer":  stagedRoot + "/src/Entity.php",
		"twig-cs-fixer": stagedRoot + "/src/Resources/views/list.html.twig",
		"eslint":        stagedRoot + "/src/Resources/public/js/app.js",
		"stylelint":     stagedRoot + "/src/Resources/public/scss/main.scss",
		"stylelint-css": stagedRoot + "/src/Resources/public/css/print.css",
	}

	restricted := Restrict(stagedTools(t), files)

	for name, want := range wants {
		args := strings.Join(toolByName(t, restricted, name).Args, " ")
		if !strings.Contains(args, "'"+want+"'") {
			t.Errorf("%s is missing the staged %s: %s", name, want, args)
		}
		// Every other staged file is one this tool cannot read — README.md included, which no tool
		// in the set has anything to say about.
		for _, other := range files {
			if other == want {
				continue
			}
			if strings.Contains(args, "'"+other+"'") {
				t.Errorf("%s was handed %s: %s", name, other, args)
			}
		}
	}
}

// TestRestrictLeavesPhpstanWhole is the rule the whole feature turns on: PHPStan analyses the
// tree, never the commit, because a changed class breaks the callers the commit did not touch.
func TestRestrictLeavesPhpstanWhole(t *testing.T) {
	tools := stagedTools(t)
	before := strings.Join(toolByName(t, tools, "phpstan").Args, " ")

	restricted := Restrict(tools, []string{stagedRoot + "/src/Entity.php"})
	after := strings.Join(toolByName(t, restricted, "phpstan").Args, " ")

	if before != after {
		t.Errorf("Restrict changed PHPStan's arguments:\n before: %s\n  after: %s", before, after)
	}
}

// TestRestrictDropsToolsWithNothingToCheck: an empty file list means "the whole tree" to some of
// these CLIs, so a tool with no staged input is removed instead of run.
func TestRestrictDropsToolsWithNothingToCheck(t *testing.T) {
	restricted := Restrict(stagedTools(t), []string{stagedRoot + "/src/Resources/views/list.html.twig"})

	if got := namesOf(restricted); len(got) != 2 || got[0] != "phpstan" || got[1] != "twig-cs-fixer" {
		t.Errorf("Restrict kept %v, want [phpstan twig-cs-fixer]", got)
	}
}

func TestRestrictWithNoStagedFiles(t *testing.T) {
	restricted := Restrict(stagedTools(t), nil)

	if got := namesOf(restricted); len(got) != 1 || got[0] != "phpstan" {
		t.Errorf("Restrict kept %v, want [phpstan]", got)
	}
}

// TestRestrictReplacesGlobs: the linters already carry a positional target, and a run given both
// the glob and the file list would walk the whole tree again.
func TestRestrictReplacesGlobs(t *testing.T) {
	files := []string{
		stagedRoot + "/src/Resources/public/js/app.js",
		stagedRoot + "/src/Resources/public/scss/main.scss",
		stagedRoot + "/src/Resources/public/css/print.css",
	}
	restricted := Restrict(stagedTools(t), files)

	for name, glob := range map[string]string{"eslint": jsTarget, "stylelint": scssTarget, "stylelint-css": cssTarget} {
		args := strings.Join(toolByName(t, restricted, name).Args, " ")
		if strings.Contains(args, glob) {
			t.Errorf("%s still carries its glob %s: %s", name, glob, args)
		}
	}
}

// TestRestrictKeepsPhpCSFixerConfiguration: without --path-mode=intersection an explicit path
// overrides the config's Finder, so a staged file the project excluded would be checked anyway.
func TestRestrictKeepsPhpCSFixerConfiguration(t *testing.T) {
	restricted := Restrict(stagedTools(t), []string{stagedRoot + "/src/Entity.php"})

	if args := strings.Join(toolByName(t, restricted, "php-cs-fixer").Args, " "); !strings.Contains(args, "--path-mode=intersection") {
		t.Errorf("php-cs-fixer is missing --path-mode=intersection: %s", args)
	}
}

// TestRestrictQuotesPaths: the tools are run as one shell line, so a checkout under a path with a
// space in it has to survive the join.
func TestRestrictQuotesPaths(t *testing.T) {
	restricted := Restrict(stagedTools(t), []string{stagedRoot + "/src/My Entity.php"})

	if args := strings.Join(toolByName(t, restricted, "rector").Args, " "); !strings.Contains(args, `'`+stagedRoot+`/src/My Entity.php'`) {
		t.Errorf("rector's path is not quoted: %s", args)
	}
}

// TestRestrictMatchesExtensionsCaseInsensitively: .PHP is a file PHP-CS-Fixer still fixes.
func TestRestrictMatchesExtensionsCaseInsensitively(t *testing.T) {
	restricted := Restrict(stagedTools(t), []string{stagedRoot + "/src/Entity.PHP"})

	if args := strings.Join(toolByName(t, restricted, "php-cs-fixer").Args, " "); !strings.Contains(args, "Entity.PHP") {
		t.Errorf("php-cs-fixer did not take the staged Entity.PHP: %s", args)
	}
}
