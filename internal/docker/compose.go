// Package docker provides helpers to generate and run Docker Compose for Orobox.
package docker

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"text/template"

	"github.com/algoritma-dev/orobox/internal/config"

	"github.com/spf13/viper"
)

var memoizedComposeCmd []string

// GetComposeCommand returns the docker compose command to use, preferring
// the integrated 'docker compose' when available and falling back to
// the legacy 'docker-compose'.
func GetComposeCommand() []string {
	if memoizedComposeCmd != nil {
		return memoizedComposeCmd
	}
	// Check if "docker compose" is available
	cmd := exec.Command("docker", "compose", "version")
	if err := cmd.Run(); err == nil {
		memoizedComposeCmd = []string{"docker", "compose"}
	} else {
		// Fallback to "docker-compose"
		memoizedComposeCmd = []string{"docker-compose"}
	}
	return memoizedComposeCmd
}

// GetNginxPorts returns the configured HTTP and HTTPS ports for Nginx.
func GetNginxPorts() (httpPort string, httpsPort string) {
	httpPort = viper.GetString("nginx_http_port")
	if httpPort == "" {
		httpPort = os.Getenv("ORO_NGINX_HTTP_PORT")
	}
	if httpPort == "" {
		httpPort = "8080"
	}

	httpsPort = viper.GetString("nginx_https_port")
	if httpsPort == "" {
		httpsPort = os.Getenv("ORO_NGINX_HTTPS_PORT")
	}
	if httpsPort == "" {
		httpsPort = "8443"
	}
	return
}

// GetApplicationURLs returns the list of URLs where the application is reachable.
func GetApplicationURLs() []string {
	var domains []config.DomainConfig
	_ = viper.UnmarshalKey("domains", &domains)

	httpPort, httpsPort := GetNginxPorts()

	urls := make([]string, 0, len(domains))
	for _, d := range domains {
		protocol := "http"
		port := httpPort
		if d.Ssl {
			protocol = "https"
			port = httpsPort
		}

		url := fmt.Sprintf("%s://%s", protocol, d.Host)
		if (protocol == "http" && port != "80") || (protocol == "https" && port != "443") {
			url += ":" + port
		}
		urls = append(urls, url)
	}
	return urls
}

// EnsureDockerCompose renders and writes all docker-related files under the
// internal directory. It returns true if any file content changed.
func EnsureDockerCompose() bool {
	internalDir := config.GetInternalDir()
	err := os.MkdirAll(internalDir, 0755)
	if err != nil {
		panic(err)
	}

	data := struct {
		Type                 string
		OroVersion           string
		PHPVersion           string
		NodeVersion          string
		NpmVersion           string
		BundlePath           string
		Postgres             bool
		PostgresVersion      string
		Redis                bool
		RedisVersion         string
		Mailpit              bool
		RabbitMQ             bool
		RabbitMQVersion      string
		Elasticsearch        bool
		ElasticsearchVersion string
		InternalDir          string
		OroRootDir           string
		CustomBundle         string
		BundleNamespace      string
		Domains              []config.DomainConfig
		MemoryLimit          string
		NginxHTTPPort        string
		NginxHTTPSPort       string
		PhpFpmPort           string
		HasSsl               bool
		CertsPath            string
		Xdebug               bool
		UserRuntime          string
	}{
		Type:            viper.GetString("type"),
		InternalDir:     internalDir,
		OroRootDir:      config.OroRootDir,
		CustomBundle:    config.CustomBundlePath,
		BundleNamespace: config.GetBundlePath(),
		MemoryLimit:     "2048M", // Default
		PhpFpmPort:      "9000",
		UserRuntime:     "www-data",
	}

	if runtime.GOOS == "linux" {
		if currentUser, err := user.Current(); err == nil {
			data.UserRuntime = currentUser.Uid + ":" + currentUser.Gid
		}
	}

	data.NginxHTTPPort, data.NginxHTTPSPort = GetNginxPorts()

	_ = viper.UnmarshalKey("domains", &data.Domains)
	for _, domain := range data.Domains {
		if domain.Ssl {
			data.HasSsl = true
			break
		}
	}

	if data.HasSsl {
		absCertsPath, err := filepath.Abs(filepath.Join(config.GetInternalDir(), "certs"))
		if err == nil {
			data.CertsPath = absCertsPath
		} else {
			data.CertsPath = "certs"
		}
	}

	oroVersion := viper.GetString("oro_version")
	data.OroVersion = oroVersion

	versions := config.GetVersionsForOro(oroVersion)

	data.PHPVersion = versions.PHP
	data.NodeVersion = versions.Node
	data.NpmVersion = versions.NPM
	data.PostgresVersion = versions.Postgres
	data.Postgres = true

	data.RabbitMQ = viper.GetBool("services.rabbitmq")
	if data.RabbitMQ {
		data.RabbitMQVersion = versions.RabbitMQ
	}

	data.Elasticsearch = viper.GetBool("services.elasticsearch")
	if data.Elasticsearch {
		data.ElasticsearchVersion = versions.Elasticsearch
	}

	absBundlePath, err := filepath.Abs(config.GetHostBundlePath())
	if err != nil {
		absBundlePath = config.GetHostBundlePath()
	}
	data.BundlePath = absBundlePath
	data.Redis = viper.GetBool("services.redis")
	if data.Redis {
		data.RedisVersion = versions.Redis
	}
	data.Mailpit = viper.GetBool("services.mailpit")
	data.Xdebug = viper.GetBool("services.php.xdebug")

	changed := false
	changed = writeDockerfile(internalDir, data) || changed
	changed = writeNginxConf(internalDir, data) || changed
	changed = writeEntrypoint(internalDir, data) || changed
	changed = writeEnvFile("templates/docker/.env", internalDir, data) || changed
	changed = writeEnvFile("templates/docker/.env.test", internalDir, data) || changed
	changed = writeInitDbSQL(internalDir, data) || changed

	changed = writeComposeFile(internalDir, "docker-compose.yml", data) || changed
	changed = writeComposeFile(internalDir, "docker-compose.setup.yml", data) || changed
	changed = writeComposeFile(internalDir, "docker-compose.test.yml", data) || changed

	return changed
}

func writeComposeFile(internalDir string, filename string, data any) bool {
	src := filepath.Join("templates/docker", filename)
	composeTemplate, err := fs.ReadFile(Templates, src)
	if err != nil {
		fmt.Printf("Warning: could not read template %s: %v\n", src, err)
		return false
	}

	tmpl, err := template.New(filename).Parse(string(composeTemplate))
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		panic(err)
	}

	dest := filepath.Join(internalDir, filename)
	oldContent, err := os.ReadFile(dest)
	if err == nil && bytes.Equal(oldContent, buf.Bytes()) {
		return false
	}

	err = os.WriteFile(dest, buf.Bytes(), 0644)
	if err != nil {
		panic(err)
	}

	return true
}

// GetBaseComposeArgs returns the base arguments to pass to docker compose,
// including project name and compose file path.
func GetBaseComposeArgs() []string {
	projectName := config.GetProjectName()
	internalDir := config.GetInternalDir()
	composeFile := filepath.Join(internalDir, "docker-compose.yml")
	args := []string{"-p", projectName, "-f", composeFile}

	// Add setup and test files if they exist
	setupFile := filepath.Join(internalDir, "docker-compose.setup.yml")
	if _, err := os.Stat(setupFile); err == nil {
		args = append(args, "-f", setupFile)
	}
	testFile := filepath.Join(internalDir, "docker-compose.test.yml")
	if _, err := os.Stat(testFile); err == nil {
		args = append(args, "-f", testFile)
	}

	return args
}

// RunComposeCommand runs docker compose with the provided arguments.
// It is a variable to allow overriding in tests.
var RunComposeCommand = func(args ...string) error {
	composeCmd := GetComposeCommand()

	argsToRun := append(composeCmd[1:], GetBaseComposeArgs()...)
	argsToRun = append(argsToRun, args...)

	cmd := exec.Command(composeCmd[0], argsToRun...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writeDockerfile(internalDir string, data any) bool {
	src := "templates/docker/Dockerfile"
	dockerfileContent, err := fs.ReadFile(Templates, src)
	if err != nil {
		fmt.Printf("Warning: could not read template %s: %v\n", src, err)
		return false
	}

	tmpl, err := template.New("dockerfile").Parse(string(dockerfileContent))
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		panic(err)
	}

	dest := filepath.Join(internalDir, "Dockerfile")
	oldContent, err := os.ReadFile(dest)
	if err == nil && bytes.Equal(oldContent, buf.Bytes()) {
		return false
	}

	err = os.WriteFile(dest, buf.Bytes(), 0644)
	if err != nil {
		panic(err)
	}

	return true
}

func writeEnvFile(path string, internalDir string, data any) bool {
	envContent, err := fs.ReadFile(Templates, path)
	if err != nil {
		fmt.Printf("Warning: could not read template %s: %v\n", path, err)
		return false
	}

	tmpl, err := template.New("env").Parse(string(envContent))
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		panic(err)
	}

	dest := filepath.Join(internalDir, filepath.Base(path))
	oldContent, err := os.ReadFile(dest)
	if err == nil && bytes.Equal(oldContent, buf.Bytes()) {
		return false
	}

	err = os.WriteFile(dest, buf.Bytes(), 0644)
	if err != nil {
		panic(err)
	}

	return true
}

func writeNginxConf(internalDir string, data any) bool {
	src := "templates/docker/nginx.conf"
	nginxContent, err := fs.ReadFile(Templates, src)
	if err != nil {
		fmt.Printf("Warning: could not read template %s: %v\n", src, err)
		return false
	}

	tmpl, err := template.New("nginx").Parse(string(nginxContent))
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		panic(err)
	}

	dest := filepath.Join(internalDir, "nginx.conf")
	oldContent, err := os.ReadFile(dest)
	if err == nil && bytes.Equal(oldContent, buf.Bytes()) {
		return false
	}

	err = os.WriteFile(dest, buf.Bytes(), 0644)
	if err != nil {
		panic(err)
	}

	return true
}

func writeInitDbSQL(internalDir string, data any) bool {
	src := "templates/docker/init-db.sql"
	content, err := fs.ReadFile(Templates, src)
	if err != nil {
		fmt.Printf("Warning: could not read template %s: %v\n", src, err)
		return false
	}

	tmpl, err := template.New("init-db").Parse(string(content))
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		panic(err)
	}

	dest := filepath.Join(internalDir, "init-db.sql")
	oldContent, err := os.ReadFile(dest)
	if err == nil && bytes.Equal(oldContent, buf.Bytes()) {
		return false
	}

	err = os.WriteFile(dest, buf.Bytes(), 0644)
	if err != nil {
		panic(err)
	}

	return true
}

func writeEntrypoint(internalDir string, data any) bool {
	src := "templates/docker/docker-entrypoint.sh"
	content, err := fs.ReadFile(Templates, src)
	if err != nil {
		fmt.Printf("Warning: could not read template %s: %v\n", src, err)
		return false
	}

	tmpl, err := template.New("entrypoint").Parse(string(content))
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		panic(err)
	}

	dest := filepath.Join(internalDir, "docker-entrypoint.sh")
	oldContent, err := os.ReadFile(dest)
	if err == nil && bytes.Equal(oldContent, buf.Bytes()) {
		return false
	}

	err = os.WriteFile(dest, buf.Bytes(), 0755)
	if err != nil {
		panic(err)
	}

	return true
}
