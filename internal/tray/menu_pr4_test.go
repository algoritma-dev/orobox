package tray

import (
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/project"
)

func TestBuildMenuShellItem(t *testing.T) {
	views := []ProjectView{upProject("mybundle", &config.OroConfig{})}
	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")

	shell := findChild(t, header, "shell:mybundle")
	if shell.Disabled {
		t.Errorf("Shell Disabled = true, want false when project is up")
	}
}

func TestBuildMenuShellDisabledWhenDown(t *testing.T) {
	views := []ProjectView{{Project: project.Project{Name: "mybundle", Config: &config.OroConfig{}}, State: project.StateDown}}
	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")

	shell := findChild(t, header, "shell:mybundle")
	if !shell.Disabled {
		t.Errorf("Shell Disabled = false, want true when project is down")
	}
}

func TestBuildMenuLogsSubmenuHasSixKinds(t *testing.T) {
	views := []ProjectView{upProject("mybundle", &config.OroConfig{})}
	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")

	logs := findChild(t, header, "logs")
	wantActions := []string{
		"logs:mybundle:app", "logs:mybundle:php", "logs:mybundle:nginx",
		"logs:mybundle:consumer", "logs:mybundle:cron", "logs:mybundle:ws",
	}
	if len(logs.Children) != len(wantActions) {
		t.Fatalf("Logs children = %+v, want %d entries", logs.Children, len(wantActions))
	}
	for i, want := range wantActions {
		if logs.Children[i].Action != want {
			t.Errorf("Logs.Children[%d].Action = %q, want %q", i, logs.Children[i].Action, want)
		}
	}
}

func TestBuildMenuLogsDisabledWhenDown(t *testing.T) {
	views := []ProjectView{{Project: project.Project{Name: "mybundle", Config: &config.OroConfig{}}, State: project.StateDown}}
	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")

	logs := findChild(t, header, "logs")
	if !logs.Disabled {
		t.Errorf("Logs Disabled = false, want true when project is down")
	}
}

func TestBuildMenuTestsQAConsoleItems(t *testing.T) {
	views := []ProjectView{upProject("mybundle", &config.OroConfig{})}
	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")

	for _, action := range []string{"test:mybundle", "qa:mybundle", "console:mybundle"} {
		item := findChild(t, header, action)
		if item.Disabled {
			t.Errorf("%s Disabled = true, want false when project is up", action)
		}
	}
}

func TestBuildMenuTestsQAConsoleDisabledWhenDown(t *testing.T) {
	views := []ProjectView{{Project: project.Project{Name: "mybundle", Config: &config.OroConfig{}}, State: project.StateDown}}
	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")

	for _, action := range []string{"test:mybundle", "qa:mybundle", "console:mybundle"} {
		item := findChild(t, header, action)
		if !item.Disabled {
			t.Errorf("%s Disabled = false, want true when project is down", action)
		}
	}
}
