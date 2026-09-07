package tray

import (
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/project"
)

func TestBuildMenuEmptyShowsPlaceholderAndFooter(t *testing.T) {
	menu := BuildMenu(nil, DockerOK, MkcertPreflight{Available: true}, false)

	if !containsAction(menu.Items, "refresh") || !containsAction(menu.Items, "quit") {
		t.Errorf("BuildMenu(empty) = %+v, want Refresh and Quit present", menu)
	}
	if len(projectHeaders(menu)) != 0 {
		t.Errorf("BuildMenu(empty) has project headers, want none")
	}
}

func TestBuildMenuUpProjectDisablesUpEnablesDown(t *testing.T) {
	views := []ProjectView{{Project: project.Project{Name: "mybundle"}, State: project.StateUp}}

	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	header := findHeader(t, menu, "mybundle")
	up := findChild(t, header, "up:mybundle")
	down := findChild(t, header, "down:mybundle")
	if !up.Disabled {
		t.Errorf("Up item Disabled = false, want true when project is already up")
	}
	if down.Disabled {
		t.Errorf("Down item Disabled = true, want false when project is up")
	}
}

func TestBuildMenuDownProjectEnablesUpDisablesDown(t *testing.T) {
	views := []ProjectView{{Project: project.Project{Name: "mybundle"}, State: project.StateDown}}

	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	header := findHeader(t, menu, "mybundle")
	up := findChild(t, header, "up:mybundle")
	down := findChild(t, header, "down:mybundle")
	if up.Disabled {
		t.Errorf("Up item Disabled = true, want false when project is down")
	}
	if !down.Disabled {
		t.Errorf("Down item Disabled = false, want true when project is already down")
	}
}

func TestBuildMenuBusyProjectDisablesBothActions(t *testing.T) {
	views := []ProjectView{{Project: project.Project{Name: "mybundle"}, State: project.StateBusy}}

	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	header := findHeader(t, menu, "mybundle")
	up := findChild(t, header, "up:mybundle")
	down := findChild(t, header, "down:mybundle")
	if !up.Disabled || !down.Disabled {
		t.Errorf("StateBusy project = up.Disabled=%v down.Disabled=%v, want both true", up.Disabled, down.Disabled)
	}
}

func TestBuildMenuDockerUnavailableShowsBanner(t *testing.T) {
	menu := BuildMenu(nil, DockerDaemonNotRunning, MkcertPreflight{Available: true}, false)

	if len(menu.Items) == 0 || !strings.Contains(menu.Items[0].Label, "Docker") {
		t.Errorf("BuildMenu(docker down) = %+v, want a Docker banner as the first item", menu.Items)
	}
	if !menu.Items[0].Disabled {
		t.Errorf("Docker banner item Disabled = false, want true (informational only)")
	}
}

func TestBuildMenuStaleProjectTooltipMentionsIt(t *testing.T) {
	views := []ProjectView{{Project: project.Project{Name: "ghost", Stale: true}, State: project.StateDown}}

	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	header := findHeader(t, menu, "ghost")
	if !strings.Contains(strings.ToLower(header.Tooltip), "stale") && !strings.Contains(strings.ToLower(header.Tooltip), "no longer exist") {
		t.Errorf("Stale project header tooltip = %q, want it to mention staleness", header.Tooltip)
	}
}

func TestBuildMenuConflictProjectShowsConflictState(t *testing.T) {
	views := []ProjectView{{Project: project.Project{Name: "clash", Conflict: true, ConflictHostPath: "/other/clash"}, State: project.StateDown}}

	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	header := findHeader(t, menu, "clash")
	if !strings.Contains(header.Label, "Conflict") {
		t.Errorf("Conflict project header label = %q, want it to say Conflict", header.Label)
	}
	if !strings.Contains(header.Tooltip, "/other/clash") {
		t.Errorf("Conflict project header tooltip = %q, want it to mention the other host path", header.Tooltip)
	}
}

func TestBuildMenuSortedProjectsPreserveInputOrder(t *testing.T) {
	// Discover() already sorts by name; BuildMenu must not reorder, since caller order may
	// later distinguish "active" from "other" (PR5).
	views := []ProjectView{
		{Project: project.Project{Name: "alpha"}, State: project.StateDown},
		{Project: project.Project{Name: "zeta"}, State: project.StateDown},
	}

	menu := BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false)

	headers := projectHeaders(menu)
	if len(headers) != 2 || headers[0].Label[:5] != "alpha" || !strings.HasPrefix(headers[1].Label, "zeta") {
		t.Errorf("BuildMenu() headers = %+v, want alpha then zeta in order", headers)
	}
}

func containsAction(items []MenuItem, action string) bool {
	for _, it := range items {
		if it.Action == action {
			return true
		}
		if containsAction(it.Children, action) {
			return true
		}
	}
	return false
}

func projectHeaders(menu Menu) []MenuItem {
	var headers []MenuItem
	for _, it := range menu.Items {
		if strings.HasPrefix(it.Action, "project:") {
			headers = append(headers, it)
		}
	}
	return headers
}

func findHeader(t *testing.T, menu Menu, name string) MenuItem {
	t.Helper()
	for _, it := range menu.Items {
		if it.Action == "project:"+name {
			return it
		}
	}
	t.Fatalf("no project header found for %q in %+v", name, menu.Items)
	return MenuItem{}
}

func findChild(t *testing.T, item MenuItem, action string) MenuItem {
	t.Helper()
	for _, c := range item.Children {
		if c.Action == action {
			return c
		}
	}
	t.Fatalf("no child with action %q found in %+v", action, item.Children)
	return MenuItem{}
}
