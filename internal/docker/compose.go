package docker

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"orobox/internal/config"

	"github.com/spf13/viper"
)

var memoizedComposeCmd []string

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

func EnsureDockerCompose() bool {
	internalDir := config.GetInternalDir()
	err := os.MkdirAll(internalDir, 0755)
	if err != nil {
		panic(err)
	}

	data := struct {
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
		NginxHttpPort        string
		NginxHttpsPort       string
		PhpFpmPort           string
		HasSsl               bool
		CertsPath            string
	}{
		InternalDir:     internalDir,
		OroRootDir:      config.OroRootDir,
		CustomBundle:    config.CustomBundlePath,
		BundleNamespace: config.GetBundlePath(),
		MemoryLimit:     "2048M", // Default
		PhpFpmPort:      "9000",
	}

	data.NginxHttpPort = viper.GetString("nginx_http_port")
	if data.NginxHttpPort == "" {
		data.NginxHttpPort = os.Getenv("ORO_NGINX_HTTP_PORT")
	}
	if data.NginxHttpPort == "" {
		data.NginxHttpPort = "8080"
	}

	data.NginxHttpsPort = viper.GetString("nginx_https_port")
	if data.NginxHttpsPort == "" {
		data.NginxHttpsPort = os.Getenv("ORO_NGINX_HTTPS_PORT")
	}
	if data.NginxHttpsPort == "" {
		data.NginxHttpsPort = "8443"
	}

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

	data.PHPVersion = viper.GetString("services.php_version")
	if data.PHPVersion == "" {
		data.PHPVersion = versions.PHP
	}

	data.NodeVersion = viper.GetString("services.node_version")
	if data.NodeVersion == "" {
		data.NodeVersion = versions.Node
	}

	data.NpmVersion = viper.GetString("services.npm_version")
	if data.NpmVersion == "" {
		data.NpmVersion = versions.NPM
	}

	data.PostgresVersion = viper.GetString("services.postgres")
	if data.PostgresVersion == "" || data.PostgresVersion == "true" {
		data.PostgresVersion = versions.Postgres
	}
	data.Postgres = true

	data.RabbitMQVersion = viper.GetString("services.rabbitmq")
	data.RabbitMQ = data.RabbitMQVersion != "" && data.RabbitMQVersion != "false"
	if data.RabbitMQ && data.RabbitMQVersion == "true" {
		data.RabbitMQVersion = versions.RabbitMQ
	}

	data.ElasticsearchVersion = viper.GetString("services.elasticsearch")
	data.Elasticsearch = data.ElasticsearchVersion != "" && data.ElasticsearchVersion != "false"
	if data.Elasticsearch && data.ElasticsearchVersion == "true" {
		data.ElasticsearchVersion = versions.Elasticsearch
	}

	absBundlePath, err := filepath.Abs(config.GetHostBundlePath())
	if err != nil {
		absBundlePath = config.GetHostBundlePath()
	}
	data.BundlePath = absBundlePath
	data.RedisVersion = viper.GetString("services.redis")
	data.Redis = data.RedisVersion != "" && data.RedisVersion != "false"
	if data.Redis && data.RedisVersion == "true" {
		data.RedisVersion = versions.Redis
	}
	data.Mailpit = viper.GetBool("services.mailpit")

	changed := false
	changed = writeDockerfile(internalDir, data) || changed
	changed = writeNginxConf(internalDir, data) || changed
	changed = writeEntrypoint(internalDir, data) || changed
	changed = writeEnvFile(internalDir, data) || changed
	changed = writeInitDbSql(internalDir, data) || changed

	src := "templates/docker/docker-compose.yml"
	composeTemplate, err := fs.ReadFile(Templates, src)
	if err != nil {
		fmt.Printf("Warning: could not read template %s: %v\n", src, err)
		return changed
	}

	tmpl, err := template.New("compose").Parse(string(composeTemplate))
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		panic(err)
	}

	composeFile := filepath.Join(internalDir, "docker-compose.yml")
	oldContent, err := os.ReadFile(composeFile)
	if err == nil && bytes.Equal(oldContent, buf.Bytes()) {
		return changed
	}

	err = os.WriteFile(composeFile, buf.Bytes(), 0644)
	if err != nil {
		panic(err)
	}

	return true
}

func GetBaseComposeArgs() []string {
	projectName := config.GetProjectName()
	internalDir := config.GetInternalDir()
	composeFile := filepath.Join(internalDir, "docker-compose.yml")
	return []string{"-p", projectName, "-f", composeFile}
}

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

func writeEnvFile(internalDir string, data any) bool {
	src := "templates/docker/.env"
	envContent, err := fs.ReadFile(Templates, src)
	if err != nil {
		fmt.Printf("Warning: could not read template %s: %v\n", src, err)
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

	dest := filepath.Join(internalDir, ".env")
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

func writeInitDbSql(internalDir string, data any) bool {
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
