package docker

import (
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
