// Command orobox-tray is a Linux system tray applet for managing orobox environments without
// a terminal. See docs/agents and the design spec for the full behavior; this file is wiring
// only — menu construction, Docker/binary checks and state polling live in internal/tray.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/algoritma-dev/orobox/internal/project"
	"github.com/algoritma-dev/orobox/internal/tray"
)

// Version is overridden at build time via -ldflags "-X main.Version=...", the same way the CLI
// stamps cmd.Version. "dev" is honest for a build that did not set it.
var Version = "dev"

const pollInterval = 5 * time.Second

func main() {
	dryRun := flag.Bool("dry-run", false, "print the menu tree to stdout instead of running the tray")
	debug := flag.Bool("debug", false, "also log to stderr")
	flag.Parse()

	if *dryRun {
		runDryRun()
		return
	}

	acquired, release, err := tray.AcquireSingleInstance()
	if err != nil {
		fmt.Fprintf(os.Stderr, "orobox-tray: could not reach the session bus: %v\n", err)
		os.Exit(1)
	}
	if !acquired {
		fmt.Println("orobox-tray is already running")
		os.Exit(0)
	}
	defer release()

	closeLog := setupLogging(*debug)
	defer closeLog()

	app := newApp()
	systray.Run(app.onReady, app.onExit)
}

// runDryRun prints the menu tree that would be built right now, with no D-Bus registration and
// no display required — see internal/tray.RenderDryRun.
func runDryRun() {
	views, err := tray.PollAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "orobox-tray: discovery failed: %v\n", err)
		os.Exit(1)
	}
	dockerState := tray.CheckDocker()
	mkcert := tray.MkcertPreflight{Available: tray.MkcertAvailable()}
	fmt.Print(tray.RenderDryRun(tray.BuildMenu(views, dockerState, mkcert, tray.IsAutostartEnabled())))
}

// setupLogging writes to ~/.local/state/orobox/tray.log (the tray has no visible stdout once
// running detached from a terminal), and additionally to stderr under --debug. Falling back to
// stderr-only is not fatal: a tray that cannot log is still better than one that cannot start.
func setupLogging(debug bool) func() {
	stateDir, err := os.UserHomeDir()
	if err != nil {
		log.SetOutput(os.Stderr)
		return func() {}
	}
	logDir := filepath.Join(stateDir, ".local", "state", "orobox")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.SetOutput(os.Stderr)
		return func() {}
	}

	f, err := os.OpenFile(filepath.Join(logDir, "tray.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.SetOutput(os.Stderr)
		return func() {}
	}

	if debug {
		log.SetOutput(io.MultiWriter(f, os.Stderr))
	} else {
		log.SetOutput(f)
	}
	return func() { _ = f.Close() }
}

// app holds the tray's runtime state: which orobox binary to shell out to, which projects
// currently have an action in flight (so polling reports them Busy instead of racing the
// action's own compose calls), each project's state as of the last refresh (so an unprompted Up
// -> Error transition — a container dying on its own — can be told apart from every other
// transition, which stays silent per §6.6), and which project pollSmart last found active (§11:
// once more than a handful of environments are known, only that one gets queried on the
// automatic background refreshes; the rest are assumed Down until something forces a full look).
type app struct {
	oroboxBin string

	mu             sync.Mutex
	busy           map[string]bool
	lastState      map[string]project.State
	lastActiveName string

	// refreshCh serializes every refresh: systray.ResetMenu() plus rebuilding the tree is not
	// safe to run twice concurrently (two goroutines racing it visibly tears an open menu down
	// mid-click), so exactly one consumer goroutine drains this channel and every trigger — the
	// event stream, the fallback ticker, a dispatch handler — goes through requestRefresh
	// instead of calling refresh directly. The channel is depth-1 and non-blocking on send, so a
	// burst of triggers collapses into a single pending refresh rather than queuing one per
	// trigger. forceNext is separate from the channel so a coalesced-away request that asked for
	// a full poll is never silently downgraded to the cheap one.
	refreshCh chan struct{}
	forceMu   sync.Mutex
	forceNext bool

	// lastMenu is the last tree actually rendered — touched only inside refresh, which only
	// ever runs on runRefreshLoop's single goroutine, so it needs no lock of its own.
	lastMenu tray.Menu
}

func newApp() *app {
	oroboxBin, err := tray.ResolveOroboxBin()
	if err != nil {
		log.Printf("orobox-tray: %v", err)
	}
	return &app{
		oroboxBin: oroboxBin,
		busy:      make(map[string]bool),
		lastState: make(map[string]project.State),
		refreshCh: make(chan struct{}, 1),
	}
}

// requestRefresh queues a refresh, coalescing with any already-pending one. force=true asks for
// a full poll of every known project regardless of pollSmart's usual optimization — used for
// every user-initiated trigger (an explicit Refresh click, or right after an action the user
// just took), where correctness matters more than the query cost. force=false is for the
// automatic background triggers (docker events, the fallback ticker).
func (a *app) requestRefresh(force bool) {
	if force {
		a.forceMu.Lock()
		a.forceNext = true
		a.forceMu.Unlock()
	}
	select {
	case a.refreshCh <- struct{}{}:
	default:
	}
}

// runRefreshLoop is the single consumer of refreshCh — the only goroutine ever allowed to call
// refresh, which is what makes requestRefresh's serialization actually hold.
func (a *app) runRefreshLoop() {
	for range a.refreshCh {
		a.forceMu.Lock()
		force := a.forceNext
		a.forceNext = false
		a.forceMu.Unlock()
		a.refresh(force)
	}
}

func (a *app) onReady() {
	systray.SetIcon(tray.IconBase)
	systray.SetTitle("")
	systray.SetTooltip("orobox")

	if a.oroboxBin != "" {
		if cliVersion, err := tray.FetchCLIVersion(a.oroboxBin); err == nil {
			if warning, mismatched := tray.CheckVersionMismatch(cliVersion, Version); mismatched {
				log.Print(warning)
			}
		}
	}

	// The icon needs a StatusNotifierWatcher on the bus to appear anywhere at all (§12) — most
	// commonly missing on stock GNOME Shell, which has no tray without an extension. A silently
	// running-but-invisible process reads as "it didn't start"; name the fix instead.
	if present, err := tray.StatusNotifierWatcherPresent(); err == nil && !present {
		log.Print(`orobox-tray: no StatusNotifierWatcher found on the session bus — the icon will not appear anywhere. On GNOME Shell, install the "AppIndicator and KStatusNotifierItem Support" extension. KDE Plasma needs no extra setup.`)
	}

	go a.runRefreshLoop()
	a.requestRefresh(true)

	go a.runEventLoop()
}

func (a *app) onExit() {}

// runEventLoop drives refreshes from `docker events` (§6.4 v2): near-zero-cost push updates
// instead of a `docker compose ps` per known environment every pollInterval. Events arrive in
// bursts (container created, started, health_status, ...) for one action, so they are debounced
// down to a single refresh. The stream ending — daemon restart, docker itself going away — falls
// back to the plain ticker for the rest of the process's life, per spec: polling is the
// fallback, not a peer.
func (a *app) runEventLoop() {
	debouncedRefresh := debounce(250*time.Millisecond, func() { a.requestRefresh(false) })

	err := tray.StreamDockerEvents(context.Background(), func(tray.DockerEvent) { debouncedRefresh() })
	if err != nil {
		log.Printf("orobox-tray: docker events stream ended (%v), falling back to polling", err)
	} else {
		log.Print("orobox-tray: docker events stream ended, falling back to polling")
	}
	a.pollLoop()
}

// pollLoop refreshes every pollInterval.
func (a *app) pollLoop() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for range ticker.C {
		a.requestRefresh(false)
	}
}

// debounce returns a function that calls f after d has passed with no further calls — the
// standard trailing-edge debounce, used to collapse a burst of docker events into one refresh.
func debounce(d time.Duration, f func()) func() {
	var mu sync.Mutex
	var timer *time.Timer
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(d, f)
	}
}

// refresh re-discovers and re-renders the whole menu. Projects with an action in flight are
// reported Busy rather than queried, so a `docker compose up` in progress does not get
// overwritten by a transient status read racing it. A reachable project (up, or up but not
// installed) additionally gets its Xdebug status queried, since that is the only state the
// checkbox can render truthfully. Always call this through requestRefresh, never directly — it
// is not safe to run concurrently with itself (see the app struct's refreshCh doc).
func (a *app) refresh(force bool) {
	views, err := a.pollSmart(force)
	if err != nil {
		log.Printf("orobox-tray: discovery failed: %v", err)
		views = nil
	}

	a.mu.Lock()
	for i, v := range views {
		if a.busy[v.Project.Name] {
			views[i].State = project.StateBusy
		}
	}
	a.mu.Unlock()

	a.notifyUnpromptedFailures(views)

	if a.oroboxBin != "" {
		for i, v := range views {
			reachable := v.State == project.StateUp || v.State == project.StateUpNotInstalled
			if !reachable {
				continue
			}
			if status, err := tray.XdebugStatus(a.oroboxBin, v.Project.HostPath); err == nil {
				views[i].XdebugStatus = status
			}
		}
	}

	dockerState := tray.CheckDocker()
	mkcert := tray.MkcertPreflight{Available: tray.MkcertAvailable()}
	menu := tray.BuildMenu(views, dockerState, mkcert, tray.IsAutostartEnabled())

	// systray has no "menu is currently open" hook to pause around (verified against the
	// library's own dbusmenu implementation — AboutToShow is a stub), so ResetMenu is the only
	// tool available, and it always closes whatever is open when it runs, mid-click or not. The
	// next best thing: never call it when nothing actually changed. Requests fire every few
	// seconds from docker events even at rest (health checks alone), and a refresh landing while
	// the user has a submenu open otherwise slams it shut on every single one, not just the rare
	// unlucky one. refresh only ever runs from the single runRefreshLoop goroutine, so lastMenu
	// needs no lock of its own.
	if reflect.DeepEqual(menu, a.lastMenu) {
		return
	}
	a.lastMenu = menu

	systray.SetIcon(tray.IconFor(views))
	systray.ResetMenu()
	renderMenu(menu, a.dispatch)
}

// pollSmart discovers every known project but, per §11, only queries Status() for all of them
// when there are few enough (<=3) that the cost is trivial, or when force is set (an explicit
// Refresh click, or right after an action so its own result is accurate). Otherwise only the
// project pollSmart last found active is queried; every other one is reported Down without a
// docker call. This is what makes the background refreshes (docker events, the fallback ticker)
// cheap on a machine with many known environments instead of shelling out to `docker compose ps`
// (and, for the active one, an extra install check) for every single one on every tick.
func (a *app) pollSmart(force bool) ([]tray.ProjectView, error) {
	projects, err := project.Discover()
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	activeHint := a.lastActiveName
	a.mu.Unlock()

	full := force || len(projects) <= 3 || activeHint == ""
	views := make([]tray.ProjectView, len(projects))
	for i, p := range projects {
		if full || p.Name == activeHint {
			views[i] = tray.ProjectView{Project: p, State: p.Status()}
		} else {
			views[i] = tray.ProjectView{Project: p, State: project.StateDown}
		}
	}

	if name, ok := tray.ActiveProjectName(views); ok {
		a.mu.Lock()
		a.lastActiveName = name
		a.mu.Unlock()
		return views, nil
	}
	if full {
		a.mu.Lock()
		a.lastActiveName = ""
		a.mu.Unlock()
		return views, nil
	}

	// The assumed-active project turned out not to be: sweep the rest once to find whichever
	// one really is active (or confirm none is), rather than getting stuck on a stale name.
	for i, p := range projects {
		if p.Name == activeHint {
			continue
		}
		views[i] = tray.ProjectView{Project: p, State: p.Status()}
	}
	name, ok := tray.ActiveProjectName(views)
	a.mu.Lock()
	if ok {
		a.lastActiveName = name
	} else {
		a.lastActiveName = ""
	}
	a.mu.Unlock()
	return views, nil
}

// notifyUnpromptedFailures compares each project's state to what the previous refresh saw, and
// notifies the one transition §6.6 calls out: a project that was Up and, with no action of the
// tray's own in flight, is now Error — a container that died on its own. Every other transition
// (including into Busy, which happens on every action) stays silent, or every ordinary
// transition would become noise the user disables the whole notifier over.
func (a *app) notifyUnpromptedFailures(views []tray.ProjectView) {
	a.mu.Lock()
	defer a.mu.Unlock()

	seen := make(map[string]bool, len(views))
	for _, v := range views {
		seen[v.Project.Name] = true
		prev, known := a.lastState[v.Project.Name]
		if known && prev == project.StateUp && v.State == project.StateError {
			summary, body := tray.BuildTransitionNotification(v.Project.Name)
			if err := tray.Notify(summary, body); err != nil {
				log.Printf("orobox-tray: notify failed: %v", err)
			}
		}
		a.lastState[v.Project.Name] = v.State
	}
	for name := range a.lastState {
		if !seen[name] {
			delete(a.lastState, name)
		}
	}
}

// findHostPath re-discovers and looks up a single project's host path by name. Actions need
// it fresh rather than cached from the last render, since a project can be added or go stale
// between renders.
func (a *app) findHostPath(name string) (string, bool) {
	p, ok := a.findProject(name)
	return p.HostPath, ok
}

// dispatch handles a menu click by its symbolic action id (see internal/tray.MenuItem.Action).
// Parent items that only group children ("open", "services", "run", "docker-status",
// "not-installed") carry no behavior of their own.
func (a *app) dispatch(action string) {
	switch {
	case action == "quit":
		systray.Quit()
	case action == "refresh":
		a.requestRefresh(true)
	case strings.HasPrefix(action, "up:"):
		a.dispatchUp(strings.TrimPrefix(action, "up:"))
	case strings.HasPrefix(action, "down:"):
		a.runAction(strings.TrimPrefix(action, "down:"), "down")
	case strings.HasPrefix(action, "open-url:"):
		openURL(strings.TrimPrefix(action, "open-url:"))
	case strings.HasPrefix(action, "run:"):
		a.dispatchRun(strings.TrimPrefix(action, "run:"))
	case strings.HasPrefix(action, "xdebug-toggle:"):
		a.dispatchXdebugToggle(strings.TrimPrefix(action, "xdebug-toggle:"))
	case strings.HasPrefix(action, "shell:"):
		a.dispatchShell(strings.TrimPrefix(action, "shell:"))
	case strings.HasPrefix(action, "logs:"):
		a.dispatchLogs(strings.TrimPrefix(action, "logs:"))
	case strings.HasPrefix(action, "test:"):
		a.dispatchTest(strings.TrimPrefix(action, "test:"))
	case strings.HasPrefix(action, "qa:"):
		a.dispatchQA(strings.TrimPrefix(action, "qa:"))
	case strings.HasPrefix(action, "console:"):
		a.dispatchConsole(strings.TrimPrefix(action, "console:"))
	case strings.HasPrefix(action, "switch:"):
		a.dispatchSwitch(strings.TrimPrefix(action, "switch:"))
	case action == "toggle-autostart":
		a.dispatchToggleAutostart()
	}
}

// dispatchSwitch is the tray's central operation (§6.3): stop the active project, start
// target. Confirmed first, since an unwanted `down` on work in progress is the worst thing this
// app can do; both projects sit in StateBusy for the whole operation so neither can be touched
// again mid-switch; a port conflict on the way up gets a message that names the real cause
// instead of Docker's raw wording.
func (a *app) dispatchSwitch(targetName string) {
	if a.oroboxBin == "" {
		log.Print("orobox-tray: cannot switch, orobox was not found in PATH")
		return
	}

	views, err := a.pollSmart(true)
	if err != nil {
		log.Printf("orobox-tray: discovery failed: %v", err)
		return
	}

	activeName, hasActive := tray.ActiveProjectName(views)
	if !hasActive {
		// Nothing to stop: behaves like a plain Up on the target.
		a.dispatchUp(targetName)
		return
	}
	if activeName == targetName {
		return
	}

	var activeHostPath, targetHostPath string
	var targetProject project.Project
	for _, v := range views {
		switch v.Project.Name {
		case activeName:
			activeHostPath = v.Project.HostPath
		case targetName:
			targetHostPath, targetProject = v.Project.HostPath, v.Project
		}
	}
	if activeHostPath == "" || targetHostPath == "" {
		log.Printf("orobox-tray: switch: could not resolve host paths for %s/%s", activeName, targetName)
		return
	}

	confirmed, err := tray.ConfirmSwitch(activeName, targetName)
	if err != nil {
		log.Printf("orobox-tray: confirm switch failed: %v", err)
		return
	}
	if !confirmed {
		return
	}

	if !a.mkcertPreflight(targetProject) {
		return
	}

	a.mu.Lock()
	a.busy[activeName] = true
	a.busy[targetName] = true
	a.mu.Unlock()
	a.requestRefresh(true)

	go func() {
		if output, err := tray.RunAction(a.oroboxBin, activeHostPath, "down"); err != nil {
			log.Printf("orobox-tray: down %s (switch) failed: %v\n%s", activeName, err, output)
		}

		output, upErr := tray.RunAction(a.oroboxBin, targetHostPath, "up")
		var summary, body string
		if upErr != nil {
			log.Printf("orobox-tray: up %s (switch) failed: %v\n%s", targetName, upErr, output)
			summary, body = tray.BuildSwitchFailureNotification(targetName, upErr, string(output))
		} else {
			summary = fmt.Sprintf("orobox: switched to %s", targetName)
			body = fmt.Sprintf("stopped %s, started %s", activeName, targetName)
		}
		if err := tray.Notify(summary, body); err != nil {
			log.Printf("orobox-tray: notify failed: %v", err)
		}

		a.mu.Lock()
		delete(a.busy, activeName)
		delete(a.busy, targetName)
		a.mu.Unlock()
		a.requestRefresh(true)
	}()
}

// dispatchToggleAutostart flips "Start at login" and re-renders so the checkbox reflects the
// new state immediately.
func (a *app) dispatchToggleAutostart() {
	var err error
	if tray.IsAutostartEnabled() {
		err = tray.DisableAutostart()
	} else {
		err = tray.EnableAutostart()
	}
	if err != nil {
		log.Printf("orobox-tray: could not change autostart: %v", err)
	}
	a.requestRefresh(true)
}

// dispatchUp runs the mkcert preflight (§2.6) before starting a project with at least one SSL
// domain: mkcert missing is already reflected as a disabled Up in the menu, so reaching this
// function that way would mean a stale render — checked again here regardless.
func (a *app) dispatchUp(name string) {
	target, ok := a.findProject(name)
	if !ok {
		log.Printf("orobox-tray: unknown project %q", name)
		return
	}
	if !a.mkcertPreflight(target) {
		return
	}
	a.runAction(name, "up")
}

// findProject re-discovers and looks up a single project by name. Uses the cheap Discover()
// directly rather than a Status-querying poll: every caller only wants HostPath/Config here.
func (a *app) findProject(name string) (project.Project, bool) {
	projects, err := project.Discover()
	if err != nil {
		log.Printf("orobox-tray: discovery failed: %v", err)
		return project.Project{}, false
	}
	for _, p := range projects {
		if p.Name == name {
			return p, true
		}
	}
	return project.Project{}, false
}

// mkcertPreflight is the SSL precondition §2.6 asks for before any `up`, plain or as part of a
// switch: a project needing SSL with mkcert altogether missing must not start (already reflected
// as a disabled Up in the menu, checked again here regardless of how dispatch was reached). A CA
// that has never been installed blocks nothing — instead a terminal running `mkcert -install`
// opens so the user can enter their sudo password, and only once that closes does this return.
func (a *app) mkcertPreflight(p project.Project) bool {
	if !tray.NeedsSSL(p) {
		return true
	}
	if !tray.MkcertAvailable() {
		log.Printf("orobox-tray: cannot start %s: mkcert is not installed", p.Name)
		return false
	}
	if !tray.MkcertCAInstalled() {
		a.runMkcertInstall()
	}
	return true
}

// runMkcertInstall opens a terminal running `mkcert -install` and blocks until the user closes
// it, so `up` only proceeds after the CA has had a chance to be installed.
func (a *app) runMkcertInstall() {
	bin, argPrefix, err := tray.DetectTerminal("")
	if err != nil {
		log.Printf("orobox-tray: %v", err)
		return
	}
	if err := tray.BuildMkcertInstallCmd(bin, argPrefix).Run(); err != nil {
		log.Printf("orobox-tray: mkcert -install terminal exited with an error: %v", err)
	}
}

// dispatchRun opens a terminal running one of the project's custom commands (Config.Commands),
// per §6.5: interactive/long commands never run captured inside the tray.
func (a *app) dispatchRun(rest string) {
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		log.Printf("orobox-tray: malformed run action %q", rest)
		return
	}
	name, command := parts[0], parts[1]
	a.openTerminal(name, "run", command)
}

// dispatchShell opens a terminal running `orobox shell` (the application container).
func (a *app) dispatchShell(name string) {
	a.openTerminal(name, "shell")
}

// logFlags maps a Logs submenu kind to cmd/logs.go's matching flag.
var logFlags = map[string]string{
	"app": "--app", "php": "--php", "nginx": "--nginx",
	"consumer": "--consumer", "cron": "--cron", "ws": "--ws",
}

// dispatchLogs opens a terminal tailing one service's logs (`orobox logs --<kind>`).
func (a *app) dispatchLogs(rest string) {
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		log.Printf("orobox-tray: malformed logs action %q", rest)
		return
	}
	name, kind := parts[0], parts[1]
	flag, ok := logFlags[kind]
	if !ok {
		log.Printf("orobox-tray: unknown log kind %q", kind)
		return
	}
	a.openTerminal(name, "logs", flag)
}

// dispatchTest opens a terminal running `orobox test`.
func (a *app) dispatchTest(name string) {
	a.openTerminal(name, "test")
}

// dispatchQA opens a terminal running `orobox qa`.
func (a *app) dispatchQA(name string) {
	a.openTerminal(name, "qa")
}

// dispatchConsole opens a terminal running `orobox console` with no arguments: bin/console's
// own interactive command list stands in for a native input prompt, which systray has no
// dialog widget to provide.
func (a *app) dispatchConsole(name string) {
	a.openTerminal(name, "console")
}

// openTerminal spawns a detected terminal emulator running `orobox <oroboxArgs...>` in name's
// host directory. Shared by every interactive/long command (§6.5): shell, logs, run, test, qa,
// console all just differ in which orobox subcommand they hand to the same terminal.
func (a *app) openTerminal(name string, oroboxArgs ...string) {
	hostPath, ok := a.findHostPath(name)
	if !ok {
		log.Printf("orobox-tray: unknown project %q", name)
		return
	}

	bin, argPrefix, err := tray.DetectTerminal("")
	if err != nil {
		log.Printf("orobox-tray: %v", err)
		return
	}
	if err := tray.BuildTerminalCmd(bin, argPrefix, hostPath, oroboxArgs...).Start(); err != nil {
		log.Printf("orobox-tray: could not open a terminal for %q: %v", strings.Join(oroboxArgs, " "), err)
	}
}

// dispatchXdebugToggle re-queries the current state (never trusting the menu's last render) and
// flips it, since accidentally repeating "on" or "off" is harmless but toggling the wrong way
// from stale data is not.
func (a *app) dispatchXdebugToggle(name string) {
	if a.oroboxBin == "" {
		log.Print("orobox-tray: cannot toggle Xdebug, orobox was not found in PATH")
		return
	}
	hostPath, ok := a.findHostPath(name)
	if !ok {
		log.Printf("orobox-tray: unknown project %q", name)
		return
	}

	status, err := tray.XdebugStatus(a.oroboxBin, hostPath)
	if err != nil {
		log.Printf("orobox-tray: could not read Xdebug status for %s: %v", name, err)
		return
	}

	verb := "on"
	if tray.XdebugChecked(status) {
		verb = "off"
	}

	if output, err := tray.RunAction(a.oroboxBin, hostPath, "xdebug", verb); err != nil {
		log.Printf("orobox-tray: xdebug %s failed for %s: %v\n%s", verb, name, err, output)
	}
	a.requestRefresh(true)
}

// openURL opens url with xdg-open, never a hardcoded browser (§6.2).
func openURL(url string) {
	if err := exec.Command("xdg-open", url).Start(); err != nil {
		log.Printf("orobox-tray: could not open %s: %v", url, err)
	}
}

// runAction shells out to `orobox <verb>` in the project's host directory. It never blocks the
// tray's event loop: `orobox up` can run for minutes, so this runs in its own goroutine and
// marks the project Busy for the duration.
func (a *app) runAction(name, verb string) {
	if a.oroboxBin == "" {
		log.Print("orobox-tray: cannot run action, orobox was not found in PATH")
		return
	}

	hostPath, ok := a.findHostPath(name)
	if !ok {
		log.Printf("orobox-tray: unknown project %q", name)
		return
	}

	a.mu.Lock()
	a.busy[name] = true
	a.mu.Unlock()
	a.requestRefresh(true)

	go func() {
		output, actionErr := tray.RunAction(a.oroboxBin, hostPath, verb)
		if actionErr != nil {
			log.Printf("orobox-tray: %s %s failed: %v\n%s", verb, name, actionErr, output)
		}

		summary, body := tray.BuildActionNotification(name, verb, actionErr)
		if err := tray.Notify(summary, body); err != nil {
			log.Printf("orobox-tray: notify failed: %v", err)
		}

		a.mu.Lock()
		delete(a.busy, name)
		a.mu.Unlock()
		a.requestRefresh(true)
	}()
}

// renderMenu builds the real systray menu tree from an already-computed tray.Menu, wiring each
// item's ClickedCh to dispatch. systray has no generic "render a tree" API: top-level items,
// submenu items and checkboxes are each added through a different method, so this walks the
// tree once per shape.
func renderMenu(menu tray.Menu, dispatch func(string)) {
	for _, item := range menu.Items {
		si := addTopLevel(item)
		wireClick(si, item.Action, dispatch)
		addChildren(si, item.Children, dispatch)
	}
}

func addTopLevel(item tray.MenuItem) *systray.MenuItem {
	var si *systray.MenuItem
	if item.IsCheckbox {
		si = systray.AddMenuItemCheckbox(item.Label, item.Tooltip, item.Checked)
	} else {
		si = systray.AddMenuItem(item.Label, item.Tooltip)
	}
	if item.Disabled {
		si.Disable()
	}
	return si
}

func addChildren(parent *systray.MenuItem, items []tray.MenuItem, dispatch func(string)) {
	for _, item := range items {
		var si *systray.MenuItem
		if item.IsCheckbox {
			si = parent.AddSubMenuItemCheckbox(item.Label, item.Tooltip, item.Checked)
		} else {
			si = parent.AddSubMenuItem(item.Label, item.Tooltip)
		}
		if item.Disabled {
			si.Disable()
		}
		wireClick(si, item.Action, dispatch)
		addChildren(si, item.Children, dispatch)
	}
}

func wireClick(item *systray.MenuItem, action string, dispatch func(string)) {
	go func() {
		for range item.ClickedCh {
			dispatch(action)
		}
	}()
}
