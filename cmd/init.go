// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/algoritma-dev/orobox/internal/certificates"
	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	yamlv3 "gopkg.in/yaml.v3"
)

var (
	bundlePath      string
	oroVersion      string
	bundleNamespace string
	installType     string
	forceInstall    bool
	stdin           io.Reader = os.Stdin
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the development environment",
	// A clone, a composer install or an oro:install that fails is a runtime problem, not a
	// usage problem: printing the flag list after it buries the actual error under help text.
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		absPath, err := filepath.Abs(bundlePath)
		if err != nil {
			panic(err)
		}
		bundlePath = absPath

		err = os.MkdirAll(bundlePath, 0755)
		if err != nil {
			panic(err)
		}

		if err := os.Chdir(bundlePath); err != nil {
			panic(err)
		}

		generateConfig()

		// Reload config after generation
		viper.SetConfigFile(".orobox.yaml")
		if err := viper.ReadInConfig(); err != nil {
			utils.PrintWarning(fmt.Sprintf("Could not read configuration: %v", err))
		}

		certificates.InstallSslCertificates()

		// Check hosts file
		var missingHosts []string
		for _, domain := range config.GetDomains() {
			if !utils.CheckHostInEtcHosts(domain.Host) {
				missingHosts = append(missingHosts, domain.Host)
			}
		}

		docker.EnsureDockerCompose()

		// The failing step has already printed what went wrong; the error exists so Execute
		// exits 1. Returning nil here would report a bootstrap that never happened as a
		// success, and every caller — CI job, Makefile, e2e harness — reads the exit code.
		if !performInstallation() {
			return errors.New("environment initialization failed")
		}

		utils.PrintSuccess("Environment initialized successfully!")

		if len(missingHosts) > 0 {
			utils.PrintTitle("Missing domains in hosts file")
			utils.PrintWarning("The following domains are missing from your hosts file. Please add them manually to /etc/hosts:")
			for _, host := range missingHosts {
				fmt.Printf("127.0.0.1 %s\n", host)
			}
		}
		return nil
	},
}

// performInstallation is a variable so a test can drive the command's exit code without
// standing up Docker, a clone and a full oro:install.
var performInstallation = func() bool {
	var conf config.OroConfig
	if err := viper.Unmarshal(&conf); err != nil {
		utils.PrintError(fmt.Sprintf("Error reading config: %v", err))
		return false
	}

	strategy, err := config.InstallTypeFor(conf.Type)
	if err != nil {
		utils.PrintError(fmt.Sprintf("%v", err))
		return false
	}

	// EnsureDockerCompose has already written the internal env files, so project and demo
	// checkouts can be seeded before anything starts or clones.
	seedProjectEnvFiles(strategy)

	// Remove any existing containers to ensure fresh bind mounts after init.
	// If vendor-oro was deleted and recreated, running containers would still hold
	// a bind mount to the old (deleted) inode, causing an empty vendor inside containers.
	if err := docker.RunComposeCommandSilently("Stopping existing containers...", "down", "--remove-orphans"); err != nil {
		utils.PrintWarning(fmt.Sprintf("Could not stop existing containers: %v", err))
	}

	// 0. Resolve OroCommerce version to the latest tag
	oroRepo := "https://github.com/oroinc/orocommerce-application.git"
	resolvedVersion, err := utils.GetLatestTag(oroRepo, conf.OroVersion)
	if err != nil {
		utils.PrintWarning(fmt.Sprintf("Could not resolve latest tag for %s, using it as is: %v", conf.OroVersion, err))
		resolvedVersion = conf.OroVersion
	}

	// Ensure an SSH agent with a loaded key is available before any composer/git
	// run that clones a private SSH repository, so credentials forward into the
	// containers. No-op when no SSH repo is configured or an agent already exists.
	sshCleanup, err := docker.EnsureSSHAgent(conf.Composer, oroRepo)
	if err != nil {
		utils.PrintError(err.Error())
		return false
	}
	defer sshCleanup()

	// 1. Download sources (git clone)
	// (Project support removed from main branch)

	// 2. Ensure environment is ready.
	// gotenberg is always required by the install service.
	services := []string{"up", "-d", "db", "gotenberg"}
	if conf.Services.Redis {
		services = append(services, "redis")
	}
	if conf.Services.RabbitMQ {
		services = append(services, "rabbitmq")
	}
	if conf.Services.Elasticsearch {
		services = append(services, "elasticsearch")
	}
	if conf.Services.Mailpit {
		services = append(services, "mail")
	}
	if err := docker.RunComposeCommandSilently("Starting services for installation...", services...); err != nil {
		utils.PrintError(fmt.Sprintf("Failed to start services: %v", err))
		return false
	}

	// `up -d` returns before Postgres listens, and the database this just started is the one
	// oro:install writes into. On a first start it also has to initdb and run init-db.sql.
	if err := docker.WaitForDatabaseReady(false); err != nil {
		utils.PrintError(err.Error())
		return false
	}

	// Run volume-init to fix permissions before any composer/git command.
	//
	// Every one-off container below runs with --no-deps (see initRunArgs): the services we
	// need are already started explicitly above, and the `application` service depends_on
	// web/consumer/cron, so without it Compose would start the whole long-running stack
	// before OroCommerce exists. Those services cannot boot without an installed database,
	// so `restart: unless-stopped` puts them in a restart loop, and every retry compiles the
	// Symfony container into var/cache/dev. That races oro:install, whose
	// "rm -rf var/cache/*" then fails with "can't remove 'var/cache/dev': Directory not
	// empty", leaving a half-written entity-config cache behind. oro:install later reads it
	// and dies in oro:entity-extend:cache:clear with "ConfigCache::getCacheKey(): Argument #1
	// ($key) must be of type string, null given".
	if err := docker.RunComposeCommandSilently("Ensuring permissions...", initRunArgs("volume-init")...); err != nil {
		utils.PrintWarning(fmt.Sprintf("volume-init failed: %v", err))
	}

	// runOroInstall gates step 5 (oro:install). A fresh scaffold always installs;
	// an existing project checkout (composer.json present) is preserved unless the
	// user opts in, because oro:install resets the database.
	runOroInstall := true

	// 3. For bundle, we need to clone into the volume if not already there
	// Always try to clone if composer.json is missing in the container
	checkCmd := initRunArgs("application", "test", "-f", "composer.json")
	utils.StartLoader("Checking for OroCommerce installation...")
	_, err = docker.RunComposeCommandWithOutput(checkCmd...)
	utils.StopLoader()
	if err != nil {
		// No composer.json in the source root: bootstrap a full OroCommerce application.
		// For bundle this lands in the oro_app volume; for project the source root is the
		// bind-mounted checkout, so the app is scaffolded directly into the user's repo.
		// A temporary clone dir avoids "directory not empty" errors when something is
		// already mounted at the destination.
		scaffoldMsg := "Downloading and installing OroCommerce into volume..."
		if strategy.BindWholeRepo() {
			scaffoldMsg = "Scaffolding OroCommerce application into the project checkout..."
		}
		// Drop orobox-managed env files from the clone before copying: they are
		// single-file bind mounts at OroRoot, and cp cannot replace a mount point
		// ("can't create ... File exists"). The mounted versions are authoritative.
		cloneCmd := initRunArgs()
		cloneCmd = append(cloneCmd, docker.CredentialRunArgs(conf.Composer, oroRepo)...)
		cloneCmd = append(cloneCmd, "application", "bash", "-c",
			fmt.Sprintf("git clone -b %s --depth 1 %s /tmp/oro-app && rm -f /tmp/oro-app/.env-app.local /tmp/oro-app/.env-app.test && cp -rf /tmp/oro-app/. . && rm -rf /tmp/oro-app && composer install", resolvedVersion, oroRepo))
		if err := docker.RunComposeCommandSilently(scaffoldMsg, cloneCmd...); err != nil {
			utils.PrintError(fmt.Sprintf("OroCommerce download/install failed: %v", err))
			return false
		}
	} else {
		// Sources present. For project installs the source root is the user's
		// bind-mounted checkout; running oro:install would reset an existing
		// database. Ask before doing so (default no), unless --force-install.
		// In non-interactive runs the reader hits EOF, AskYesNo returns its
		// default (false), so oro:install is skipped.
		if strategy.BindWholeRepo() && !forceInstall {
			reader := bufio.NewReader(stdin)
			runOroInstall = utils.AskYesNo(reader, "OroCommerce already present (composer.json found). Run oro:install? This resets the database", false)
		}

		// Sources present: check for vendors (especially if vendor-oro was just added)
		checkVendor := initRunArgs("application", "test", "-f", "vendor/autoload.php")
		utils.StartLoader("Checking for vendors...")
		_, errVendor := docker.RunComposeCommandWithOutput(checkVendor...)
		utils.StopLoader()
		if errVendor != nil {
			installCmd := initRunArgs()
			installCmd = append(installCmd, docker.CredentialRunArgs(conf.Composer)...)
			installCmd = append(installCmd, "application", "composer", "install")
			if err := docker.RunComposeCommandSilently("Installing dependencies...", installCmd...); err != nil {
				utils.PrintError(fmt.Sprintf("Composer install failed: %v", err))
				return false
			}
		}
	}

	// 4. Install bundle into vendor-oro via composer require (runs in 'application'
	// where vendor-oro is already mounted as the vendor directory).
	// Project installs vendor from their own composer.lock, so this step is bundle-only.
	if bundlePackageName := getBundlePackageName(); strategy.RunsComposerRequire() && bundlePackageName != "" {
		bundleNamespace := config.GetBundlePath()
		bashCmd := fmt.Sprintf(
			`COMPOSER_ALLOW_SUPERUSER=1 composer config repositories.bundle '{"type":"path","url":"bundles/%s","options":{"symlink":true}}'`,
			bundleNamespace,
		)
		for i, repo := range conf.Composer.Repositories {
			repoJSON, err := json.Marshal(repo)
			if err != nil {
				utils.PrintWarning(fmt.Sprintf("Could not serialize composer repository %d: %v", i, err))
				continue
			}
			bashCmd += fmt.Sprintf(
				` && COMPOSER_ALLOW_SUPERUSER=1 composer config repositories.orobox_%d '%s'`,
				i, string(repoJSON),
			)
		}
		bashCmd += fmt.Sprintf(
			` && COMPOSER_ALLOW_SUPERUSER=1 composer require "%s:@dev" --no-interaction --no-scripts`,
			bundlePackageName,
		)
		requireCmd := initRunArgs()
		requireCmd = append(requireCmd, docker.CredentialRunArgs(conf.Composer)...)
		requireCmd = append(requireCmd, "application", "bash", "-c", bashCmd)
		if err := docker.RunComposeCommandSilently("Installing bundle into vendor...", requireCmd...); err != nil {
			utils.PrintWarning(fmt.Sprintf("Bundle installation failed: %v", err))
		}
	}

	// 5. Run Oro installation.
	// We run volume-setup first for permissions, then install with --no-deps
	// because all dependencies (db, gotenberg, etc.) are already running above.
	// Using --no-deps avoids Docker Compose's dependency resolution, which can
	// trigger a "network not found" error when it tries to (re)start containers
	// whose network was replaced by the earlier `down --remove-orphans`.
	if !runOroInstall {
		utils.PrintInfo("Skipping OroCommerce installation (existing project preserved).")
		return true
	}

	volumeSetupCmd := initRunArgs()
	volumeSetupCmd = append(volumeSetupCmd, docker.CredentialRunArgs(conf.Composer)...)
	volumeSetupCmd = append(volumeSetupCmd, "volume-setup")
	if err := docker.RunSetupComposeCommandSilently("Setting up volumes for installation...", volumeSetupCmd...); err != nil {
		utils.PrintWarning(fmt.Sprintf("volume-setup failed: %v", err))
	}
	if err := docker.RunSetupComposeCommandSilently("Running OroCommerce installation (this may take several minutes)...", "run", "--rm", "-T", "--no-deps", "install"); err != nil {
		utils.PrintError(fmt.Sprintf("OroCommerce installation failed: %v", err))
		return false
	}

	return true
}

// initRunArgs builds the leading `docker compose run` arguments for the one-off containers
// init uses, appending any extra arguments (service name and command).
//
// --no-deps is the important part: init runs before OroCommerce is installed, and the
// `application` service depends_on web, consumer and cron. Without it Compose starts that
// whole long-running stack, which cannot boot against an empty database and therefore
// restart-loops (restart: unless-stopped), recompiling the Symfony container into
// var/cache/dev on every retry. That concurrent writer makes oro:install's
// "rm -rf var/cache/*" fail ("can't remove 'var/cache/dev': Directory not empty") and the
// surviving half-written entity-config cache then crashes
// oro:entity-extend:cache:clear with a null cache key. The services init genuinely needs
// (db, gotenberg and the optional ones) are started explicitly beforehand.
func initRunArgs(extra ...string) []string {
	args := []string{"run", "--rm", "-T", "--no-deps"}
	return append(args, extra...)
}

// seedProjectEnvFiles copies Orobox's generated env files into the checkout for install
// types that no longer bind-mount them (project, demo), so a freshly scaffolded
// application starts with working DSNs. An existing file is never touched: from the first
// init onwards the project owns these files. This runs only from init — wiring it into the
// EnsureDockerCompose path shared by up, run, test and friends would clobber the project's
// own edits on every command.
func seedProjectEnvFiles(strategy config.InstallType) {
	if strategy.MountsInternalEnvFiles() {
		return
	}

	internalDir := config.GetInternalDir()
	repoDir := config.GetHostBundlePath()

	seeds := []struct{ src, dst string }{
		{filepath.Join(internalDir, ".env"), filepath.Join(repoDir, ".env-app.local")},
		{filepath.Join(internalDir, ".env.test"), filepath.Join(repoDir, ".env-app.test")},
	}

	for _, seed := range seeds {
		if _, err := os.Stat(seed.dst); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			utils.PrintWarning(fmt.Sprintf("Could not check %s: %v", seed.dst, err))
			continue
		}

		content, err := os.ReadFile(seed.src)
		if err != nil {
			utils.PrintWarning(fmt.Sprintf("Could not read %s: %v", seed.src, err))
			continue
		}
		if err := os.WriteFile(seed.dst, content, 0644); err != nil {
			utils.PrintWarning(fmt.Sprintf("Could not seed %s: %v", seed.dst, err))
			continue
		}
		utils.PrintInfo(fmt.Sprintf("Seeded %s from %s (the project owns it from now on).", filepath.Base(seed.dst), seed.src))
	}
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringVarP(&bundlePath, "bundle-path", "b", ".", "Bundle path")
	initCmd.Flags().StringVarP(&oroVersion, "oro-version", "v", "6.1", "OroCommerce version")
	initCmd.Flags().StringVarP(&bundleNamespace, "bundle-namespace", "n", "", "Bundle namespace")
	initCmd.Flags().StringVarP(&installType, "type", "t", "", "Installation type (bundle|project|demo)")
	initCmd.Flags().BoolVar(&forceInstall, "force-install", false, "Force oro:install even if the project already has composer.json")
}

// getBundlePackageName reads the composer package name from the bundle's composer.json.
func getBundlePackageName() string {
	composerJSONPath := filepath.Join(config.GetHostBundlePath(), "composer.json")
	content, err := os.ReadFile(composerJSONPath)
	if err != nil {
		return ""
	}
	var data struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(content, &data); err != nil {
		return ""
	}
	return data.Name
}

func generateConfig() {
	configPath := ".orobox.yaml"
	if _, err := os.Stat(configPath); err == nil {
		// Config already exists, validate it
		if validateConfig() {
			return
		}
		utils.PrintWarning("Config file .orobox.yaml is invalid. Let's recreate it.")
	} else if !errors.Is(err, os.ErrNotExist) {
		utils.PrintWarning(fmt.Sprintf("Warning checking %s: %v", configPath, err))
		return
	}

	utils.PrintTitle("Config file .orobox.yaml not found or invalid. Let's create it interactively.")
	reader := bufio.NewReader(stdin)

	typeOfInstall := installType
	if typeOfInstall == "" {
		typeOfInstall = utils.AskSelection(reader, "Installation type",
			[]string{config.InstallTypeBundle, config.InstallTypeProject, config.InstallTypeDemo}, config.InstallTypeBundle)
	}

	strategy, err := config.InstallTypeFor(typeOfInstall)
	if err != nil {
		utils.PrintWarning(fmt.Sprintf("%v; falling back to %q", err, config.InstallTypeBundle))
		typeOfInstall = config.InstallTypeBundle
		strategy, _ = config.InstallTypeFor(typeOfInstall)
	}

	var className, namespace string
	if strategy.RequiresBundleNamespace() {
		bundleClass := utils.AskQuestion(reader, "Full bundle class (eg: Algoritma\\Bundle\\TestBundle\\TestBundle)", "")

		if bundleClass != "" {
			var found bool
			className, namespace, _, found = config.FindPhpClass(".", bundleClass)
			if !found {
				utils.PrintWarning(fmt.Sprintf("PHP class for %s not found in current directory or subdirectories.", bundleClass))
				// Manual parsing if not found
				lastSlash := strings.LastIndex(bundleClass, "\\")
				if lastSlash != -1 {
					className = bundleClass[lastSlash+1:]
					namespace = bundleClass[:lastSlash]
				} else {
					className = bundleClass
					namespace = ""
				}
			} else {
				utils.PrintInfo(fmt.Sprintf("Found class %s in namespace %s", className, namespace))
			}
		}
	}

	version := utils.AskSelection(reader, "OroCommerce version", config.SupportedOroVersions, oroVersion)
	host := utils.AskQuestion(reader, "Main domain host", "oro.demo")
	root := utils.AskQuestion(reader, "Main domain root", "public")
	ssl := utils.AskYesNo(reader, "Enable SSL?", true)

	redisEnabled := utils.AskYesNo(reader, "Enable Redis?", false)
	redisInsightEnabled := false
	if redisEnabled {
		redisInsightEnabled = utils.AskYesNo(reader, "Enable RedisInsight?", true)
	}

	mailpit := utils.AskYesNo(reader, "Enable Mailpit?", true)

	rabbitmqEnabled := utils.AskYesNo(reader, "Enable RabbitMQ?", false)
	elasticsearchEnabled := utils.AskYesNo(reader, "Enable Elasticsearch?", false)

	kibanaEnabled := false
	if elasticsearchEnabled {
		kibanaEnabled = utils.AskYesNo(reader, "Enable Kibana?", true)
	}

	adminerEnabled := utils.AskYesNo(reader, "Enable Adminer?", true)

	conf := config.OroConfig{
		Type:       typeOfInstall,
		Class:      className,
		Namespace:  namespace,
		OroVersion: version,
		Domains: []config.DomainConfig{
			{
				Host: host,
				Root: root,
				Ssl:  ssl,
			},
		},
		Services: config.ServicesConfig{
			Redis:         redisEnabled,
			RedisInsight:  redisInsightEnabled,
			Mailpit:       mailpit,
			RabbitMQ:      rabbitmqEnabled,
			Elasticsearch: elasticsearchEnabled,
			Kibana:        kibanaEnabled,
			Adminer:       adminerEnabled,
		},
		Test: config.TestConfig{
			UseTmpfs: false,
		},
	}

	data, err := yamlv3.Marshal(&conf)
	if err != nil {
		utils.PrintWarning(fmt.Sprintf("Yaml marshal error: %s", err))
		return
	}

	err = os.WriteFile(configPath, data, 0644)
	if err != nil {
		utils.PrintWarning(fmt.Sprintf("Write config error: %s", err))
	}
}

func validateConfig() bool {
	configPath := ".orobox.yaml"
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	c, err := config.ParseConfig(data)
	if err != nil {
		utils.PrintError(fmt.Sprintf("Validation error: %v", err))
		return false
	}
	if err := c.Validate(); err != nil {
		utils.PrintError(fmt.Sprintf("Validation error: %v", err))
		return false
	}
	return true
}
