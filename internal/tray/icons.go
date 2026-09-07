package tray

import (
	trayassets "github.com/algoritma-dev/orobox/assets/tray"
	"github.com/algoritma-dev/orobox/internal/project"
)

// Re-exported so callers (main.go's systray.SetIcon calls) need only import internal/tray.
var (
	IconBase    = trayassets.IconBase
	IconUp      = trayassets.IconUp
	IconDown    = trayassets.IconDown
	IconPartial = trayassets.IconPartial
	IconError   = trayassets.IconError
)

// IconFor picks the header icon for the aggregate state across every discovered project, worst
// first: an error or conflict anywhere outranks an up project, which outranks partial, which
// outranks a stack that is simply down everywhere. Only one project can really be up at a time
// (fixed host ports), so "up" here means "the active one is up".
func IconFor(views []ProjectView) []byte {
	sawUp, sawPartial := false, false
	for _, v := range views {
		switch v.State {
		case project.StateError, project.StateConflict:
			return IconError
		case project.StateUp:
			sawUp = true
		case project.StatePartial:
			sawPartial = true
		}
	}
	switch {
	case sawUp:
		return IconUp
	case sawPartial:
		return IconPartial
	default:
		return IconDown
	}
}
