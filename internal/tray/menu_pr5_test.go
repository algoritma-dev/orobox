package tray

import (
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/project"
)

func findTopLevel(t *testing.T, menu Menu, action string) MenuItem {
	t.Helper()
	for _, it := range menu.Items {
		if it.Action == action {
			return it
		}
	}
	t.Fatalf("no top-level item with action %q found in %+v", action, menu.Items)
	return MenuItem{}
}

func hasTopLevel(menu Menu, action string) bool {
	for _, it := range menu.Items {
		if it.Action == action {
			return true
		}
	}
	return false
}

func TestBuildMenuNoOtherEnvironmentsWithSingleProject(t *testing.T) {
	views := []ProjectView{upProject("mybundle", &config.OroConfig{})}
	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	if hasTopLevel(menu, "other-environments") {
		t.Error("other-environments present with a single known project, want absent")
	}
	// the active project still gets its own full header
	findHeader(t, menu, "mybundle")
}

func TestBuildMenuActiveProjectKeepsFullMenuOthersBecomeSwitchTargets(t *testing.T) {
	views := []ProjectView{
		upProject("active-one", &config.OroConfig{}),
		{Project: project.Project{Name: "idle-one", Config: &config.OroConfig{}}, State: project.StateDown},
	}
	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	// active project keeps its full header with Up/Down/Shell/etc.
	activeHeader := findHeader(t, menu, "active-one")
	findChild(t, activeHeader, "shell:active-one")

	// idle project must NOT have its own top-level header anymore
	if hasTopLevel(menu, "project:idle-one") {
		t.Error("idle-one still has its own top-level header, want it folded into Other environments")
	}

	other := findTopLevel(t, menu, "other-environments")
	sw := findChild(t, other, "switch:idle-one")
	if sw.Disabled {
		t.Errorf("switch:idle-one Disabled = true, want false when neither project is busy")
	}
}

func TestBuildMenuSwitchDisabledWhenEitherProjectBusy(t *testing.T) {
	views := []ProjectView{
		{Project: project.Project{Name: "active-one"}, State: project.StateBusy},
		{Project: project.Project{Name: "idle-one"}, State: project.StateDown},
	}
	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	other := findTopLevel(t, menu, "other-environments")
	sw := findChild(t, other, "switch:idle-one")
	if !sw.Disabled {
		t.Errorf("switch:idle-one Disabled = false, want true when the active project is busy")
	}
}

func TestBuildMenuAllDownGivesEveryProjectFullMenuNoOtherEnvironments(t *testing.T) {
	views := []ProjectView{
		{Project: project.Project{Name: "a", Config: &config.OroConfig{}}, State: project.StateDown},
		{Project: project.Project{Name: "b", Config: &config.OroConfig{}}, State: project.StateDown},
	}
	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	findHeader(t, menu, "a")
	findHeader(t, menu, "b")
	if hasTopLevel(menu, "other-environments") {
		t.Error("other-environments present when no project is active, want absent")
	}
}

func TestBuildMenuUpNotInstalledCountsAsActive(t *testing.T) {
	views := []ProjectView{
		{Project: project.Project{Name: "active-one", Config: &config.OroConfig{}}, State: project.StateUpNotInstalled},
		{Project: project.Project{Name: "idle-one", Config: &config.OroConfig{}}, State: project.StateDown},
	}
	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	findHeader(t, menu, "active-one")
	other := findTopLevel(t, menu, "other-environments")
	findChild(t, other, "switch:idle-one")
}
