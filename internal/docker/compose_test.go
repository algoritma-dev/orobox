package docker

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func init() {
	Templates = fstest.MapFS{
		"templates/docker/Dockerfile":           &fstest.MapFile{Data: []byte("FROM php:{{.PHPVersion}}-fpm")},
		"templates/docker/.env":                 &fstest.MapFile{Data: []byte("ORO_VERSION={{.OroVersion}}")},
		"templates/docker/.env.test":            &fstest.MapFile{Data: []byte("ORO_VERSION={{.OroVersion}}")},
		"templates/docker/nginx.conf":           &fstest.MapFile{Data: []byte("server { listen 80; }")},
		"templates/docker/init-db.sql":          &fstest.MapFile{Data: []byte("CREATE DATABASE oro;")},
		"templates/docker/docker-entrypoint.sh": &fstest.MapFile{Data: []byte("#!/bin/bash")},
		"templates/docker/docker-compose.yml":   &fstest.MapFile{Data: []byte("version: '3'")},
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
