package tray

import (
	"fmt"
	"time"

	"github.com/esiqveland/notify"
	"github.com/godbus/dbus/v5"
)

// confirmSwitchTimeout bounds how long a switch waits for a response before treating silence as
// cancel — a confirmation nobody answers must not leave two environments stuck mid-switch.
const confirmSwitchTimeout = 60 * time.Second

// ConfirmSwitch asks, via a desktop notification carrying Switch/Cancel action buttons, whether
// to stop `from` and start `to` (§6.3: a switch must name the environment it stops, since an
// unintended `down` on work in progress is the worst thing this app can do). It blocks until the
// user picks one, dismisses the notification, or confirmSwitchTimeout elapses — the latter two
// both read as cancel.
//
// Not unit tested: it needs a real notification server on the session bus, the same D-Bus
// boundary AcquireSingleInstance has (§9) — the rest of this package stays pure specifically so
// this is one of the few places that can't be.
var ConfirmSwitch = func(from, to string) (bool, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false, err
	}

	responseCh := make(chan bool, 1)
	respond := func(ok bool) {
		select {
		case responseCh <- ok:
		default:
		}
	}

	n, err := notify.New(conn,
		notify.WithOnAction(func(sig *notify.ActionInvokedSignal) { respond(sig.ActionKey == "switch") }),
		notify.WithOnClosed(func(sig *notify.NotificationClosedSignal) { respond(false) }),
	)
	if err != nil {
		return false, err
	}
	defer n.Close()

	_, err = n.SendNotification(notify.Notification{
		AppName: "orobox",
		Summary: fmt.Sprintf("Switch to %s?", to),
		Body:    fmt.Sprintf("This stops %s and starts %s.", from, to),
		Actions: []notify.Action{
			{Key: "switch", Label: "Switch"},
			{Key: "cancel", Label: "Cancel"},
		},
		ExpireTimeout: notify.ExpireTimeoutNever,
	})
	if err != nil {
		return false, err
	}

	select {
	case ok := <-responseCh:
		return ok, nil
	case <-time.After(confirmSwitchTimeout):
		return false, nil
	}
}
