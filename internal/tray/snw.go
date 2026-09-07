package tray

import "github.com/godbus/dbus/v5"

// hasNameOwner is a variable so tests could replace the actual bus call; not exercised
// automatically today since StatusNotifierWatcherPresent needs a real session bus, the same
// boundary AcquireSingleInstance and ConfirmSwitch have (§9).
var hasNameOwner = func(conn *dbus.Conn, name string) (bool, error) {
	var owned bool
	err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&owned)
	return owned, err
}

// StatusNotifierWatcherPresent reports whether anything on the session bus currently owns
// org.kde.StatusNotifierWatcher — the object a tray host (GNOME's AppIndicator extension, KDE
// Plasma, ...) must register before any StatusNotifierItem icon, orobox-tray's included, can
// appear anywhere (§12). Its absence is the single most common reason a user reports "the tray
// doesn't start" when it in fact started and is simply invisible.
func StatusNotifierWatcherPresent() (bool, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false, err
	}
	return hasNameOwner(conn, "org.kde.StatusNotifierWatcher")
}
