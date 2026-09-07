package tray

import "github.com/godbus/dbus/v5"

// trayBusName is the well-known session bus name orobox-tray claims to enforce a single
// running instance: two tray processes would otherwise produce two icons and duplicate actions.
const trayBusName = "it.algoritma.Orobox.Tray"

// AcquireSingleInstance claims trayBusName on the session bus with DBUS_NAME_FLAG_DO_NOT_QUEUE.
// acquired is false when another instance already holds the name — the caller should print a
// message and exit 0, not treat that as an error. release gives the name back up on shutdown.
//
// Not unit tested: it needs a real D-Bus session bus, which does not exist in a test binary.
// The rest of the tray package keeps its logic pure and testable specifically so this is the
// only boundary that can't be — matching the spec's own "no automated UI tests" carve-out.
func AcquireSingleInstance() (acquired bool, release func(), err error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false, func() {}, err
	}

	reply, err := conn.RequestName(trayBusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		_ = conn.Close()
		return false, func() {}, err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		_ = conn.Close()
		return false, func() {}, nil
	}

	return true, func() {
		_, _ = conn.ReleaseName(trayBusName)
		_ = conn.Close()
	}, nil
}
