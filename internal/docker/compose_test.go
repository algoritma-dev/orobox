package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"text/template"
	"time"

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
		// From the package itself, so the assertions check the relationship between the two
		// numbers rather than a copy that could drift from what EnsureDockerCompose renders.
		"BuildMemoryLimit":      buildMemoryLimit,
		"NodeHeapMB":            nodeHeapMB,
		"WebsocketBackendHost":  "ws",
		"WebsocketBackendPort":  "8080",
		"WebsocketFrontendPort": "8080",
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

// A service_healthy dependency waits for the dependency's first *successful* probe, and Docker
// runs the first probe one interval after the container starts — start_period only decides
// whether a failure counts, not when probing begins. So a healthcheck with no explicit interval
// inherits the 30s default and taxes every dependent service ~30 seconds even when the
// dependency was ready in one: `orobox up` and `orobox init` both wait on db, and the install
// waits on gotenberg too.
//
// The failure budget is the other half. retries × interval is how long a probe may keep failing
// after start_period before the container is marked unhealthy, and shortening the interval
// without raising retries quietly shrinks that: the default 3 retries at 30s allowed 90s, and
// the same 3 retries at 2s would allow 6s — enough to fail a cold initdb on a loaded runner.
func TestHealthDependenciesProbeQuicklyWithoutLosingTheirBudget(t *testing.T) {
	type healthcheck struct {
		Test        any    `yaml:"test"`
		Interval    string `yaml:"interval"`
		Timeout     string `yaml:"timeout"`
		Retries     int    `yaml:"retries"`
		StartPeriod string `yaml:"start_period"`
	}

	// Compose consumes the three files together (GetBaseComposeArgs passes -f for each), so
	// the definitions and the dependencies are merged the same way here: db lives in the
	// runtime file but the setup file is what depends on it.
	checks := map[string]healthcheck{}
	awaited := map[string]string{}

	files := map[string]string{
		"runtime": "../../templates/docker/docker-compose.yml",
		"setup":   "../../templates/docker/docker-compose.setup.yml",
		"test":    "../../templates/docker/docker-compose.test.yml",
	}
	for label, path := range files {
		// Every optional service is enabled: they are all behind {{if}} blocks, and with them
		// off neither the service nor the depends_on entry renders, so the invariant would
		// silently skip exactly the services a user opts into.
		data := projectComposeData()
		data["UseTmpfs"] = false
		data["TmpfsSize"] = "1g"
		for _, svc := range []string{"Redis", "RedisInsight", "RabbitMQ", "Elasticsearch", "Kibana", "Mailpit", "Adminer"} {
			data[svc] = true
		}
		data["RedisVersion"] = "7.2-alpine"
		data["RabbitMQVersion"] = "3.12-management-alpine"
		data["ElasticsearchVersion"] = "8.4.1"
		out := renderRealTemplate(t, path, data)

		var compose struct {
			Services map[string]struct {
				Healthcheck healthcheck `yaml:"healthcheck"`
				// depends_on has two shapes: a bare list, which means service_started and
				// never waits for health, and a map of conditions.
				DependsOn any `yaml:"depends_on"`
			} `yaml:"services"`
		}
		if err := yamlv3.Unmarshal([]byte(out), &compose); err != nil {
			t.Fatalf("%s compose is not valid YAML: %v", label, err)
		}

		for name, svc := range compose.Services {
			if svc.Healthcheck.Test != nil {
				checks[name] = svc.Healthcheck
			}
			for _, dep := range healthDependencies(svc.DependsOn) {
				awaited[dep] = label + "/" + name
			}
		}
	}

	if len(awaited) == 0 {
		t.Fatal("no service_healthy dependencies found: the templates or this test drifted")
	}

	const (
		maxInterval  = 5 * time.Second
		minTolerance = 60 * time.Second
	)

	// EnsureServicesRunning reads Health out of `docker compose ps` for whatever it starts and
	// accepts "starting", so both databases pay the interval there too even when nothing
	// declares a service_healthy dependency on them — db-test is started on its own.
	for _, db := range []string{"db", "db-test"} {
		if _, ok := checks[db]; !ok {
			t.Errorf("%s declares no healthcheck", db)
			continue
		}
		if prev := awaited[db]; prev != "" {
			awaited[db] = prev + " and EnsureServicesRunning"
		} else {
			awaited[db] = "EnsureServicesRunning"
		}
	}

	for dep, waiter := range awaited {
		check, ok := checks[dep]
		if !ok {
			t.Errorf("%s waits for %s to be healthy, but %s declares no healthcheck", waiter, dep, dep)
			continue
		}

		if check.Interval == "" {
			t.Errorf("%s has no explicit healthcheck interval, so it inherits the 30s default and makes %s wait that long", dep, waiter)
			continue
		}
		interval, err := time.ParseDuration(check.Interval)
		if err != nil {
			t.Errorf("%s healthcheck interval %q does not parse: %v", dep, check.Interval, err)
			continue
		}
		if interval > maxInterval {
			t.Errorf("%s probes every %s, so %s waits at least that long after it is already usable; want at most %s", dep, interval, waiter, maxInterval)
		}

		if check.Retries == 0 {
			t.Errorf("%s sets an interval but no retries, so it keeps the default 3 and tolerates only %s of failure", dep, 3*interval)
			continue
		}
		if tolerance := time.Duration(check.Retries) * interval; tolerance < minTolerance {
			t.Errorf("%s tolerates only %s of failing probes after start_period (%d retries x %s); want at least %s", dep, tolerance, check.Retries, interval, minTolerance)
		}
	}
}

// Compose resolves a relative bind source against --project-directory, which
// GetBaseComposeArgs sets to the internal directory. Writing the internal directory into the
// source as well made it resolve to <internal>/<internal>/.env — a path that does not exist, so
// Docker mounted a fresh empty directory over .env-app.local. Symfony's Dotenv skips anything
// that is not a file, so the container fell back to Oro's own .env-app: ORO_ENV=prod and the
// default 127.0.0.1 database host, surfacing as "connection to server at 127.0.0.1, port 5432
// failed: Connection refused" on the first console command after `orobox up`.
//
// It only ever bit bundle installs, which are the only ones that mount these files, and only
// with a relative internal directory — under CI or OROBOX_LOCAL_CONFIG. A local absolute path
// was used verbatim and resolved fine, which is why it never showed up in development.
//
// The install step escaped it because the setup compose file gives its services
// `env_file: .env`, a bare relative name that resolves against the project directory correctly.
// That is why oro:install connected to the database and only `orobox run` failed.
func TestBundleEnvMountsResolveInsideTheProjectDirectory(t *testing.T) {
	const path = "../../templates/docker/docker-compose.yml"

	for _, internalDir := range []string{".orobox", "/home/dev/.config/orobox/proj"} {
		t.Run(internalDir, func(t *testing.T) {
			data := bundleComposeData()
			data["InternalDir"] = internalDir
			out := renderRealTemplate(t, path, data)

			var compose struct {
				Services map[string]struct {
					Volumes []string `yaml:"volumes"`
				} `yaml:"services"`
			}
			if err := yamlv3.Unmarshal([]byte(out), &compose); err != nil {
				t.Fatalf("runtime compose is not valid YAML: %v", err)
			}

			want := map[string]string{
				"/var/www/oro/.env-app.local": filepath.Join(internalDir, ".env"),
				"/var/www/oro/.env-app.test":  filepath.Join(internalDir, ".env.test"),
			}

			seen := map[string]bool{}
			for _, mount := range compose.Services["application"].Volumes {
				parts := strings.Split(mount, ":")
				if len(parts) < 2 {
					continue
				}
				source, target := parts[0], parts[1]
				expected, ok := want[target]
				if !ok {
					continue
				}
				seen[target] = true

				if got := resolveComposeBindSource(internalDir, source); got != expected {
					t.Errorf("%s is mounted from %q, which compose resolves to %q, want %q",
						target, source, got, expected)
				}
			}

			for target := range want {
				if !seen[target] {
					t.Errorf("%s is not mounted at all in a bundle install", target)
				}
			}
		})
	}
}

// healthDependencies returns the services a depends_on block waits on with
// condition: service_healthy. A bare list carries no condition and waits on nothing.
func healthDependencies(dependsOn any) []string {
	block, ok := dependsOn.(map[string]any)
	if !ok {
		return nil
	}

	var deps []string
	for dep, raw := range block {
		spec, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if spec["condition"] == "service_healthy" {
			deps = append(deps, dep)
		}
	}
	return deps
}

// resolveComposeBindSource mirrors how Compose resolves a bind source: an absolute path is used
// verbatim, a relative one is taken from --project-directory, which GetBaseComposeArgs sets to
// the internal directory.
func resolveComposeBindSource(projectDir, source string) string {
	if filepath.IsAbs(source) {
		return source
	}
	return filepath.Join(projectDir, source)
}

// Oro builds its assets with webpack, and node sizes its default heap from the host's RAM
// rather than from the container's cgroup limit. Every oro-app service shares a mem_limit, so
// on a CI runner with plenty of RAM webpack grew straight past it and the cgroup OOM killer
// took the process out — which oro:assets:build reported as
// `The process has been signaled with signal "9"`, with no "JavaScript heap out of memory"
// anywhere, because node never reached a limit of its own.
//
// The application service is where composer install, oro:install and the assets build run, so
// it is the one that needs both the headroom and a heap ceiling underneath it.
func TestApplicationBuildContainerBoundsTheNodeHeap(t *testing.T) {
	const path = "../../templates/docker/docker-compose.yml"

	parse := func(t *testing.T, out string) map[string]struct {
		MemLimit    string            `yaml:"mem_limit"`
		Environment map[string]string `yaml:"environment"`
	} {
		t.Helper()
		var compose struct {
			Services map[string]struct {
				MemLimit    string            `yaml:"mem_limit"`
				Environment map[string]string `yaml:"environment"`
			} `yaml:"services"`
		}
		if err := yamlv3.Unmarshal([]byte(out), &compose); err != nil {
			t.Fatalf("runtime compose is not valid YAML: %v", err)
		}
		return compose.Services
	}

	t.Run("node is told the container budget", func(t *testing.T) {
		services := parse(t, renderRealTemplate(t, path, projectComposeData()))

		app, ok := services["application"]
		if !ok {
			t.Fatal("application service missing from runtime compose")
		}

		opts := app.Environment["NODE_OPTIONS"]
		if !strings.Contains(opts, "--max-old-space-size=") {
			t.Fatalf("application NODE_OPTIONS = %q, want a --max-old-space-size cap", opts)
		}

		heap := heapMBFromNodeOptions(t, opts)
		limit := memLimitMB(t, app.MemLimit)
		if heap >= limit {
			t.Errorf("node heap %dMB is not below the container limit %dMB: the cgroup OOM killer would still win", heap, limit)
		}
		if limit-heap < 512 {
			t.Errorf("only %dMB left below the limit for node's non-heap memory and the PHP process waiting on it", limit-heap)
		}
	})

	// The headroom belongs to the build container alone. The long-running services never run
	// webpack, and raising their ceiling would give away the protection the limit exists for.
	t.Run("long-running services keep the smaller limit", func(t *testing.T) {
		services := parse(t, renderRealTemplate(t, path, projectComposeData()))

		app := memLimitMB(t, services["application"].MemLimit)
		for _, name := range []string{"web", "consumer", "cron", "ws", "php-fpm-app"} {
			svc, ok := services[name]
			if !ok {
				t.Fatalf("%s service missing from runtime compose", name)
			}
			if got := memLimitMB(t, svc.MemLimit); got >= app {
				t.Errorf("%s mem_limit = %dMB, want less than the build container's %dMB", name, got, app)
			}
		}
	})

	// The heap cap is unconditional, so it must not displace the SSH forwarding variables the
	// application service carries when an agent socket is mounted.
	t.Run("ssh forwarding survives", func(t *testing.T) {
		data := projectComposeData()
		data["SSHAgentSocket"] = "/tmp/agent.sock"
		services := parse(t, renderRealTemplate(t, path, data))

		env := services["application"].Environment
		if env["SSH_AUTH_SOCK"] != "/ssh-agent" {
			t.Errorf("SSH_AUTH_SOCK = %q, want /ssh-agent", env["SSH_AUTH_SOCK"])
		}
		if !strings.Contains(env["GIT_SSH_COMMAND"], "StrictHostKeyChecking=accept-new") {
			t.Errorf("GIT_SSH_COMMAND = %q, want the accept-new host key option", env["GIT_SSH_COMMAND"])
		}
		if !strings.Contains(env["NODE_OPTIONS"], "--max-old-space-size=") {
			t.Errorf("NODE_OPTIONS = %q, want the heap cap alongside the SSH variables", env["NODE_OPTIONS"])
		}
	})
}

// heapMBFromNodeOptions reads the megabyte value out of a --max-old-space-size flag, tolerating
// the ${VAR:-default} form the template writes so the value stays overridable.
func heapMBFromNodeOptions(t *testing.T, opts string) int {
	t.Helper()

	_, after, ok := strings.Cut(opts, "--max-old-space-size=")
	if !ok {
		t.Fatalf("no --max-old-space-size in %q", opts)
	}
	if _, def, isVar := strings.Cut(after, ":-"); isVar {
		after = strings.TrimSuffix(def, "}")
	}
	mb, err := strconv.Atoi(strings.TrimSpace(after))
	if err != nil {
		t.Fatalf("could not read a megabyte value out of %q: %v", opts, err)
	}
	return mb
}

// memLimitMB reads a compose mem_limit, in the ${VAR:-default} form the template writes, as
// megabytes. Only the suffixes the template actually uses are supported.
func memLimitMB(t *testing.T, limit string) int {
	t.Helper()

	if limit == "" {
		t.Fatal("mem_limit is empty: the container would be unbounded")
	}
	if _, def, isVar := strings.Cut(limit, ":-"); isVar {
		limit = strings.TrimSuffix(def, "}")
	}
	limit = strings.TrimSpace(limit)

	scale := 1
	switch {
	case strings.HasSuffix(limit, "g"):
		scale, limit = 1024, strings.TrimSuffix(limit, "g")
	case strings.HasSuffix(limit, "m"):
		scale, limit = 1, strings.TrimSuffix(limit, "m")
	default:
		t.Fatalf("mem_limit %q has no unit this test understands", limit)
	}

	n, err := strconv.Atoi(limit)
	if err != nil {
		t.Fatalf("could not read mem_limit %q: %v", limit, err)
	}
	return n * scale
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
