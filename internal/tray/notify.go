package tray

import (
	"fmt"

	"github.com/esiqveland/notify"
	"github.com/godbus/dbus/v5"
)

// sendNotification delivers one desktop notification over the session bus. A variable so tests
// can drive it without a real notification server.
var sendNotification = func(summary, body string) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return err
	}
	_, err = notify.SendNotification(conn, notify.Notification{
		AppName:       "orobox",
		Summary:       summary,
		Body:          body,
		ExpireTimeout: notify.ExpireTimeoutSetByNotificationServer,
	})
	return err
}

// Notify sends a desktop notification. Best-effort: the caller logs a failure itself, a
// notification server being unavailable is never fatal to the tray.
func Notify(summary, body string) error {
	return sendNotification(summary, body)
}

// BuildActionNotification builds the summary/body for an up/down action's completion — the
// spec's "always notify" cases (§6.6), success or failure alike.
func BuildActionNotification(project, verb string, actionErr error) (summary, body string) {
	if actionErr != nil {
		return fmt.Sprintf("orobox: %s failed", project), fmt.Sprintf("%s %s: %v", verb, project, actionErr)
	}
	return fmt.Sprintf("orobox: %s", project), fmt.Sprintf("%s completed", pastTense(verb))
}

func pastTense(verb string) string {
	switch verb {
	case "up":
		return "Started"
	case "down":
		return "Stopped"
	default:
		return verb
	}
}

// BuildTransitionNotification builds the notification for an unprompted StateUp -> StateError
// transition (a container that died on its own) — the one polling-detected transition §6.6
// asks to notify; ordinary transitions stay silent so this doesn't become noise.
func BuildTransitionNotification(project string) (summary, body string) {
	return fmt.Sprintf("orobox: %s", project), "went from up to error — a container may have died"
}
