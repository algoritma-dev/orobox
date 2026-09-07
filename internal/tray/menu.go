package tray

import (
	"fmt"
	"strings"

	"github.com/algoritma-dev/orobox/internal/project"
)

// MenuItem is a UI-framework-agnostic node of the tray menu tree. Action is a symbolic id
// ("up:<name>", "down:<name>", "refresh", "quit", ...) the caller dispatches on; it is a plain
// string rather than a callback so the tree stays a pure, comparable value — buildable and
// testable, or printed by --dry-run, with no systray/D-Bus dependency at all.
type MenuItem struct {
	Label      string
	Tooltip    string
	Disabled   bool
	IsCheckbox bool // renders as a checkbox item (systray.AddMenuItemCheckbox) instead of a plain one
	Checked    bool // only meaningful when IsCheckbox is true
	Children   []MenuItem
	Action     string
}

// Menu is the whole tray menu tree.
type Menu struct {
	Items []MenuItem
}

// ProjectView pairs a discovered Project with its polled State, resolved by the caller so
// BuildMenu itself performs no I/O. XdebugStatus is nil when it was not queried or the query
// failed — the Xdebug item then renders disabled rather than guessing a checkbox state.
type ProjectView struct {
	Project      project.Project
	State        project.State
	XdebugStatus map[string]bool
}

// MkcertPreflight is the one Docker-independent precondition Up needs: a project with an SSL
// domain that mkcert cannot help (binary missing) should never even attempt to start (§2.6).
// Whether the CA itself is installed does not change the menu — it changes what dispatching
// "up" does (spawn `mkcert -install` in a terminal first) — so it is not tracked here.
type MkcertPreflight struct {
	Available bool
}

// BuildMenu assembles the tray menu tree for the given projects and global Docker
// availability. Pure: same inputs, same tree, every time.
func BuildMenu(views []ProjectView, dockerState DockerState, mkcert MkcertPreflight, autostartEnabled bool) Menu {
	var items []MenuItem

	if dockerState != DockerOK {
		items = append(items, MenuItem{
			Label:    "⚠ Docker unavailable",
			Tooltip:  dockerState.Message(),
			Disabled: true,
			Action:   "docker-status",
		})
	}

	activeName, hasActive := ActiveProjectName(views)

	if !hasActive {
		// Nothing is up: every project can be started directly, none needs a switch.
		for _, v := range views {
			items = append(items, projectMenuItem(v, mkcert))
		}
	} else {
		var others []ProjectView
		activeBusy := false
		for _, v := range views {
			if v.Project.Name == activeName {
				items = append(items, projectMenuItem(v, mkcert))
				activeBusy = v.State == project.StateBusy
			} else {
				others = append(others, v)
			}
		}
		if len(others) > 0 {
			items = append(items, otherEnvironmentsItem(others, activeName, activeBusy))
		}
	}

	items = append(items,
		MenuItem{Label: "Refresh", Action: "refresh"},
		MenuItem{
			Label:      "Start at login",
			Action:     "toggle-autostart",
			IsCheckbox: true,
			Checked:    autostartEnabled,
		},
		MenuItem{Label: "Quit", Action: "quit"},
	)

	return Menu{Items: items}
}

// ActiveProjectName is the one project fixed host ports mean can really be up right now (§2.4):
// the first view whose state implies its containers exist — up, up-but-not-installed, partial,
// or an action in flight. Every other known project can then only be switched to, not started
// directly, since starting it would collide on ports with this one.
func ActiveProjectName(views []ProjectView) (name string, ok bool) {
	for _, v := range views {
		switch v.State {
		case project.StateUp, project.StateUpNotInstalled, project.StatePartial, project.StateBusy:
			return v.Project.Name, true
		}
	}
	return "", false
}

// otherEnvironmentsItem lists every non-active project as a single "switch to this" action —
// disabled while the active project (or the target itself) is busy, since a switch already in
// flight must not be interrupted by another one.
func otherEnvironmentsItem(others []ProjectView, activeName string, activeBusy bool) MenuItem {
	children := make([]MenuItem, len(others))
	for i, v := range others {
		children[i] = MenuItem{
			Label:    fmt.Sprintf("%s (%s)", v.Project.Name, stateLabel(v.State)),
			Action:   "switch:" + v.Project.Name,
			Disabled: activeBusy || v.State == project.StateBusy,
			Tooltip:  fmt.Sprintf("stop %s and start %s", activeName, v.Project.Name),
		}
	}
	return MenuItem{Label: "Other environments", Action: "other-environments", Children: children}
}

func projectMenuItem(v ProjectView, mkcert MkcertPreflight) MenuItem {
	p := v.Project
	label := fmt.Sprintf("%s (%s)", p.Name, stateLabel(v.State))
	tooltip := p.HostPath

	switch {
	case p.Conflict:
		label = fmt.Sprintf("%s (Conflict)", p.Name)
		tooltip = fmt.Sprintf("two host paths map to %q: this one (%s) and %s — resolve manually", p.Name, p.HostPath, p.ConflictHostPath)
	case p.Stale:
		tooltip = fmt.Sprintf("host path %s no longer exists on disk (stale)", p.HostPath)
	case p.ConfigError != nil:
		tooltip = fmt.Sprintf("could not read .orobox.yaml: %v", p.ConfigError)
	}

	if missing := MissingHosts(p); len(missing) > 0 {
		note := fmt.Sprintf("missing from /etc/hosts: %s", strings.Join(missing, ", "))
		if tooltip != "" {
			tooltip += " (" + note + ")"
		} else {
			tooltip = note
		}
	}

	busy := v.State == project.StateBusy
	reachable := v.State == project.StateUp || v.State == project.StateUpNotInstalled

	upDisabled := busy || v.State == project.StateUp
	upTooltip := ""
	if !upDisabled && NeedsSSL(p) && !mkcert.Available {
		upDisabled = true
		upTooltip = "mkcert not installed — install it to provision a trusted HTTPS certificate for this domain"
	}

	children := []MenuItem{
		{Label: "Up", Action: "up:" + p.Name, Disabled: upDisabled, Tooltip: upTooltip},
		{Label: "Down", Action: "down:" + p.Name, Disabled: busy || v.State == project.StateDown},
	}
	children = append(children, openItem(p, v.State))
	children = append(children, MenuItem{Label: "Shell", Action: "shell:" + p.Name, Disabled: !reachable})
	children = append(children, logsItem(p.Name, reachable))
	if services := servicesItem(p, reachable); services != nil {
		children = append(children, *services)
	}
	if run := runItem(p, reachable); run != nil {
		children = append(children, *run)
	}
	children = append(children, xdebugItem(v, reachable))
	children = append(children,
		MenuItem{Label: "Tests", Action: "test:" + p.Name, Disabled: !reachable},
		MenuItem{Label: "QA", Action: "qa:" + p.Name, Disabled: !reachable},
		MenuItem{Label: "Console…", Action: "console:" + p.Name, Disabled: !reachable},
	)

	return MenuItem{
		Label:    label,
		Tooltip:  tooltip,
		Action:   "project:" + p.Name,
		Children: children,
	}
}

// openItem is a single "Open" item for one domain, an "Open" submenu for several, or a
// disabled placeholder when the project cannot be opened right now.
func openItem(p project.Project, state project.State) MenuItem {
	if state == project.StateUpNotInstalled {
		return MenuItem{
			Label:    "Not installed — run `orobox init`",
			Action:   "not-installed",
			Disabled: true,
		}
	}

	urls := p.ApplicationURLs()
	disabled := state != project.StateUp || len(urls) == 0

	switch {
	case len(urls) == 0:
		return MenuItem{Label: "Open", Action: "open", Disabled: true, Tooltip: "no domain configured"}
	case len(urls) == 1:
		tooltip := ""
		if disabled {
			tooltip = "environment is not up"
		}
		return MenuItem{Label: "Open", Action: "open-url:" + urls[0], Disabled: disabled, Tooltip: tooltip}
	default:
		children := make([]MenuItem, len(urls))
		for i, u := range urls {
			children[i] = MenuItem{Label: u, Action: "open-url:" + u, Disabled: disabled}
		}
		return MenuItem{Label: "Open", Action: "open", Disabled: disabled, Children: children}
	}
}

// servicesItem returns nil when no optional service is enabled: the submenu itself does not
// appear, rather than showing empty (mirrored by runItem for Config.Commands).
func servicesItem(p project.Project, reachable bool) *MenuItem {
	links := ServiceLinks(p.Config)
	if len(links) == 0 {
		return nil
	}
	children := make([]MenuItem, len(links))
	for i, l := range links {
		children[i] = MenuItem{Label: l.Label, Action: "open-url:" + l.URL, Disabled: !reachable}
	}
	return &MenuItem{Label: "Services", Action: "services", Disabled: !reachable, Children: children}
}

// runItem returns nil when Config.Commands is empty.
func runItem(p project.Project, reachable bool) *MenuItem {
	if p.Config == nil || len(p.Config.Commands) == 0 {
		return nil
	}
	children := make([]MenuItem, len(p.Config.Commands))
	for i, c := range p.Config.Commands {
		children[i] = MenuItem{
			Label:    c.Name,
			Tooltip:  c.Description,
			Action:   "run:" + p.Name + ":" + c.Name,
			Disabled: !reachable,
		}
	}
	return &MenuItem{Label: "Run", Action: "run", Disabled: !reachable, Children: children}
}

// logKinds maps a Logs submenu label to the flag suffix internal/tray's dispatch turns into
// `orobox logs --<kind>` (see cmd/logs.go).
var logKinds = []struct{ label, kind string }{
	{"App", "app"},
	{"PHP-FPM", "php"},
	{"Nginx", "nginx"},
	{"Consumer", "consumer"},
	{"Cron", "cron"},
	{"WebSocket", "ws"},
}

func logsItem(name string, reachable bool) MenuItem {
	children := make([]MenuItem, len(logKinds))
	for i, k := range logKinds {
		children[i] = MenuItem{Label: k.label, Action: "logs:" + name + ":" + k.kind, Disabled: !reachable}
	}
	return MenuItem{Label: "Logs", Action: "logs", Disabled: !reachable, Children: children}
}

func xdebugItem(v ProjectView, reachable bool) MenuItem {
	if v.XdebugStatus == nil {
		return MenuItem{
			Label:      "Xdebug",
			Action:     "xdebug-toggle:" + v.Project.Name,
			IsCheckbox: true,
			Disabled:   true,
			Tooltip:    "status unknown — could not query orobox xdebug status",
		}
	}
	return MenuItem{
		Label:      "Xdebug",
		Action:     "xdebug-toggle:" + v.Project.Name,
		IsCheckbox: true,
		Disabled:   !reachable,
		Checked:    XdebugChecked(v.XdebugStatus),
	}
}

func stateLabel(s project.State) string {
	switch s {
	case project.StateUp:
		return "up"
	case project.StateUpNotInstalled:
		return "not installed"
	case project.StateDown:
		return "down"
	case project.StatePartial:
		return "partial"
	case project.StateBusy:
		return "busy"
	case project.StateError:
		return "error"
	case project.StateConflict:
		return "conflict"
	default:
		return "unknown"
	}
}
