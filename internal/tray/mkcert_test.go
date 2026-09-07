package tray

import (
	"errors"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/project"
)

func TestNeedsSSLTrueWhenAnyDomainHasSsl(t *testing.T) {
	p := project.Project{Config: &config.OroConfig{Domains: []config.DomainConfig{
		{Host: "oro.demo", Ssl: false},
		{Host: "secure.demo", Ssl: true},
	}}}

	if !NeedsSSL(p) {
		t.Error("NeedsSSL() = false, want true when a domain has ssl: true")
	}
}

func TestNeedsSSLFalseWhenNoDomainHasSsl(t *testing.T) {
	p := project.Project{Config: &config.OroConfig{Domains: []config.DomainConfig{{Host: "oro.demo", Ssl: false}}}}

	if NeedsSSL(p) {
		t.Error("NeedsSSL() = true, want false when no domain has ssl: true")
	}
}

func TestNeedsSSLFalseWhenConfigNil(t *testing.T) {
	if NeedsSSL(project.Project{}) {
		t.Error("NeedsSSL() = true, want false when Config is nil")
	}
}

func TestMkcertAvailableReflectsLookPath(t *testing.T) {
	orig := lookPathMkcert
	defer func() { lookPathMkcert = orig }()

	lookPathMkcert = func() error { return nil }
	if !MkcertAvailable() {
		t.Error("MkcertAvailable() = false, want true when lookPathMkcert succeeds")
	}

	lookPathMkcert = func() error { return errors.New("not found") }
	if MkcertAvailable() {
		t.Error("MkcertAvailable() = true, want false when lookPathMkcert fails")
	}
}

func TestMkcertCAInstalledTrueWhenRootCAFileExists(t *testing.T) {
	origCAROOT, origStat := runMkcertCAROOT, statPath
	defer func() { runMkcertCAROOT, statPath = origCAROOT, origStat }()

	runMkcertCAROOT = func() (string, error) { return "/home/user/.local/share/mkcert", nil }
	statPath = func(path string) bool { return path == "/home/user/.local/share/mkcert/rootCA.pem" }

	if !MkcertCAInstalled() {
		t.Error("MkcertCAInstalled() = false, want true when rootCA.pem exists under CAROOT")
	}
}

func TestMkcertCAInstalledFalseWhenRootCAFileMissing(t *testing.T) {
	origCAROOT, origStat := runMkcertCAROOT, statPath
	defer func() { runMkcertCAROOT, statPath = origCAROOT, origStat }()

	runMkcertCAROOT = func() (string, error) { return "/home/user/.local/share/mkcert", nil }
	statPath = func(path string) bool { return false }

	if MkcertCAInstalled() {
		t.Error("MkcertCAInstalled() = true, want false when rootCA.pem is missing")
	}
}

func TestMkcertCAInstalledFalseWhenCARootCommandFails(t *testing.T) {
	origCAROOT := runMkcertCAROOT
	defer func() { runMkcertCAROOT = origCAROOT }()

	runMkcertCAROOT = func() (string, error) { return "", errors.New("mkcert not found") }

	if MkcertCAInstalled() {
		t.Error("MkcertCAInstalled() = true, want false when `mkcert -CAROOT` itself fails")
	}
}

func TestMissingHostsReturnsOnlyUnresolvedDomains(t *testing.T) {
	orig := checkHostInEtcHosts
	defer func() { checkHostInEtcHosts = orig }()
	checkHostInEtcHosts = func(host string) bool { return host == "known.demo" }

	p := project.Project{Config: &config.OroConfig{Domains: []config.DomainConfig{
		{Host: "known.demo"},
		{Host: "unknown.demo"},
	}}}

	got := MissingHosts(p)
	if len(got) != 1 || got[0] != "unknown.demo" {
		t.Errorf("MissingHosts() = %v, want [unknown.demo]", got)
	}
}

func TestMissingHostsNilConfigReturnsEmpty(t *testing.T) {
	if got := MissingHosts(project.Project{}); len(got) != 0 {
		t.Errorf("MissingHosts() = %v, want empty when Config is nil", got)
	}
}
