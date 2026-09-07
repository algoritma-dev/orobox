package tray

import "testing"

func TestBuildMenuStartAtLoginCheckbox(t *testing.T) {
	menu := BuildMenu(nil, DockerOK, MkcertPreflight{Available: true}, true)

	item := findTopLevel(t, menu, "toggle-autostart")
	if !item.IsCheckbox {
		t.Errorf("Start at login IsCheckbox = false, want true")
	}
	if !item.Checked {
		t.Errorf("Start at login Checked = false, want true when autostartEnabled=true")
	}
}

func TestBuildMenuStartAtLoginUnchecked(t *testing.T) {
	menu := BuildMenu(nil, DockerOK, MkcertPreflight{Available: true}, false)

	item := findTopLevel(t, menu, "toggle-autostart")
	if item.Checked {
		t.Errorf("Start at login Checked = true, want false when autostartEnabled=false")
	}
}
