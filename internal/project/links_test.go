package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
)

func writeEnvFile(t *testing.T, internalDir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(internalDir, ".env"), []byte(content), 0644); err != nil {
		t.Fatalf("writeEnvFile: %v", err)
	}
}

func TestNginxPortsDefaultsWhenEnvMissing(t *testing.T) {
	p := Project{InternalDir: t.TempDir()}

	httpPort, httpsPort := p.NginxPorts()

	if httpPort != "8080" || httpsPort != "8443" {
		t.Errorf("NginxPorts() = (%q, %q), want (8080, 8443)", httpPort, httpsPort)
	}
}

func TestNginxPortsReadFromGeneratedEnvFile(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir, "ORO_NGINX_HTTP_PORT=9090\nORO_NGINX_HTTPS_PORT=9443\n")
	p := Project{InternalDir: dir}

	httpPort, httpsPort := p.NginxPorts()

	if httpPort != "9090" || httpsPort != "9443" {
		t.Errorf("NginxPorts() = (%q, %q), want (9090, 9443)", httpPort, httpsPort)
	}
}

func TestApplicationURLsBuildsFromDomainsAndPorts(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir, "ORO_NGINX_HTTP_PORT=80\nORO_NGINX_HTTPS_PORT=9443\n")
	p := Project{
		InternalDir: dir,
		Config: &config.OroConfig{
			Domains: []config.DomainConfig{
				{Host: "oro.demo", Ssl: false},
				{Host: "secure.demo", Ssl: true},
			},
		},
	}

	got := p.ApplicationURLs()

	want := []string{"http://oro.demo", "https://secure.demo:9443"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ApplicationURLs() = %v, want %v", got, want)
	}
}

func TestApplicationURLsNilConfigReturnsEmpty(t *testing.T) {
	p := Project{InternalDir: t.TempDir()}

	if got := p.ApplicationURLs(); len(got) != 0 {
		t.Errorf("ApplicationURLs() = %v, want empty when Config is nil", got)
	}
}

func TestDatabaseCredentialsDefaults(t *testing.T) {
	p := Project{InternalDir: t.TempDir()}

	user, pass, dbname, service := p.DatabaseCredentials()

	if user != "oro_db_user" || pass != "oro_db_pass" || dbname != "oro_db" || service != "db" {
		t.Errorf("DatabaseCredentials() = (%q, %q, %q, %q), want defaults", user, pass, dbname, service)
	}
}

func TestDatabaseCredentialsReadFromGeneratedEnvFile(t *testing.T) {
	dir := t.TempDir()
	writeEnvFile(t, dir, "ORO_DB_USER=custom_user\nORO_DB_PASSWORD=custom_pass\nORO_DB_NAME=custom_db\n")
	p := Project{InternalDir: dir}

	user, pass, dbname, service := p.DatabaseCredentials()

	if user != "custom_user" || pass != "custom_pass" || dbname != "custom_db" || service != "db" {
		t.Errorf("DatabaseCredentials() = (%q, %q, %q, %q), want the .env overrides", user, pass, dbname, service)
	}
}
