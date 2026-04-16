// Package docker provides helpers to generate and run Docker Compose for Orobox.
package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/spf13/viper"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"text/template"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/utils"
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
	domains := config.GetDomains()

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

// GetDatabaseCredentials returns the credentials to access the database.
func GetDatabaseCredentials() (user string, pass string, dbname string, composeServiceName string) {
	return GetDatabaseCredentialsFor(false)
}

// GetDatabaseTestCredentials returns the credentials to access the test database.
func GetDatabaseTestCredentials() (user string, pass string, dbname string, composeServiceName string) {
	return GetDatabaseCredentialsFor(true)
}

// GetDatabaseCredentialsFor returns the credentials to access the specified database environment.
func GetDatabaseCredentialsFor(test bool) (user string, pass string, dbname string, composeServiceName string) {
	internalDir := config.GetInternalDir()
	envFile := filepath.Join(internalDir, ".env")

	// Default values if anything fails
	user = "oro_db_user"
	pass = "oro_db_pass"
	composeServiceName = "db"
	if test {
		dbname = "oro_db_test"
		composeServiceName = "db-test"
	} else {
		dbname = "oro_db"
	}

	v := viper.New()
	v.SetConfigFile(envFile)
	v.SetConfigType("dotenv")
	if err := v.ReadInConfig(); err == nil {
		if u := v.GetString("ORO_DB_USER"); u != "" {
			user = u
		}
		if p := v.GetString("ORO_DB_PASSWORD"); p != "" {
			pass = p
		}
		if test {
			if db := v.GetString("ORO_DB_NAME_TEST"); db != "" {
				dbname = db
			}
		} else {
			if db := v.GetString("ORO_DB_NAME"); db != "" {
				dbname = db
			}
		}
	}

	// Check environment overrides
	if u := os.Getenv("ORO_DB_USER"); u != "" {
		user = u
	}
	if p := os.Getenv("ORO_DB_PASSWORD"); p != "" {
		pass = p
	}
	if test {
		if db := os.Getenv("ORO_DB_NAME_TEST"); db != "" {
			dbname = db
		}
	} else {
		if db := os.Getenv("ORO_DB_NAME"); db != "" {
			dbname = db
		}
	}

	return user, pass, dbname, composeServiceName
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
		Type                    string
		OroVersion              string
		PHPVersion              string
		NodeVersion             string
		NpmVersion              string
		PnpmVersion             string
		BundlePath              string
		Postgres                bool
		PostgresVersion         string
		Redis                   bool
		RedisVersion            string
		Mailpit                 bool
		RabbitMQ                bool
		RabbitMQVersion         string
		Elasticsearch           bool
		ElasticsearchVersion    string
		RedisInsight            bool
		Kibana                  bool
		Adminer                 bool
		InternalDir             string
		OroRootDir              string
		CustomBundle            string
		BundleNamespace         string
		Domains                 []config.DomainConfig
		MemoryLimit             string
		NginxHTTPPort           string
		NginxHTTPSPort          string
		PhpFpmPort              string
		HasSsl                  bool
		CertsPath               string
		UserRuntime             string
		UseTmpfs                bool
		TmpfsSize               string
		BundleRootContainerPath string
		BundlePackageName       string
	}{
		Type:                    viper.GetString("type"),
		InternalDir:             internalDir,
		OroRootDir:              config.OroRootDir,
		CustomBundle:            config.CustomBundlePath,
		BundleNamespace:         config.GetBundlePath(),
		MemoryLimit:             "2048M", // Default
		PhpFpmPort:              "9000",
		UserRuntime:             "www-data",
		UseTmpfs:                viper.GetBool("test.use_tmpfs"),
		TmpfsSize:               viper.GetString("test.tmpfs_size"),
		BundleRootContainerPath: config.GetBundleRootContainerPath(),
	}

	if data.TmpfsSize == "" {
		data.TmpfsSize = "1g"
	}

	if runtime.GOOS == "linux" {
		if currentUser, err := user.Current(); err == nil {
			data.UserRuntime = currentUser.Uid + ":" + currentUser.Gid
		}
	}

	data.NginxHTTPPort, data.NginxHTTPSPort = GetNginxPorts()

	data.Domains = config.GetDomains()
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
	data.PnpmVersion = versions.PNPM
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

	data.Kibana = data.Elasticsearch
	if viper.IsSet("services.kibana") {
		data.Kibana = viper.GetBool("services.kibana")
	}

	absBundlePath, err := filepath.Abs(config.GetHostBundlePath())
	if err != nil {
		absBundlePath = config.GetHostBundlePath()
	}
	data.BundlePath = absBundlePath

	// Try to get package name from composer.json
	composerJSONPath := filepath.Join(data.BundlePath, "composer.json")
	if content, err := os.ReadFile(composerJSONPath); err == nil {
		var composerData struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(content, &composerData); err == nil {
			data.BundlePackageName = composerData.Name
		} else {
			utils.PrintWarning(fmt.Sprintf("Could not parse composer.json in %s: %v", data.BundlePath, err))
		}
	} else if data.Type == config.InstallTypeBundle {
		utils.PrintWarning(fmt.Sprintf("composer.json not found in %s. Bundle package name will be unknown.", data.BundlePath))
	}

	data.Redis = viper.GetBool("services.redis")
	if data.Redis {
		data.RedisVersion = versions.Redis
	}

	data.RedisInsight = data.Redis
	if viper.IsSet("services.redisinsight") {
		data.RedisInsight = viper.GetBool("services.redisinsight")
	}

	data.Mailpit = viper.GetBool("services.mailpit")

	data.Adminer = data.Postgres
	if viper.IsSet("services.adminer") {
		data.Adminer = viper.GetBool("services.adminer")
	}

	changed := false
	// Only write all templates if we are in project-local mode (like in CI)
	if internalDir == ".orobox" {
		changed = writeDockerfile(internalDir, data) || changed
		changed = writeEntrypoint(internalDir, data) || changed
	}

	changed = writeEnvFile("templates/docker/.env", internalDir, data) || changed
	changed = writeEnvFile("templates/docker/.env.test", internalDir, data) || changed
	changed = writeNginxConf(internalDir, data) || changed
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
	args := []string{"-p", projectName, "--project-directory", internalDir}

	args = append(args, "-f", composeFile)

	// Add setup and test files if they exist
	setupFile := filepath.Join(internalDir, "docker-compose.setup.yml")
	if _, err := os.Stat(setupFile); err == nil {
		args = append(args, "-f", setupFile)
	}
	if includeTestFiles {
		testFile := filepath.Join(internalDir, "docker-compose.test.yml")
		if _, err := os.Stat(testFile); err == nil {
			args = append(args, "-f", testFile)
		}
	}

	return args
}

// RunComposeCommandSilently runs docker compose with the provided arguments
// and captures its output, showing it only if an error occurs.
// It shows a loader while running.
var RunComposeCommandSilently = func(message string, args ...string) error {
	debug := viper.GetBool("debug")
	if !debug {
		utils.StartLoader(message)
		defer utils.StopLoader()
	} else if message != "" {
		utils.PrintInfo(message)
	}

	composeCmd := GetComposeCommand()

	var argsToRun []string
	argsToRun = append(argsToRun, composeCmd[1:]...)

	// Add --progress quiet if it's 'docker compose' (V2) and not in debug mode
	if !debug && len(composeCmd) > 1 && composeCmd[1] == "compose" {
		argsToRun = append(argsToRun, "--progress", "quiet")
	}

	argsToRun = append(argsToRun, GetBaseComposeArgs()...)
	argsToRun = append(argsToRun, args...)

	cmd := exec.Command(composeCmd[0], argsToRun...)

	if debug {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		utils.StopLoader() // Stop loader before printing error
		stderrStr := stderr.String()
		if stderr.Len() > 0 {
			fmt.Print(stderrStr)
		}
		if stdout.Len() > 0 {
			fmt.Print(stdout.String())
		}

		if strings.Contains(stderrStr, "unauthorized: incorrect username or password") {
			utils.PrintWarning("\nDocker registry authentication failed.")
			utils.PrintInfo("This often happens when your Docker login has expired or is invalid for public images on Docker Hub.")
			utils.PrintInfo("Try running: docker logout")
		}

		return err
	}
	return nil
}

// RunSetupComposeCommandSilently is like RunComposeCommandSilently but enables
// the "setup" profile so that services marked with profiles: [setup] are accessible.
var RunSetupComposeCommandSilently = func(message string, args ...string) error {
	debug := viper.GetBool("debug")
	if !debug {
		utils.StartLoader(message)
		defer utils.StopLoader()
	} else if message != "" {
		utils.PrintInfo(message)
	}

	composeCmd := GetComposeCommand()

	var argsToRun []string
	argsToRun = append(argsToRun, composeCmd[1:]...)

	if !debug && len(composeCmd) > 1 && composeCmd[1] == "compose" {
		argsToRun = append(argsToRun, "--progress", "quiet")
	}

	argsToRun = append(argsToRun, "--profile", "setup")
	argsToRun = append(argsToRun, GetBaseComposeArgs()...)
	argsToRun = append(argsToRun, args...)

	cmd := exec.Command(composeCmd[0], argsToRun...)

	if debug {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		utils.StopLoader()
		stderrStr := stderr.String()
		if stderr.Len() > 0 {
			fmt.Print(stderrStr)
		}
		if stdout.Len() > 0 {
			fmt.Print(stdout.String())
		}
		return err
	}
	return nil
}

// RunComposeCommand runs docker compose with the provided arguments
// and connects to system stdout/stderr.
var RunComposeCommand = func(message string, args ...string) error {
	if message != "" {
		utils.PrintInfo(message)
	}
	debug := viper.GetBool("debug")
	composeCmd := GetComposeCommand()

	var argsToRun []string
	argsToRun = append(argsToRun, composeCmd[1:]...)

	// Add --progress quiet if it's 'docker compose' (V2) and not in debug mode
	if !debug && len(composeCmd) > 1 && composeCmd[1] == "compose" {
		argsToRun = append(argsToRun, "--progress", "quiet")
	}

	argsToRun = append(argsToRun, GetBaseComposeArgs()...)
	argsToRun = append(argsToRun, args...)

	cmd := exec.Command(composeCmd[0], argsToRun...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	var stderrBuf bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	err := cmd.Run()
	if err != nil && strings.Contains(stderrBuf.String(), "unauthorized: incorrect username or password") {
		utils.PrintWarning("\nDocker registry authentication failed.")
		utils.PrintInfo("This often happens when your Docker login has expired or is invalid for public images on Docker Hub.")
		utils.PrintInfo("Try running: docker logout")
	}
	return err
}

// PullAllLocalOrobotImages finds all local images from algoritmadev/orobox and pulls updates for them.
// It returns true if any image was updated.
func PullAllLocalOrobotImages() (bool, error) {
	// Filter for our images and get their IDs
	cmd := exec.Command("docker", "images", "--filter", "reference=algoritmadev/orobox:*", "--format", "{{.Repository}}:{{.Tag}} {{.ID}}", "--no-trunc")
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	beforeIDs := make(map[string]string)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var images []string
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			imageName := parts[0]
			imageID := parts[1]
			if !strings.HasSuffix(imageName, ":<none>") {
				beforeIDs[imageName] = imageID
				images = append(images, imageName)
			}
		}
	}

	if len(images) == 0 {
		return false, nil
	}

	var toPull []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	debug := viper.GetBool("debug")

	if !debug {
		utils.StartLoader("Checking for updates for local orobox images...")
		defer utils.StopLoader()
	}

	semaphore := make(chan struct{}, 4)
	for _, img := range images {
		wg.Add(1)
		go func(imageName string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			if needsPull(imageName) {
				mu.Lock()
				toPull = append(toPull, imageName)
				mu.Unlock()
			}
		}(img)
	}
	wg.Wait()

	if len(toPull) == 0 {
		if !debug {
			utils.StopLoader()
		}
		return false, nil
	}

	if !debug {
		utils.StopLoader()
		utils.StartLoader(fmt.Sprintf("Pulling %d updated orobox images...", len(toPull)))
	} else {
		utils.PrintInfo(fmt.Sprintf("Pulling %d updated orobox images...", len(toPull)))
	}

	wg = sync.WaitGroup{}
	for _, img := range toPull {
		wg.Add(1)
		go func(imageName string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			pullCmd := exec.Command("docker", "pull", imageName)
			if debug {
				pullCmd.Stdout = os.Stdout
				pullCmd.Stderr = os.Stderr
			}
			_ = pullCmd.Run()
		}(img)
	}
	wg.Wait()
	cmdAfter := exec.Command("docker", "images", "--filter", "reference=algoritmadev/orobox:*", "--format", "{{.Repository}}:{{.Tag}} {{.ID}}", "--no-trunc")
	outputAfter, err := cmdAfter.Output()
	if err != nil {
		return false, err
	}

	afterIDs := make(map[string]string)
	linesAfter := strings.Split(strings.TrimSpace(string(outputAfter)), "\n")
	for _, line := range linesAfter {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			afterIDs[parts[0]] = parts[1]
		}
	}

	updated := false
	for img, beforeID := range beforeIDs {
		if afterID, ok := afterIDs[img]; ok && afterID != beforeID {
			updated = true
			break
		}
	}

	return updated, nil
}

// PullProjectImages gets all images used by the current project and pulls updates for them.
// It returns true if any image was updated.
func PullProjectImages() (bool, error) {
	// Get all images from compose config
	composeCmd := GetComposeCommand()
	args := append(composeCmd[1:], GetBaseComposeArgs()...)
	args = append(args, "config", "--images")
	cmd := exec.Command(composeCmd[0], args...)
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	projectImages := make(map[string]bool)
	for _, img := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		img = strings.TrimSpace(img)
		if img != "" {
			// Some images might not have a tag in the config (defaults to latest)
			// But docker images will show them with :latest
			if !strings.Contains(img, ":") {
				img += ":latest"
			}
			projectImages[img] = true
		}
	}

	if len(projectImages) == 0 {
		return false, nil
	}

	var toPull []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	debug := viper.GetBool("debug")

	if !debug {
		utils.StartLoader("Checking for updates for project images...")
		defer utils.StopLoader()
	}

	semaphore := make(chan struct{}, 4)
	for img := range projectImages {
		wg.Add(1)
		go func(imageName string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			if needsPull(imageName) {
				mu.Lock()
				toPull = append(toPull, imageName)
				mu.Unlock()
			}
		}(img)
	}
	wg.Wait()

	if len(toPull) == 0 {
		if !debug {
			utils.StopLoader()
		}
		return false, nil
	}

	if !debug {
		utils.StopLoader()
		utils.StartLoader(fmt.Sprintf("Pulling %d updated project images...", len(toPull)))
	} else {
		utils.PrintInfo(fmt.Sprintf("Pulling %d updated project images...", len(toPull)))
	}

	// Helper to get map of relevant image IDs
	getImageIDs := func() map[string]string {
		ids := make(map[string]string)
		for img := range projectImages {
			cmdIDs := exec.Command("docker", "inspect", "-f", "{{.ID}}", img)
			outputIDs, err := cmdIDs.Output()
			if err == nil {
				ids[img] = strings.TrimSpace(string(outputIDs))
			}
		}
		return ids
	}

	beforeIDs := getImageIDs()

	// Pull only images that actually need it
	wg = sync.WaitGroup{}
	for _, img := range toPull {
		wg.Add(1)
		go func(imageName string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			pullCmd := exec.Command("docker", "pull", imageName)
			if debug {
				pullCmd.Stdout = os.Stdout
				pullCmd.Stderr = os.Stderr
			}
			_ = pullCmd.Run()
		}(img)
	}
	wg.Wait()

	afterIDs := getImageIDs()

	updated := false
	// Check for changed IDs
	for img, beforeID := range beforeIDs {
		if afterID, ok := afterIDs[img]; ok && afterID != beforeID {
			updated = true
			break
		}
	}

	// Check for newly pulled images
	if !updated {
		for img := range projectImages {
			if _, before := beforeIDs[img]; !before {
				if _, after := afterIDs[img]; after {
					updated = true
					break
				}
			}
		}
	}

	return updated, nil
}

func needsPull(imageName string) bool {
	// Get local image info
	localDigest, arch, osName, err := getLocalImageInfo(imageName)
	if err != nil {
		// If we can't get local info, maybe it's not even pulled yet, so pull it.
		return true
	}

	// Get remote image info
	remoteDigest, err := getRemoteImageDigest(imageName, arch, osName)
	if err != nil {
		// If we can't get remote info (e.g. manifest inspect failed), fallback to always pull
		return true
	}

	return localDigest != remoteDigest
}

func getLocalImageInfo(imageName string) (digest, arch, osName string, err error) {
	// Use inspect to get digests, architecture and os
	cmd := exec.Command("docker", "inspect", "-f", "{{range .RepoDigests}}{{.}} {{end}}|{{.Architecture}}|{{.Os}}", imageName)
	output, err := cmd.Output()
	if err != nil {
		return "", "", "", err
	}
	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(parts) < 3 {
		return "", "", "", fmt.Errorf("unexpected inspect output")
	}

	digests := strings.Fields(parts[0])
	for _, d := range digests {
		if idx := strings.Index(d, "@sha256:"); idx != -1 {
			digest = d[idx+1:]
			break
		}
	}
	arch = parts[1]
	osName = parts[2]
	return digest, arch, osName, nil
}

type manifestDescriptor struct {
	Digest   string `json:"digest"`
	Platform struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	} `json:"platform"`
}

type manifestVerboseEntry struct {
	Descriptor manifestDescriptor `json:"Descriptor"`
}

func getRemoteImageDigest(imageName, arch, osName string) (string, error) {
	// Use manifest inspect -v to get a consistent format for both single and multi-arch images
	cmd := exec.Command("docker", "manifest", "inspect", "-v", imageName)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// For a single image it might return a single object, but with -v it usually returns a list
	// Let's try to parse as a list first.
	var list []manifestVerboseEntry
	if err := json.Unmarshal(output, &list); err != nil {
		// Try as a single object
		var single manifestVerboseEntry
		if err := json.Unmarshal(output, &single); err != nil {
			return "", err
		}
		list = []manifestVerboseEntry{single}
	}

	for _, entry := range list {
		if entry.Descriptor.Platform.Architecture == arch && entry.Descriptor.Platform.OS == osName {
			return entry.Descriptor.Digest, nil
		}
	}

	return "", fmt.Errorf("no matching platform found")
}

// RunComposeCommandWithOutput runs docker compose and returns its combined output.
// It is a variable to allow overriding in tests.
var RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
	debug := viper.GetBool("debug")
	composeCmd := GetComposeCommand()

	var argsToRun []string
	argsToRun = append(argsToRun, composeCmd[1:]...)

	// Add --progress quiet if it's 'docker compose' (V2) and not in debug mode
	if !debug && len(composeCmd) > 1 && composeCmd[1] == "compose" {
		argsToRun = append(argsToRun, "--progress", "quiet")
	}

	argsToRun = append(argsToRun, GetBaseComposeArgs()...)
	argsToRun = append(argsToRun, args...)

	cmd := exec.Command(composeCmd[0], argsToRun...)
	if debug {
		var buf bytes.Buffer
		cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
		cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
		err := cmd.Run()
		output := buf.Bytes()
		if err != nil && strings.Contains(string(output), "unauthorized: incorrect username or password") {
			utils.PrintWarning("\nDocker registry authentication failed.")
			utils.PrintInfo("This often happens when your Docker login has expired or is invalid for public images on Docker Hub.")
			utils.PrintInfo("Try running: docker logout")
		}
		return output, err
	}
	output, err := cmd.CombinedOutput()
	if err != nil && strings.Contains(string(output), "unauthorized: incorrect username or password") {
		utils.PrintWarning("\nDocker registry authentication failed.")
		utils.PrintInfo("This often happens when your Docker login has expired or is invalid for public images on Docker Hub.")
		utils.PrintInfo("Try running: docker logout")
	}
	return output, err
}

// ServiceStatus represents the status of a Docker Compose service.
type ServiceStatus struct {
	Service string `json:"Service"`
	State   string `json:"State"`
	Health  string `json:"Health"`
}

var (
	ensuredServices      = make(map[string]bool)
	ensuredServicesMu    sync.Mutex
	dbInitializedCache   = make(map[bool]bool)
	dbInitializedCacheMu sync.Mutex
	includeTestFiles     = false
)

// SetIncludeTestFiles sets whether to include test-related compose files.
func SetIncludeTestFiles(include bool) {
	includeTestFiles = include
}

// ResetEnsuredServices resets the cache of ensured services and database states.
// This is primarily used for testing.
func ResetEnsuredServices() {
	ensuredServicesMu.Lock()
	defer ensuredServicesMu.Unlock()
	ensuredServices = make(map[string]bool)

	dbInitializedCacheMu.Lock()
	defer dbInitializedCacheMu.Unlock()
	dbInitializedCache = make(map[bool]bool)
}

// EnsureServicesRunning checks if the services are running and healthy.
// If any are not, it starts them with 'up -d'.
func EnsureServicesRunning(serviceNames []string) error {
	var servicesToStart []string
	var servicesToCheck []string

	ensuredServicesMu.Lock()
	for _, name := range serviceNames {
		if !ensuredServices[name] {
			servicesToCheck = append(servicesToCheck, name)
		}
	}
	ensuredServicesMu.Unlock()

	if len(servicesToCheck) == 0 {
		return nil
	}

	// Use docker compose ps --format json to check status of all services at once
	args := append([]string{"ps", "--format", "json"}, servicesToCheck...)
	output, err := RunComposeCommandWithOutput(args...)

	statusMap := make(map[string]ServiceStatus)
	if err == nil {
		// try to parse as array
		var statuses []ServiceStatus
		if jsonErr := json.Unmarshal(output, &statuses); jsonErr == nil {
			for _, s := range statuses {
				statusMap[s.Service] = s
			}
		} else {
			// Fallback for line-delimited or single object
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			for _, line := range lines {
				if line == "" {
					continue
				}
				var s ServiceStatus
				if jsonErr := json.Unmarshal([]byte(line), &s); jsonErr == nil {
					statusMap[s.Service] = s
				}
			}
		}
	}

	for _, name := range servicesToCheck {
		status, ok := statusMap[name]
		if ok && status.State == "running" && (status.Health == "" || status.Health == "healthy" || status.Health == "starting") {
			ensuredServicesMu.Lock()
			ensuredServices[name] = true
			ensuredServicesMu.Unlock()
		} else {
			servicesToStart = append(servicesToStart, name)
		}
	}

	if len(servicesToStart) == 0 {
		return nil
	}

	// If any service is not running/healthy, start them
	upArgs := append([]string{"up", "-d"}, servicesToStart...)
	msg := fmt.Sprintf("Starting services %s...", strings.Join(servicesToStart, ", "))
	if err := RunComposeCommandSilently(msg, upArgs...); err != nil {
		return err
	}

	ensuredServicesMu.Lock()
	for _, name := range servicesToStart {
		ensuredServices[name] = true
	}
	ensuredServicesMu.Unlock()

	return nil
}

// IsDatabaseInitialized checks if the database is initialized.
func IsDatabaseInitialized(test bool) (bool, error) {
	dbInitializedCacheMu.Lock()
	if initialized, ok := dbInitializedCache[test]; ok {
		dbInitializedCacheMu.Unlock()
		return initialized, nil
	}
	dbInitializedCacheMu.Unlock()

	dbUser, _, dbName, container := GetDatabaseCredentialsFor(test)

	checkArgs := []string{
		"exec", "-T", container,
		"psql",
		"-U", dbUser,
		"-d", dbName,
		"-c", "SELECT text_value FROM oro_config_value WHERE name = 'is_installed' AND section = 'oro_distribution';",
		"-t", "-A",
	}

	output, err := RunComposeCommandWithOutput(checkArgs...)
	if err != nil {
		// If the error is that the table doesn't exist, it's not initialized
		if strings.Contains(string(output), "relation \"oro_config_value\" does not exist") ||
			strings.Contains(string(output), "database \""+dbName+"\" does not exist") {
			SetDatabaseInitializedCache(test, false)
			return false, nil
		}
		return false, err
	}

	isInstalled := strings.TrimSpace(string(output)) == "1"
	SetDatabaseInitializedCache(test, isInstalled)
	return isInstalled, nil
}

// SetDatabaseInitializedCache updates the in-memory cache for the database state.
func SetDatabaseInitializedCache(test bool, initialized bool) {
	dbInitializedCacheMu.Lock()
	defer dbInitializedCacheMu.Unlock()
	dbInitializedCache[test] = initialized
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
	filename := filepath.Base(path)

	// If file exists in current directory, use it instead of template
	if _, err := os.Stat(filename); err == nil {
		content, err := os.ReadFile(filename)
		if err != nil {
			fmt.Printf("Warning: could not read local file %s: %v\n", filename, err)
			return false
		}

		dest := filepath.Join(internalDir, filename)
		oldContent, err := os.ReadFile(dest)
		if err == nil && bytes.Equal(oldContent, content) {
			return false
		}

		err = os.WriteFile(dest, content, 0644)
		if err != nil {
			panic(err)
		}
		return true
	}

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
