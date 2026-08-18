// Package report turns the QA tools' and PHPUnit's own machine-readable output into the single
// document GitLab expects. It converts nothing: every tool already emits GitLab Code Quality
// JSON, so the work here is concatenation and path normalisation.
//
// The package deliberately knows nothing about Docker, Dagger or the filesystem layout of a
// pipeline run: it takes bytes and returns bytes, which is what makes it testable against
// fixtures captured from the real tools.
package report

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// PathPrefix rewrites the file paths a tool reports into paths relative to the repository root.
//
// GitLab resolves a finding's path against the repository root and silently drops the annotation
// when it does not match a file in the diff, so this is what decides whether the report is visible
// at all. ContainerRoot is the application root inside the container (config.OroRootDir), and
// RepoSubdir is the repository-relative directory holding the application — empty unless the
// project is a monorepo, where the repository root is one or more levels above the application.
type PathPrefix struct {
	ContainerRoot string
	RepoSubdir    string
}

// Rewrite normalises one reported path. A path that is not under ContainerRoot is returned
// unchanged: it names something outside the application — a generated file in /tmp, say — and
// inventing a repository-relative name for it would be worse than leaving it alone.
func (p PathPrefix) Rewrite(filePath string) string {
	relative := filePath

	switch {
	case p.ContainerRoot != "" && strings.HasPrefix(filePath, p.ContainerRoot+"/"):
		relative = strings.TrimPrefix(filePath, p.ContainerRoot+"/")
	case strings.HasPrefix(filePath, "/"):
		return filePath
	}

	relative = strings.TrimPrefix(relative, "./")
	if p.RepoSubdir == "" {
		return relative
	}
	return path.Join(p.RepoSubdir, relative)
}

// ToolReport is one tool's report document, named so a parse failure can say which tool produced
// it. Data may be empty: a tool that found nothing sometimes writes nothing at all.
type ToolReport struct {
	Tool string
	Data []byte
}

// CodeQualityResult is the merged document and the per-tool finding counts. The counts exist
// because the report mode redirects every tool's stdout to a file, so without them a run that
// found violations would print nothing at all.
type CodeQualityResult struct {
	Data   []byte
	Counts map[string]int
}

// MergeCodeQuality concatenates the tools' GitLab Code Quality documents into one, rewriting each
// finding's path.
//
// Issues are carried as maps rather than a typed struct on purpose: the tools emit fields Orobox
// has no opinion about — categories, severity, engine-specific extras — and a typed round-trip
// would silently drop whatever the struct does not name. Only location.path is touched.
//
// An empty or blank document counts as no findings. Anything else that fails to parse is an
// error: a tool that wrote a PHP fatal into its report file has not "found nothing", and a merged
// document silently missing a whole tool's findings is the failure this package exists to prevent.
func MergeCodeQuality(reports []ToolReport, prefix PathPrefix) (CodeQualityResult, error) {
	result := CodeQualityResult{Counts: map[string]int{}}
	// Not a nil slice: it marshals to `null`, and GitLab rejects that where it accepts `[]`.
	merged := []map[string]any{}

	for _, report := range reports {
		result.Counts[report.Tool] = 0

		if len(strings.TrimSpace(string(report.Data))) == 0 {
			continue
		}

		var issues []map[string]any
		if err := json.Unmarshal(report.Data, &issues); err != nil {
			return CodeQualityResult{}, fmt.Errorf("the %s report is not valid GitLab Code Quality JSON: %w", report.Tool, err)
		}

		for _, issue := range issues {
			rewriteIssuePath(issue, prefix)
			merged = append(merged, issue)
		}
		result.Counts[report.Tool] = len(issues)
	}

	data, err := json.Marshal(merged)
	if err != nil {
		return CodeQualityResult{}, fmt.Errorf("could not encode the merged report: %w", err)
	}
	result.Data = data
	return result, nil
}

// rewriteIssuePath normalises location.path in place, leaving an issue whose shape it does not
// recognise untouched — a report Orobox cannot interpret is still better forwarded than dropped.
func rewriteIssuePath(issue map[string]any, prefix PathPrefix) {
	location, ok := issue["location"].(map[string]any)
	if !ok {
		return
	}
	filePath, ok := location["path"].(string)
	if !ok {
		return
	}
	location["path"] = prefix.Rewrite(filePath)
}
