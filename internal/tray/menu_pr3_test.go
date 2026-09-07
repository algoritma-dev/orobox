package tray

import (
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/project"
)

func upProject(name string, cfg *config.OroConfig) ProjectView {
	return ProjectView{Project: project.Project{Name: name, Config: cfg}, State: project.StateUp}
}

func TestBuildMenuSingleDomainOpenItem(t *testing.T) {
	cfg := &config.OroConfig{Domains: []config.DomainConfig{{Host: "oro.demo", Ssl: false}}}
	views := []ProjectView{upProject("mybundle", cfg)}

	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")
	open := findChild(t, header, "open-url:http://oro.demo:8080")
	if open.Disabled {
		t.Errorf("Open item Disabled = true, want false when project is up")
	}
}

func TestBuildMenuMultiDomainOpenSubmenu(t *testing.T) {
	cfg := &config.OroConfig{Domains: []config.DomainConfig{{Host: "a.demo"}, {Host: "b.demo"}}}
	views := []ProjectView{upProject("mybundle", cfg)}

	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")
	open := findChild(t, header, "open")
	if len(open.Children) != 2 {
		t.Fatalf("Open submenu children = %+v, want 2", open.Children)
	}
	if open.Children[0].Action != "open-url:http://a.demo:8080" || open.Children[1].Action != "open-url:http://b.demo:8080" {
		t.Errorf("Open submenu children = %+v", open.Children)
	}
}

func TestBuildMenuNotInstalledShowsDisabledItemInsteadOfOpen(t *testing.T) {
	cfg := &config.OroConfig{Domains: []config.DomainConfig{{Host: "oro.demo"}}}
	views := []ProjectView{{Project: project.Project{Name: "mybundle", Config: cfg}, State: project.StateUpNotInstalled}}

	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")
	notInstalled := findChild(t, header, "not-installed")
	if !notInstalled.Disabled {
		t.Errorf("Not-installed item Disabled = false, want true")
	}
	if !strings.Contains(notInstalled.Label, "orobox init") {
		t.Errorf("Not-installed item label = %q, want it to mention orobox init", notInstalled.Label)
	}
}

func TestBuildMenuOpenDisabledWhenDown(t *testing.T) {
	cfg := &config.OroConfig{Domains: []config.DomainConfig{{Host: "oro.demo"}}}
	views := []ProjectView{{Project: project.Project{Name: "mybundle", Config: cfg}, State: project.StateDown}}

	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")
	open := findChild(t, header, "open-url:http://oro.demo:8080")
	if !open.Disabled {
		t.Errorf("Open item Disabled = false, want true when project is down")
	}
}

func TestBuildMenuServicesSubmenuOnlyEnabledAndAbsentWhenNone(t *testing.T) {
	cfgWithServices := &config.OroConfig{Services: config.ServicesConfig{Mailpit: true}}
	views := []ProjectView{upProject("mybundle", cfgWithServices)}
	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")
	services := findChild(t, header, "services")
	if len(services.Children) != 1 || services.Children[0].Action != "open-url:http://localhost:8025" {
		t.Errorf("Services submenu = %+v, want just mailpit", services.Children)
	}

	viewsNone := []ProjectView{upProject("other", &config.OroConfig{})}
	headerNone := findHeader(t, BuildMenu(viewsNone, DockerOK, MkcertPreflight{Available: true}, false), "other")
	for _, c := range headerNone.Children {
		if c.Action == "services" {
			t.Errorf("Services submenu present = %+v, want absent when no service is enabled", c)
		}
	}
}

func TestBuildMenuServicesDisabledWhenProjectDown(t *testing.T) {
	cfg := &config.OroConfig{Services: config.ServicesConfig{Adminer: true}}
	views := []ProjectView{{Project: project.Project{Name: "mybundle", Config: cfg}, State: project.StateDown}}
	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")
	services := findChild(t, header, "services")
	if !services.Disabled {
		t.Errorf("Services submenu Disabled = false, want true when project is down")
	}
}

func TestBuildMenuRunSubmenuFromCommandsAbsentWhenEmpty(t *testing.T) {
	cfg := &config.OroConfig{Commands: []config.CommandConfig{{Name: "otr", Description: "Runs the test suite"}}}
	views := []ProjectView{upProject("mybundle", cfg)}
	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")
	run := findChild(t, header, "run")
	if len(run.Children) != 1 || run.Children[0].Label != "otr" || run.Children[0].Tooltip != "Runs the test suite" {
		t.Errorf("Run submenu = %+v, want a single otr item", run.Children)
	}
	if run.Children[0].Action != "run:mybundle:otr" {
		t.Errorf("Run item action = %q, want run:mybundle:otr", run.Children[0].Action)
	}

	viewsNone := []ProjectView{upProject("other", &config.OroConfig{})}
	headerNone := findHeader(t, BuildMenu(viewsNone, DockerOK, MkcertPreflight{Available: true}, false), "other")
	for _, c := range headerNone.Children {
		if c.Action == "run" {
			t.Errorf("Run submenu present = %+v, want absent with no commands configured", c)
		}
	}
}

func TestBuildMenuXdebugCheckboxReflectsStatus(t *testing.T) {
	cfg := &config.OroConfig{}
	v := upProject("mybundle", cfg)
	v.XdebugStatus = map[string]bool{"application": true}
	header := findHeader(t, BuildMenu([]ProjectView{v}, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")
	xdebug := findChild(t, header, "xdebug-toggle:mybundle")
	if !xdebug.Checked {
		t.Errorf("Xdebug item Checked = false, want true")
	}
	if xdebug.Disabled {
		t.Errorf("Xdebug item Disabled = true, want false when status is known and project is up")
	}
	if !xdebug.IsCheckbox {
		t.Errorf("Xdebug item IsCheckbox = false, want true")
	}
}

func TestBuildMenuXdebugDisabledWhenStatusUnknown(t *testing.T) {
	views := []ProjectView{upProject("mybundle", &config.OroConfig{})}
	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")
	xdebug := findChild(t, header, "xdebug-toggle:mybundle")
	if !xdebug.Disabled {
		t.Errorf("Xdebug item Disabled = false, want true when XdebugStatus is nil (unknown)")
	}
}

func TestBuildMenuUpDisabledWhenSSLNeededAndMkcertMissing(t *testing.T) {
	cfg := &config.OroConfig{Domains: []config.DomainConfig{{Host: "secure.demo", Ssl: true}}}
	views := []ProjectView{{Project: project.Project{Name: "mybundle", Config: cfg}, State: project.StateDown}}

	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: false}, false), "mybundle")
	up := findChild(t, header, "up:mybundle")
	if !up.Disabled {
		t.Errorf("Up Disabled = false, want true when SSL is needed and mkcert is unavailable")
	}
	if !strings.Contains(strings.ToLower(up.Tooltip), "mkcert") {
		t.Errorf("Up tooltip = %q, want it to mention mkcert", up.Tooltip)
	}
}

func TestBuildMenuHeaderTooltipMentionsMissingHosts(t *testing.T) {
	origCheck := checkHostInEtcHosts
	defer func() { checkHostInEtcHosts = origCheck }()
	checkHostInEtcHosts = func(host string) bool { return false }

	cfg := &config.OroConfig{Domains: []config.DomainConfig{{Host: "oro.demo"}}}
	views := []ProjectView{upProject("mybundle", cfg)}

	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: true}, false), "mybundle")
	if !strings.Contains(header.Tooltip, "oro.demo") || !strings.Contains(strings.ToLower(header.Tooltip), "hosts") {
		t.Errorf("header tooltip = %q, want it to mention the missing host oro.demo", header.Tooltip)
	}
}

func TestBuildMenuUpEnabledWhenNoSSLEvenIfMkcertMissing(t *testing.T) {
	cfg := &config.OroConfig{Domains: []config.DomainConfig{{Host: "oro.demo", Ssl: false}}}
	views := []ProjectView{{Project: project.Project{Name: "mybundle", Config: cfg}, State: project.StateDown}}

	header := findHeader(t, BuildMenu(views, DockerOK, MkcertPreflight{Available: false}, false), "mybundle")
	up := findChild(t, header, "up:mybundle")
	if up.Disabled {
		t.Errorf("Up Disabled = true, want false when no domain needs SSL, regardless of mkcert")
	}
}
