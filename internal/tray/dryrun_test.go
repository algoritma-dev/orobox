package tray

import (
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/project"
)

func TestRenderDryRunPrintsHeaderAndChildrenIndented(t *testing.T) {
	views := []ProjectView{{Project: project.Project{Name: "mybundle"}, State: project.StateUp}}
	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	got := RenderDryRun(menu)

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if !strings.Contains(lines[0], "mybundle (up)") {
		t.Errorf("RenderDryRun() first line = %q, want it to mention mybundle (up)", lines[0])
	}
	foundIndentedUp := false
	for _, line := range lines {
		if strings.HasPrefix(line, "  ") && strings.Contains(line, "Up") {
			foundIndentedUp = true
		}
	}
	if !foundIndentedUp {
		t.Errorf("RenderDryRun() = %q, want an indented Up child line", got)
	}
}

func TestRenderDryRunMarksDisabledItems(t *testing.T) {
	views := []ProjectView{{Project: project.Project{Name: "mybundle"}, State: project.StateUp}}
	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	got := RenderDryRun(menu)

	if !strings.Contains(got, "[disabled]") {
		t.Errorf("RenderDryRun() = %q, want a [disabled] marker on the already-up Up item", got)
	}
}
