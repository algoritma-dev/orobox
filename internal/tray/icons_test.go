package tray

import (
	"testing"

	"github.com/algoritma-dev/orobox/internal/project"
)

func TestIconForNoProjectsIsDown(t *testing.T) {
	if got := IconFor(nil); &got[0] != &IconDown[0] {
		t.Errorf("IconFor(nil) did not return IconDown")
	}
}

func TestIconForAnyErrorWinsOverEverythingElse(t *testing.T) {
	views := []ProjectView{
		{Project: project.Project{Name: "a"}, State: project.StateUp},
		{Project: project.Project{Name: "b"}, State: project.StateError},
	}
	if got := IconFor(views); &got[0] != &IconError[0] {
		t.Errorf("IconFor() with an error project did not return IconError")
	}
}

func TestIconForConflictCountsAsError(t *testing.T) {
	views := []ProjectView{{Project: project.Project{Name: "a"}, State: project.StateConflict}}
	if got := IconFor(views); &got[0] != &IconError[0] {
		t.Errorf("IconFor() with a conflict project did not return IconError")
	}
}

func TestIconForUpWinsOverPartialAndDown(t *testing.T) {
	views := []ProjectView{
		{Project: project.Project{Name: "a"}, State: project.StateDown},
		{Project: project.Project{Name: "b"}, State: project.StatePartial},
		{Project: project.Project{Name: "c"}, State: project.StateUp},
	}
	if got := IconFor(views); &got[0] != &IconUp[0] {
		t.Errorf("IconFor() with an up project did not return IconUp")
	}
}

func TestIconForPartialWinsOverDown(t *testing.T) {
	views := []ProjectView{
		{Project: project.Project{Name: "a"}, State: project.StateDown},
		{Project: project.Project{Name: "b"}, State: project.StatePartial},
	}
	if got := IconFor(views); &got[0] != &IconPartial[0] {
		t.Errorf("IconFor() with a partial project did not return IconPartial")
	}
}

func TestIconForAllDownIsDown(t *testing.T) {
	views := []ProjectView{{Project: project.Project{Name: "a"}, State: project.StateDown}}
	if got := IconFor(views); &got[0] != &IconDown[0] {
		t.Errorf("IconFor() with only down projects did not return IconDown")
	}
}
