package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"text/template"

	"github.com/algoritma-dev/orobox/internal/config"
	yamlv3 "gopkg.in/yaml.v3"
)

func init() {
	Templates = fstest.MapFS{
		"templates/docker/Dockerfile":               &fstest.MapFile{Data: []byte("FROM php:{{.PHPVersion}}-fpm")},
		"templates/docker/.env":                     &fstest.MapFile{Data: []byte("ORO_VERSION={{.OroVersion}}\n")},
		"templates/docker/.env.test":                &fstest.MapFile{Data: []byte("ORO_VERSION={{.OroVersion}}\n")},
		"templates/docker/nginx.conf":               &fstest.MapFile{Data: []byte("server { listen 80; }")},
		"templates/docker/init-db.sql":              &fstest.MapFile{Data: []byte("CREATE DATABASE oro;")},
		"templates/docker/docker-entrypoint.sh":     &fstest.MapFile{Data: []byte("#!/bin/bash")},
		"templates/docker/docker-compose.yml":       &fstest.MapFile{Data: []byte("version: '3'")},
		"templates/docker/docker-compose.setup.yml": &fstest.MapFile{Data: []byte("version: '3'")},
		"templates/docker/docker-compose.test.yml":  &fstest.MapFile{Data: []byte("version: '3'")},
	}
}

func TestGetComposeCommand(t *testing.T) {
	// Reset memoized result for testing
	memoizedComposeCmd = nil

	cmd := GetComposeCommand()
	if len(cmd) == 0 {
		t.Errorf("GetComposeCommand returned empty slice")
	}

	// Either ["docker", "compose"] or ["docker-compose"]
	if cmd[0] != "docker" && cmd[0] != "docker-compose" {
		t.Errorf("Unexpected first element in compose command: %s", cmd[0])
	}
}

func TestWriteFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "docker-write-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	data := struct {
		OroVersion      string
		PHPVersion      string
		NodeVersion     string
		NpmVersion      string
		BundleNamespace string
	}{
		OroVersion:      "6.1",
		PHPVersion:      "8.4",
		NodeVersion:     "22",
		NpmVersion:      "10",
		BundleNamespace: "My/Bundle",
	}

	t.Run("writeDockerfile", func(t *testing.T) {
		if !writeDockerfile(tmpDir, data) {
			t.Errorf("writeDockerfile failed")
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "Dockerfile")); os.IsNotExist(err) {
			t.Errorf("Dockerfile was not created")
		}
	})

	t.Run("writeEnvFile", func(t *testing.T) {
		if !writeEnvFile("templates/docker/.env", tmpDir, data) {
			t.Errorf("writeEnvFile .env failed")
		}
		if _, err := os.Stat(filepath.Join(tmpDir, ".env")); os.IsNotExist(err) {
			t.Errorf(".env file was not created")
		}

		if !writeEnvFile("templates/docker/.env.test", tmpDir, data) {
			t.Errorf("writeEnvFile .env.test failed")
		}
		if _, err := os.Stat(filepath.Join(tmpDir, ".env.test")); os.IsNotExist(err) {
			t.Errorf(".env.test file was not created")
		}
	})

	t.Run("writeNginxConf", func(t *testing.T) {
		// Needs more data for nginx template
		nginxData := struct {
			Domains []struct {
				Host string
				Root string
				Ssl  bool
			}
		}{
			Domains: []struct {
				Host string
				Root string
				Ssl  bool
			}{
				{Host: "localhost", Root: "public", Ssl: false},
			},
		}
		if !writeNginxConf(tmpDir, nginxData) {
			t.Errorf("writeNginxConf failed")
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "nginx.conf")); os.IsNotExist(err) {
			t.Errorf("nginx.conf was not created")
		}
	})
}

// renderRealTemplate renders a real template file from the repo against data,
// failing the test on parse/execute errors.
func renderRealTemplate(t *testing.T, relPath string, data any) string {
	t.Helper()
	content, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("read template %s: %v", relPath, err)
	}
	tmpl, err := template.New(filepath.Base(relPath)).Parse(string(content))
	if err != nil {
		t.Fatalf("parse template %s: %v", relPath, err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute template %s: %v", relPath, err)
	}
	return buf.String()
}

func assertValidYAML(t *testing.T, name, content string) {
	t.Helper()
	var out map[string]interface{}
	if err := yamlv3.Unmarshal([]byte(content), &out); err != nil {
		t.Fatalf("%s is not valid YAML: %v\n---\n%s", name, err, content)
	}
}

func bundleComposeData() map[string]any {
	return map[string]any{
		"OroVersion":              "6.1",
		"ImageSuffix":             "bundle",
		"BindWholeRepo":           false,
		"BundlePath":              "/host/repo",
		"OroRootDir":              "/var/www/oro",
		"BundleRootContainerPath": "/var/www/oro/bundles/My/Bundle",
		"BundleNamespace":         "My/Bundle",
		"BundlePackageName":       "acme/my-bundle",
		"SyncsVendorToHost":       true,
		"RunsComposerRequire":     true,
		"RunsComposerInstall":     false,
		"MountsEnvFiles":          true,
		"Type":                    "bundle",
		"UserRuntime":             "1000:1000",
		"InternalDir":             ".orobox",
		"NginxHTTPPort":           "8080",
		"NginxHTTPSPort":          "8443",
		"HasSsl":                  false,
		"Postgres":                true,
		"PostgresVersion":         "16.1-alpine",
		"Redis":                   false,
		"RedisInsight":            false,
		"Mailpit":                 false,
		"RabbitMQ":                false,
		"Elasticsearch":           false,
		"Adminer":                 false,
		"Kibana":                  false,
		"Domains":                 []config.DomainConfig{{Host: "oro.demo"}},
		"WebsocketBackendHost":    "ws",
		"WebsocketBackendPort":    "8080",
		"WebsocketFrontendPort":   "8080",
	}
}

func projectComposeData() map[string]any {
	d := bundleComposeData()
	d["ImageSuffix"] = "project"
	d["BindWholeRepo"] = true
	d["SyncsVendorToHost"] = false
	d["RunsComposerRequire"] = false
	d["RunsComposerInstall"] = true
	d["BundlePackageName"] = ""
	d["MountsEnvFiles"] = false
	d["Type"] = "project"
	return d
}

// demoComposeData is projectComposeData with the demo image tag: demo differs from project
// only in its prebuilt image and its prod runtime, not in how volumes are wired.
func demoComposeData() map[string]any {
	d := projectComposeData()
	d["ImageSuffix"] = "demo"
	d["Type"] = "demo"
	return d
}

func TestComposeSetupGolden(t *testing.T) {
	const path = "../../templates/docker/docker-compose.setup.yml"

	t.Run("bundle", func(t *testing.T) {
		out := renderRealTemplate(t, path, bundleComposeData())
		assertValidYAML(t, "setup/bundle", out)
		mustContain(t, out, "6.1-bundle-latest")
		mustContain(t, out, `"oro_app:/var/www/oro:delegated"`)
		mustContain(t, out, `"/host/repo:/var/www/oro/bundles/My/Bundle:cached"`)
		mustContain(t, out, `/host/repo/vendor-oro:/var/www/oro/vendor`)
		mustContain(t, out, "composer require")
		mustContain(t, out, "vendor-oro:/vendor-host:delegated")
		mustNotContain(t, out, "composer install --no-interaction --no-scripts")
	})

	t.Run("project", func(t *testing.T) {
		out := renderRealTemplate(t, path, projectComposeData())
		assertValidYAML(t, "setup/project", out)
		mustContain(t, out, "6.1-project-latest")
		mustContain(t, out, `"/host/repo:/var/www/oro:cached"`)
		mustContain(t, out, "composer install --no-interaction --no-scripts")
		mustNotContain(t, out, "oro_app:/var/www/oro")
		mustNotContain(t, out, "composer require")
		mustNotContain(t, out, "vendor-oro:/vendor-host")
		mustNotContain(t, out, "Populating vendor folder")
	})

	t.Run("demo", func(t *testing.T) {
		out := renderRealTemplate(t, path, demoComposeData())
		assertValidYAML(t, "setup/demo", out)
		mustContain(t, out, "6.1-demo-latest")
		mustContain(t, out, `"/host/repo:/var/www/oro:cached"`)
		mustContain(t, out, "composer install --no-interaction --no-scripts")
		mustNotContain(t, out, "oro_app:/var/www/oro")
		mustNotContain(t, out, "composer require")
	})
}

func TestComposeRuntimeGolden(t *testing.T) {
	const path = "../../templates/docker/docker-compose.yml"

	t.Run("bundle", func(t *testing.T) {
		out := renderRealTemplate(t, path, bundleComposeData())
		assertValidYAML(t, "runtime/bundle", out)
		mustContain(t, out, "6.1-bundle-latest")
		mustContain(t, out, `"oro_app:/var/www/oro:delegated"`)
		mustContain(t, out, `"/host/repo:/var/www/oro/bundles/My/Bundle:cached"`)
		mustContain(t, out, `/host/repo/vendor-oro:/var/www/oro/vendor`)
		mustContain(t, out, "vendor-oro:/vendor-host:delegated")
		mustContain(t, out, ".env-app.local")
		mustContain(t, out, ".env-app.test")
	})

	// The websocket server is a console command: without .env its ORO_ENV is empty and
	// Symfony aborts with "the environment cannot be empty" on every restart.
	t.Run("ws service gets the env file and a healthcheck", func(t *testing.T) {
		data := bundleComposeData()
		data["WebsocketFrontendPort"] = "8443"
		out := renderRealTemplate(t, path, data)

		var compose struct {
			Services map[string]struct {
				EnvFile     []string          `yaml:"env_file"`
				Environment map[string]string `yaml:"environment"`
				Healthcheck struct {
					// Compose accepts both a shell string and a command list here.
					Test any `yaml:"test"`
				} `yaml:"healthcheck"`
			} `yaml:"services"`
		}
		if err := yamlv3.Unmarshal([]byte(out), &compose); err != nil {
			t.Fatalf("runtime compose is not valid YAML: %v", err)
		}

		ws, ok := compose.Services["ws"]
		if !ok {
			t.Fatalf("ws service missing from runtime compose")
		}
		if len(ws.EnvFile) != 1 || ws.EnvFile[0] != ".env" {
			t.Errorf("ws env_file = %v, want [.env]", ws.EnvFile)
		}
		if probe := fmt.Sprint(ws.Healthcheck.Test); !strings.Contains(probe, "8080") {
			t.Errorf("ws healthcheck = %q, want a probe on port 8080", probe)
		}

		// A project checkout keeps its own .env-app.local, which points the websocket
		// somewhere else: every service talking to the server needs Orobox's addresses.
		// Not the application service: it runs the CLI and the test suite, which keep the
		// project's own (empty in test) websocket configuration.
		for _, name := range []string{"php-fpm-app", "ws", "consumer", "cron"} {
			env := compose.Services[name].Environment
			if got := env["ORO_WEBSOCKET_BACKEND_DSN"]; got != "tcp://ws:8080" {
				t.Errorf("%s ORO_WEBSOCKET_BACKEND_DSN = %q, want tcp://ws:8080", name, got)
			}
			if got := env["ORO_WEBSOCKET_FRONTEND_DSN"]; got != "//*:8443/ws" {
				t.Errorf("%s ORO_WEBSOCKET_FRONTEND_DSN = %q, want //*:8443/ws", name, got)
			}
			if got := env["ORO_WEBSOCKET_SERVER_DSN"]; got != "//0.0.0.0:8080" {
				t.Errorf("%s ORO_WEBSOCKET_SERVER_DSN = %q, want //0.0.0.0:8080", name, got)
			}
		}
	})

	t.Run("project", func(t *testing.T) {
		out := renderRealTemplate(t, path, projectComposeData())
		assertValidYAML(t, "runtime/project", out)
		mustContain(t, out, "6.1-project-latest")
		mustContain(t, out, `"/host/repo:/var/www/oro:cached"`)
		mustNotContain(t, out, ".env-app.local")
		mustNotContain(t, out, ".env-app.test")
		mustNotContain(t, out, "oro_app:/var/www/oro")
		mustNotContain(t, out, "vendor-oro:/vendor-host")
	})

	t.Run("demo", func(t *testing.T) {
		out := renderRealTemplate(t, path, demoComposeData())
		assertValidYAML(t, "runtime/demo", out)
		mustContain(t, out, "6.1-demo-latest")
		mustContain(t, out, `"/host/repo:/var/www/oro:cached"`)
		mustNotContain(t, out, ".env-app.local")
		mustNotContain(t, out, ".env-app.test")
		mustNotContain(t, out, "oro_app:/var/www/oro")
		mustNotContain(t, out, "vendor-oro:/vendor-host")
	})

	t.Run("ssh agent socket is mounted into the oro services", func(t *testing.T) {
		data := projectComposeData()
		data["SSHAgentSocket"] = "/tmp/agent.sock"
		out := renderRealTemplate(t, path, data)
		assertValidYAML(t, "runtime/ssh-agent", out)

		var compose struct {
			Services map[string]struct {
				Environment map[string]string `yaml:"environment"`
				Volumes     []string          `yaml:"volumes"`
			} `yaml:"services"`
		}
		if err := yamlv3.Unmarshal([]byte(out), &compose); err != nil {
			t.Fatalf("runtime compose is not valid YAML: %v", err)
		}

		// The mount lives in the shared volumes anchor, so every service that aliases it
		// gets the socket. Only `application` gets the environment that uses it: it is the
		// service orobox shell / run / console exec into.
		const mount = "/tmp/agent.sock:/ssh-agent"
		for _, name := range []string{"application", "php-fpm-app", "ws", "consumer", "cron"} {
			found := false
			for _, v := range compose.Services[name].Volumes {
				if v == mount {
					found = true
				}
			}
			if !found {
				t.Errorf("%s is missing the agent socket mount, volumes = %v", name, compose.Services[name].Volumes)
			}
		}

		app := compose.Services["application"].Environment
		if got := app["SSH_AUTH_SOCK"]; got != containerSSHAgentSocket {
			t.Errorf("application SSH_AUTH_SOCK = %q, want %q", got, containerSSHAgentSocket)
		}
		if got := app["GIT_SSH_COMMAND"]; !strings.Contains(got, "StrictHostKeyChecking=accept-new") {
			t.Errorf("application GIT_SSH_COMMAND = %q, want StrictHostKeyChecking=accept-new", got)
		}
	})

	t.Run("no ssh agent socket without forwarding", func(t *testing.T) {
		out := renderRealTemplate(t, path, projectComposeData())
		assertValidYAML(t, "runtime/no-ssh-agent", out)
		mustNotContain(t, out, "/ssh-agent")
		mustNotContain(t, out, "SSH_AUTH_SOCK")
	})
}

func TestEnvTemplateOroEnvPerType(t *testing.T) {
	const path = "../../templates/docker/.env"

	out := renderRealTemplate(t, path, bundleComposeData())
	mustContain(t, out, "ORO_ENV=dev")

	out = renderRealTemplate(t, path, projectComposeData())
	mustContain(t, out, "ORO_ENV=dev")

	out = renderRealTemplate(t, path, demoComposeData())
	mustContain(t, out, "ORO_ENV=prod")
}

func TestReloadWebServer(t *testing.T) {
	oldRun := RunComposeCommandSilently
	defer func() { RunComposeCommandSilently = oldRun }()

	var captured []string
	RunComposeCommandSilently = func(_ string, args ...string) error {
		captured = args
		return nil
	}

	if err := ReloadWebServer(); err != nil {
		t.Fatalf("ReloadWebServer: %v", err)
	}

	want := []string{"exec", "-T", "web", "nginx", "-s", "reload"}
	if strings.Join(captured, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", captured, want)
	}
}

func TestNginxProxiesWebsocket(t *testing.T) {
	const path = "../../templates/docker/nginx.conf"

	t.Run("configured domains", func(t *testing.T) {
		data := bundleComposeData()
		data["Domains"] = []config.DomainConfig{{Host: "oro.demo", Root: "public", Ssl: true}}
		data["HasSsl"] = true

		out := renderRealTemplate(t, path, data)

		// One /ws location per server block: plain HTTP and TLS.
		if got := strings.Count(out, "location ^~ /ws {"); got != 2 {
			t.Errorf("got %d /ws locations, want 2 (http + https)\n---\n%s", got, out)
		}
		mustContain(t, out, "proxy_pass http://$oro_ws_backend;")
		mustContain(t, out, "set $oro_ws_backend ws:8080;")
		mustContain(t, out, "proxy_set_header Upgrade $http_upgrade;")
		mustContain(t, out, `proxy_set_header Connection "Upgrade";`)
	})

	t.Run("no domains configured", func(t *testing.T) {
		data := bundleComposeData()
		data["Domains"] = []config.DomainConfig{}

		out := renderRealTemplate(t, path, data)

		mustContain(t, out, "location ^~ /ws {")
		mustContain(t, out, "set $oro_ws_backend ws:8080;")
	})
}

func TestEnvTemplateWebsocketDsns(t *testing.T) {
	const path = "../../templates/docker/.env"

	data := bundleComposeData()
	data["WebsocketFrontendPort"] = "8443"

	out := renderRealTemplate(t, path, data)

	mustContain(t, out, "ORO_WEBSOCKET_BACKEND_HOST=ws")
	mustContain(t, out, "ORO_WEBSOCKET_BACKEND_PORT=8080")
	mustContain(t, out, "ORO_WEBSOCKET_FRONTEND_PORT=8443")
	mustContain(t, out, `ORO_WEBSOCKET_FRONTEND_DSN="//*:${ORO_WEBSOCKET_FRONTEND_PORT}/${ORO_WEBSOCKET_FRONTEND_PATH}"`)
	mustContain(t, out, `ORO_WEBSOCKET_BACKEND_DSN="tcp://${ORO_WEBSOCKET_BACKEND_HOST}:${ORO_WEBSOCKET_BACKEND_PORT}"`)
}

func TestWebsocketFrontendPort(t *testing.T) {
	if got := websocketFrontendPort(false, "8080", "8443"); got != "8080" {
		t.Errorf("without ssl got %q, want 8080", got)
	}
	// The browser gets a single port, and a TLS page cannot open a plain ws:// socket.
	if got := websocketFrontendPort(true, "8080", "8443"); got != "8443" {
		t.Errorf("with ssl got %q, want 8443", got)
	}
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q\n---\n%s", needle, haystack)
	}
}

func mustNotContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected output NOT to contain %q\n---\n%s", needle, haystack)
	}
}
