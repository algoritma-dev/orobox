package qatools

import (
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
)

func TestBaselinePathSitsNextToTheProjectConfig(t *testing.T) {
	if got, want := BaselinePath(config.OroRootDir), config.OroRootDir+"/phpstan-baseline.neon"; got != want {
		t.Errorf("BaselinePath = %q, want %q", got, want)
	}
}

func TestToolsBaselineModeGeneratesInsteadOfReporting(t *testing.T) {
	baseline := config.OroRootDir + "/" + BaselineFile
	tools := Tools(ToolsOptions{
		SourceRoot:  config.OroRootDir,
		AnalyzePath: config.OroRootDir + "/src",
		Mode:        ModeFix,
		Baseline:    baseline,
	})

	args := strings.Join(toolByName(t, tools, "phpstan").Args, " ")
	for _, want := range []string{"--generate-baseline=" + baseline, "--allow-empty-baseline"} {
		if !strings.Contains(args, want) {
			t.Errorf("phpstan is missing %s: %s", want, args)
		}
	}

	// The analysed path and the merged configuration still have to be there: a baseline generated
	// from a different tree, or without the project's own config, records the wrong errors.
	for _, want := range []string{"analyze", config.OroRootDir + "/src", "--configuration="} {
		if !strings.Contains(args, want) {
			t.Errorf("phpstan is missing %s: %s", want, args)
		}
	}

	// Only PHPStan takes a baseline; nothing may leak into the other tools.
	for _, tool := range tools {
		if tool.Name == "phpstan" {
			continue
		}
		if strings.Contains(strings.Join(tool.Args, " "), "baseline") {
			t.Errorf("%s was given a baseline flag: %v", tool.Name, tool.Args)
		}
	}
}

func TestToolsBaselineModeReplacesTheReportFormat(t *testing.T) {
	tools := Tools(ToolsOptions{
		SourceRoot:  config.OroRootDir,
		AnalyzePath: config.OroRootDir + "/src",
		Report:      ReportGitLab,
		ReportDir:   "/reports/qa",
		Baseline:    config.OroRootDir + "/" + BaselineFile,
	})

	args := strings.Join(toolByName(t, tools, "phpstan").Args, " ")
	if strings.Contains(args, "--error-format=gitlab") {
		t.Errorf("a baseline run still asks for the GitLab format: %s", args)
	}
}

func TestEnsureBaselineIncludeExtendsAnExistingIncludes(t *testing.T) {
	in := "includes:\n    - /var/www/oro/vendor-bin/qa/phpstan.neon\n\nparameters:\n    level: 6\n"

	got, changed := EnsureBaselineInclude(in)
	if !changed {
		t.Fatal("EnsureBaselineInclude reported no change")
	}
	want := "includes:\n    - " + BaselineFile + "\n    - /var/www/oro/vendor-bin/qa/phpstan.neon\n\nparameters:\n    level: 6\n"
	if got != want {
		t.Errorf("EnsureBaselineInclude =\n%q\nwant\n%q", got, want)
	}
	if strings.Count(got, "includes:") != 1 {
		t.Errorf("a second includes block was written, which would drop the first one: %q", got)
	}
}

func TestEnsureBaselineIncludeAddsABlockWhenThereIsNone(t *testing.T) {
	in := "parameters:\n    ignoreErrors: []\n"

	got, changed := EnsureBaselineInclude(in)
	if !changed {
		t.Fatal("EnsureBaselineInclude reported no change")
	}
	if !strings.HasPrefix(got, "includes:\n    - "+BaselineFile+"\n") {
		t.Errorf("the includes block is not at the top: %q", got)
	}
	if !strings.Contains(got, in) {
		t.Errorf("the existing configuration was not kept: %q", got)
	}
}

func TestEnsureBaselineIncludeIsIdempotent(t *testing.T) {
	once, _ := EnsureBaselineInclude("parameters:\n    level: 6\n")

	twice, changed := EnsureBaselineInclude(once)
	if changed {
		t.Error("EnsureBaselineInclude added the baseline twice")
	}
	if twice != once {
		t.Errorf("EnsureBaselineInclude changed an already wired config: %q", twice)
	}
}

func TestEnsureBaselineIncludeIgnoresTheFilenameInAComment(t *testing.T) {
	in := "# Run `orobox qa --phpstan --generate-baseline` to write " + BaselineFile + ".\nparameters:\n    level: 6\n"

	got, changed := EnsureBaselineInclude(in)
	if !changed {
		t.Fatal("a comment naming the baseline was taken for the include itself")
	}
	if !strings.HasPrefix(got, "includes:") {
		t.Errorf("no includes block was written: %q", got)
	}
}

func TestEnsureBaselineIncludeWritesAWholeConfigWhenThereIsNoFile(t *testing.T) {
	got, changed := EnsureBaselineInclude("")
	if !changed {
		t.Fatal("EnsureBaselineInclude reported no change for an absent config")
	}
	if got != "includes:\n    - "+BaselineFile+"\n" {
		t.Errorf("unexpected config for an absent file: %q", got)
	}
}

func TestEnsureBaselineIncludeSkipsAnIndentedIncludesKey(t *testing.T) {
	in := "parameters:\n    includes:\n        - nonsense.neon\n"

	got, _ := EnsureBaselineInclude(in)
	if !strings.HasPrefix(got, "includes:\n    - "+BaselineFile+"\n") {
		t.Errorf("the baseline was added under a nested key: %q", got)
	}
}
