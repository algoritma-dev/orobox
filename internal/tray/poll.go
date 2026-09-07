package tray

import "github.com/algoritma-dev/orobox/internal/project"

// PollAll discovers every known environment and queries each one's current state. Every
// project is queried every call: the v1 optimization for more than a handful of known
// environments (§11 — query only the active one, assume the rest down) needs a notion of
// "the active project" the tray does not track until switching lands, so it is deferred.
func PollAll() ([]ProjectView, error) {
	projects, err := project.Discover()
	if err != nil {
		return nil, err
	}

	views := make([]ProjectView, len(projects))
	for i, p := range projects {
		views[i] = ProjectView{Project: p, State: p.Status()}
	}
	return views, nil
}
