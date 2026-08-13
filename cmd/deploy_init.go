// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/pipeline"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var deployInitCmd = &cobra.Command{
	Use:   "deploy-init",
	Short: "Install PHP Deployer and create the deploy configuration",
	Long: `Installs PHP Deployer into the isolated vendor-bin/deploy namespace, asks for the
deploy stages, writes them into .orobox.yaml and generates deploy.php plus the Oro recipe.

deploy.php is created only once and is yours afterwards; the recipe in
vendor-bin/deploy/orobox/oro.php is rewritten on every run so recipe fixes reach the project.`,
	Run: func(_ *cobra.Command, _ []string) {
		var conf config.OroConfig
		if err := viper.Unmarshal(&conf); err != nil {
			utils.PrintError(fmt.Sprintf("Error reading config: %v", err))
			os.Exit(1)
		}

		if conf.Type != config.InstallTypeProject {
			utils.PrintError(fmt.Sprintf("deploy-init is only available for install type %q; this project is %q, whose checkout is not a deployable application",
				config.InstallTypeProject, conf.Type))
			os.Exit(1)
		}

		runDeployInitCommand(&conf)
	},
}

func init() {
	rootCmd.AddCommand(deployInitCmd)
}

func runDeployInitCommand(conf *config.OroConfig) {
	docker.EnsureDockerCompose()

	if !installDeployer() {
		os.Exit(1)
	}

	deployConf := askDeployConfig(conf)
	conf.Deploy = deployConf

	configPath := ".orobox.yaml"
	if err := config.SaveConfig(configPath, conf); err != nil {
		utils.PrintError(fmt.Sprintf("Could not write %s: %v", configPath, err))
		os.Exit(1)
	}
	utils.PrintSuccess("Deploy configuration written to " + configPath)

	if err := writeDeployFiles(config.GetHostBundlePath()); err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}

	utils.PrintSuccess("Deployer initialized. Commit deploy.php, vendor-bin/deploy/composer.json and composer.lock.")
	utils.PrintInfo("Then run 'orobox deploy <stage>'.")
}

// installDeployer installs Deployer into its own bamarni bin namespace, the same way qa-init
// installs the QA tools: an isolated dependency graph that cannot clash with OroCommerce's
// locked versions.
func installDeployer() bool {
	deployDir := config.DeployToolsDir
	oroRoot := config.OroRootDir

	initCmd := fmt.Sprintf(
		`mkdir -p %s && [ -f %s/composer.json ] || printf '{"name":"orobox/deploy-tools"}' > %s/composer.json`,
		deployDir, deployDir, deployDir,
	)
	steps := []struct {
		msg  string
		args []string
	}{
		{"Preparing the deploy namespace...", []string{"exec", "-T", "application", "sh", "-c", initCmd}},
		{
			"Configuring bamarni/composer-bin-plugin...",
			[]string{"exec", "-w", oroRoot, "-T", "application", "composer", "config", "--no-plugins", "allow-plugins.bamarni/composer-bin-plugin", "true"},
		},
		{
			"Installing bamarni/composer-bin-plugin...",
			[]string{"exec", "-w", oroRoot, "-T", "application", "composer", "require", "--dev", "--no-scripts", "bamarni/composer-bin-plugin"},
		},
		{
			"Installing deployer/deployer...",
			[]string{"exec", "-w", oroRoot, "-T", "application", "composer", "bin", "deploy", "require", "--dev", "--no-interaction", "deployer/deployer"},
		},
	}

	for _, step := range steps {
		if err := docker.RunComposeCommandSilently(step.msg, step.args...); err != nil {
			utils.PrintError(fmt.Sprintf("%s failed: %v", step.msg, err))
			return false
		}
	}

	utils.PrintSuccess("PHP Deployer installed in vendor-bin/deploy.")
	return true
}

// askDeployConfig prompts for the deploy block, keeping any existing values as defaults so a
// second run is an edit rather than a restart.
func askDeployConfig(conf *config.OroConfig) *config.DeployConfig {
	reader := bufio.NewReader(stdin)

	existing := conf.Deploy
	if existing == nil {
		existing = &config.DeployConfig{}
	}

	defaultRepo := existing.Repository
	if defaultRepo == "" {
		if origin, err := config.GitOriginURL(); err == nil {
			defaultRepo = origin
		}
	}

	utils.PrintTitle("Deploy configuration")
	deployConf := &config.DeployConfig{
		Repository: utils.AskQuestion(reader, "Repository URL cloned on the remote host", defaultRepo),
		// A monorepo holds the application in a subdirectory; the pipeline builds from it and
		// Deployer extracts only it, so the release still looks like a plain Oro checkout.
		SourceDir: utils.AskQuestion(reader,
			"Repository subdirectory holding the application (empty when the repository root is the application)",
			existing.Source()),
		PreBuiltAssetsEnabled: utils.AskYesNo(reader,
			"Does the repository already ship pre-built assets? (no means the pipeline builds and uploads them)",
			existing.PreBuiltAssetsEnabled),
	}

	for i := 0; ; i++ {
		var defaults config.StageConfig
		if i < len(existing.Stages) {
			defaults = existing.Stages[i]
		}

		stage := askStage(reader, defaults)
		deployConf.Stages = append(deployConf.Stages, stage)

		if !utils.AskYesNo(reader, "Add another stage?", i+1 < len(existing.Stages)) {
			break
		}
	}

	return deployConf
}

func askStage(reader *bufio.Reader, defaults config.StageConfig) config.StageConfig {
	utils.PrintTitle("Stage")

	name := utils.AskQuestion(reader, "Stage name", orDefault(defaults.Name, "production"))
	stage := config.StageConfig{
		Name:       name,
		Ref:        utils.AskQuestion(reader, "Git ref to build and deploy", orDefault(defaults.Ref, "main")),
		Host:       utils.AskQuestion(reader, "Remote host", defaults.Host),
		User:       utils.AskQuestion(reader, "Remote SSH user", orDefault(defaults.User, "deploy")),
		DeployPath: utils.AskQuestion(reader, "Remote deploy path", orDefault(defaults.DeployPath, "/var/www/oro")),
	}

	stage.Port = askInt(reader, "Remote SSH port", defaults.SSHPort())
	stage.KeepReleases = askInt(reader, "Releases to keep on the remote", defaults.Releases())

	suites := utils.AskQuestion(reader, "Test suites to run (comma separated: unit, functional)", strings.Join(defaults.Suites(), ","))
	stage.TestSuites = splitSuites(suites)

	stage.RestartCommand = utils.AskQuestion(reader, "Command to restart consumers/cron on the remote (empty to skip)", defaults.RestartCommand)

	return stage
}

func askInt(reader *bufio.Reader, question string, defaultValue int) int {
	answer := utils.AskQuestion(reader, question, strconv.Itoa(defaultValue))
	value, err := strconv.Atoi(strings.TrimSpace(answer))
	if err != nil || value <= 0 {
		utils.PrintWarning(fmt.Sprintf("%q is not a positive number; using %d.", answer, defaultValue))
		return defaultValue
	}
	return value
}

func splitSuites(answer string) []string {
	var suites []string
	for _, part := range strings.Split(answer, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part != config.TestSuiteUnit && part != config.TestSuiteFunctional {
			utils.PrintWarning(fmt.Sprintf("Ignoring unknown test suite %q.", part))
			continue
		}
		suites = append(suites, part)
	}
	if len(suites) == 0 {
		suites = []string{config.TestSuiteUnit}
	}
	return suites
}

func orDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

// writeDeployFiles renders the Oro recipe, always, and the deploy.php stub only when it does
// not exist yet: the stub belongs to the project once created.
func writeDeployFiles(projectDir string) error {
	recipePath := filepath.Join(projectDir, config.DeployRecipeRelPath)
	if err := os.MkdirAll(filepath.Dir(recipePath), 0o755); err != nil {
		return fmt.Errorf("could not create %s: %w", filepath.Dir(recipePath), err)
	}

	recipe, err := pipeline.RenderRecipe()
	if err != nil {
		return fmt.Errorf("could not render the Oro recipe: %w", err)
	}
	if err := os.WriteFile(recipePath, recipe, 0o644); err != nil {
		return fmt.Errorf("could not write %s: %w", config.DeployRecipeRelPath, err)
	}
	utils.PrintSuccess("Recipe written to " + config.DeployRecipeRelPath)

	stubPath := filepath.Join(projectDir, config.DeployStubRelPath)
	if _, err := os.Stat(stubPath); err == nil {
		utils.PrintInfo(config.DeployStubRelPath + " already exists and was left untouched.")
		return nil
	}

	stub, err := pipeline.RenderStub()
	if err != nil {
		return fmt.Errorf("could not render deploy.php: %w", err)
	}
	if err := os.WriteFile(stubPath, stub, 0o644); err != nil {
		return fmt.Errorf("could not write %s: %w", config.DeployStubRelPath, err)
	}
	utils.PrintSuccess("Stub written to " + config.DeployStubRelPath)

	return nil
}
