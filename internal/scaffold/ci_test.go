package scaffold

import (
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"

	yamlv3 "gopkg.in/yaml.v3"
)

func deployConfig() *config.DeployConfig {
	return &config.DeployConfig{
		Repository: "git@gitlab.com:acme/shop.git",
		Stages: []config.StageConfig{
			{Name: "staging", Ref: "develop", Host: "staging.acme.com", User: "deploy", DeployPath: "/var/www/oro"},
			{Name: "production", Ref: "main", Host: "acme.com", User: "deploy", DeployPath: "/var/www/oro"},
		},
	}
}

func TestNewCIDataUsesTheCommandsOwnPaths(t *testing.T) {
	data := NewCIData("1.2.3", deployConfig())

	if data.QAReportPath != config.ReportsRelDir+"/code-quality.json" {
		t.Errorf("QAReportPath = %q", data.QAReportPath)
	}
	if data.JUnitReportPath != config.ReportsRelDir+"/junit.xml" {
		t.Errorf("JUnitReportPath = %q", data.JUnitReportPath)
	}
	if data.RawReportsDir != config.RawReportsRelDir {
		t.Errorf("RawReportsDir = %q", data.RawReportsDir)
	}
	if len(data.Stages) != 2 {
		t.Fatalf("Stages = %+v", data.Stages)
	}
	if got, want := data.Stages[1].ArtifactsPath, config.DeployArtifactsDir+"/production"; got != want {
		t.Errorf("production artifacts path = %q, want %q", got, want)
	}
}

func TestNewCIDataWithoutDeployHasNoStages(t *testing.T) {
	if data := NewCIData("1.2.3", nil); len(data.Stages) != 0 {
		t.Errorf("Stages = %+v, want none", data.Stages)
	}
}

func TestRenderGitLabPipeline(t *testing.T) {
	useRealTemplates(t)

	out, err := Render("templates/ci/gitlab-ci-orobox.yml.tmpl", NewCIData("1.0.0-rc30", deployConfig()))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	rendered := string(out)

	if strings.Contains(rendered, leftDelim) || strings.Contains(rendered, rightDelim) {
		t.Error("the rendered pipeline still contains Go template delimiters")
	}

	for _, want := range []string{
		"orobox:lint:",
		"orobox:test:",
		"orobox:deploy:staging:",
		"orobox:deploy:production:",
		"orobox qa --report=gitlab",
		"orobox test --report=gitlab",
		"orobox deploy production --yes --skip-qa --skip-test",
		// The README snippet this replaces never installed the binary, so every job failed on its
		// first script line.
		"apk add --no-cache git",
		"releases/download/1.0.0-rc30/orobox_Linux_x86_64",
		"codequality: " + config.ReportsRelDir + "/code-quality.json",
		"junit: " + config.ReportsRelDir + "/junit.xml",
		config.DeployArtifactsDir + "/staging/",
		`if: $CI_COMMIT_BRANCH == "main"`,
		"when: manual",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the rendered pipeline is missing %q", want)
		}
	}

	// A stages: list here would fight the one in the including file.
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, "stages:") {
			t.Error("the rendered pipeline declares its own stages:")
		}
	}

	// The substring assertions above cannot catch broken indentation, and the range block over the
	// deploy stages is exactly where that would happen.
	var parsed map[string]any
	if err := yamlv3.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("the rendered pipeline is not valid YAML: %v", err)
	}
	for _, want := range []string{"orobox:lint", "orobox:test", "orobox:deploy:staging", "orobox:deploy:production"} {
		if _, ok := parsed[want]; !ok {
			t.Errorf("the parsed pipeline has no %q job", want)
		}
	}
}

func TestRenderGitLabPipelineWithoutDeployStages(t *testing.T) {
	useRealTemplates(t)

	out, err := Render("templates/ci/gitlab-ci-orobox.yml.tmpl", NewCIData("1.0.0-rc30", nil))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	rendered := string(out)

	if !strings.Contains(rendered, "orobox:lint:") {
		t.Error("the lint job is missing from a project without deploy stages")
	}
	if strings.Contains(rendered, "orobox:deploy:") {
		t.Error("a project without deploy stages got a deploy job")
	}

	var parsed map[string]any
	if err := yamlv3.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("the rendered pipeline is not valid YAML: %v", err)
	}
}

func TestRenderGitLabRootIncludesTheOroboxPipeline(t *testing.T) {
	useRealTemplates(t)

	out, err := Render("templates/ci/gitlab-ci.yml.tmpl", NewCIData("1.0.0-rc30", nil))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(string(out), "- local: "+config.CIIncludeRelPath) {
		t.Errorf("the root pipeline does not include %s:\n%s", config.CIIncludeRelPath, out)
	}
}

func TestCIArtifactsOwnership(t *testing.T) {
	artifacts := CIArtifacts()
	if len(artifacts) != 2 {
		t.Fatalf("CIArtifacts() = %d artifacts", len(artifacts))
	}

	byPath := map[string]Artifact{}
	for _, artifact := range artifacts {
		byPath[artifact.RelPath] = artifact
	}
	if got := byPath[config.CIIncludeRelPath].Ownership; got != Rewrite {
		t.Errorf("%s ownership = %v, want Rewrite", config.CIIncludeRelPath, got)
	}
	if got := byPath[config.CIRootRelPath].Ownership; got != WriteOnce {
		t.Errorf("%s ownership = %v, want WriteOnce", config.CIRootRelPath, got)
	}
}
