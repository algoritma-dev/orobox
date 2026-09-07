package project

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/viper"
)

// envFile reads the generated <InternalDir>/.env, the same file orobox's own docker-compose
// stack reads its ORO_* variables from. A read failure is silent: every caller has its own
// hardcoded defaults for a project that has never been brought up (no .env yet).
func (p Project) envFile() *viper.Viper {
	v := viper.New()
	v.SetConfigFile(filepath.Join(p.InternalDir, ".env"))
	v.SetConfigType("dotenv")
	_ = v.ReadInConfig()
	return v
}

// NginxPorts returns the host ports this project's nginx publishes, read from the generated
// .env (ORO_NGINX_HTTP_PORT / ORO_NGINX_HTTPS_PORT) rather than the tray's own process
// environment: the port was fixed at generation time for this specific project.
func (p Project) NginxPorts() (httpPort, httpsPort string) {
	v := p.envFile()
	httpPort = v.GetString("ORO_NGINX_HTTP_PORT")
	if httpPort == "" {
		httpPort = "8080"
	}
	httpsPort = v.GetString("ORO_NGINX_HTTPS_PORT")
	if httpsPort == "" {
		httpsPort = "8443"
	}
	return
}

// ApplicationURLs builds one URL per configured domain, matching
// internal/docker.GetApplicationURLs: scheme from the domain's Ssl flag, port from
// NginxPorts, port suffix omitted on the scheme's default port.
func (p Project) ApplicationURLs() []string {
	if p.Config == nil {
		return nil
	}

	httpPort, httpsPort := p.NginxPorts()
	urls := make([]string, 0, len(p.Config.Domains))
	for _, d := range p.Config.Domains {
		protocol, port := "http", httpPort
		if d.Ssl {
			protocol, port = "https", httpsPort
		}
		url := fmt.Sprintf("%s://%s", protocol, d.Host)
		if (protocol == "http" && port != "80") || (protocol == "https" && port != "443") {
			url += ":" + port
		}
		urls = append(urls, url)
	}
	return urls
}

// DatabaseCredentials returns the credentials to reach this project's database, read from the
// generated .env with the same defaults internal/docker.GetDatabaseCredentialsFor(false) uses.
func (p Project) DatabaseCredentials() (user, pass, dbname, service string) {
	user, pass, dbname, service = "oro_db_user", "oro_db_pass", "oro_db", "db"

	v := p.envFile()
	if u := v.GetString("ORO_DB_USER"); u != "" {
		user = u
	}
	if pw := v.GetString("ORO_DB_PASSWORD"); pw != "" {
		pass = pw
	}
	if d := v.GetString("ORO_DB_NAME"); d != "" {
		dbname = d
	}
	return
}
