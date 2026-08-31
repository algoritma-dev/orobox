package scaffold

import "github.com/algoritma-dev/orobox/internal/config"

// CIStage is one generated deploy job.
type CIStage struct {
	// Name is the stage name, used both as the job suffix and as the deploy argument.
	Name string
	// Ref is the branch the job's rule matches.
	Ref string
	// ArtifactsPath is the directory the pipeline exports the release tarballs to.
	ArtifactsPath string
}

// CIData is what the GitLab templates render against.
//
// Every path is data rather than a literal in the template, because the generated pipeline has to
// publish exactly what the commands write: a second spelling of a report path is how a pipeline
// starts declaring an artifact nothing produces.
type CIData struct {
	// OroboxVersion pins the binary the jobs download. It is passed in rather than read from cmd,
	// which this package must not import.
	OroboxVersion string
	// QAReportPath is where `orobox qa --report=gitlab` writes its Code Quality report.
	QAReportPath string
	// JUnitReportPath is where `orobox test --report=gitlab` writes its JUnit report.
	JUnitReportPath string
	// RawReportsDir holds the per-tool reports, published so a wrong merged report can be traced
	// back to the tool that produced it.
	RawReportsDir string
	// IncludePath is the pipeline file Orobox owns.
	IncludePath string
	// RootPath is the pipeline file the project owns.
	RootPath string
	// Stages is one entry per configured deploy stage, empty when the project does not deploy
	// through Orobox.
	Stages []CIStage
}

// NewCIData builds the template data for a project. A nil or unconfigured deploy block yields no
// deploy jobs, which is a supported pipeline: a project that only lints and tests in CI is exactly
// what the Dagger engine's report flags were added for.
func NewCIData(oroboxVersion string, deploy *config.DeployConfig) CIData {
	data := CIData{
		OroboxVersion:   oroboxVersion,
		QAReportPath:    config.ReportsRelDir + "/code-quality.json",
		JUnitReportPath: config.ReportsRelDir + "/junit.xml",
		RawReportsDir:   config.RawReportsRelDir,
		IncludePath:     config.CIIncludeRelPath,
		RootPath:        config.CIRootRelPath,
	}

	if !deploy.Configured() {
		return data
	}

	for _, stage := range deploy.Stages {
		data.Stages = append(data.Stages, CIStage{
			Name:          stage.Name,
			Ref:           stage.Ref,
			ArtifactsPath: config.DeployArtifactsDir + "/" + stage.Name,
		})
	}

	return data
}

// CIArtifacts returns the two GitLab pipeline files, with the same ownership split as the deploy
// pair: Orobox keeps rewriting the job definitions as the commands gain flags, and the developer's
// own stages, variables and jobs live in a file that is never rewritten.
func CIArtifacts() []Artifact {
	return []Artifact{
		{
			RelPath:      config.CIIncludeRelPath,
			TemplatePath: "templates/ci/gitlab-ci-orobox.yml.tmpl",
			Ownership:    Rewrite,
		},
		{
			RelPath:      config.CIRootRelPath,
			TemplatePath: "templates/ci/gitlab-ci.yml.tmpl",
			Ownership:    WriteOnce,
		},
	}
}
