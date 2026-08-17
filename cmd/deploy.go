// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/pipeline"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	deployAssumeYes      bool
	deployNoCache        bool
	deploySkipQA         bool
	deploySkipTest       bool
	deploySkipRelease    bool
	deployRef            string
	deployCacheScope     string
	deployBaseCacheScope string
)

var deployCmd = &cobra.Command{
	Use:   "deploy [stage]",
	Short: "Build, check and release the application to a configured stage",
	Long: `Runs the full deploy pipeline for a stage configured under 'deploy' in .orobox.yaml.

Dagger builds the vendor tree and, unless the repository ships pre-built assets, the
webpack assets, then runs the QA tools in check-only mode and the configured test suites.
Only when everything passes does PHP Deployer clone the stage's ref on the remote host,
upload the artifacts and update the application.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		stageName := ""
		if len(args) == 1 {
			stageName = args[0]
		}
		runDeployCommand(stageName)
	},
}

func init() {
	deployCmd.Flags().BoolVarP(&deployAssumeYes, "yes", "y", false, "Do not ask for confirmation before releasing")
	deployCmd.Flags().BoolVar(&deployNoCache, "no-cache", false, "Rebuild everything: the dependency layers, the QA install and the test database")
	deployCmd.Flags().BoolVar(&deploySkipQA, "skip-qa", false, "Skip the QA checks")
	deployCmd.Flags().BoolVar(&deploySkipTest, "skip-test", false, "Skip the test suites")
	deployCmd.Flags().BoolVar(&deploySkipRelease, "skip-release", false, "Build, check and export the artifacts, but do not release to the remote host")
	deployCmd.Flags().StringVar(&deployRef, "ref", "", "Build this ref instead of the one configured for the stage")
	deployCmd.Flags().StringVar(&deployCacheScope, "cache-scope", "", "Name the cache volume family (default: the ref being built)")
	deployCmd.Flags().StringVar(&deployBaseCacheScope, "base-cache-scope", "", "Seed a missing test database dump from this cache scope")
	rootCmd.AddCommand(deployCmd)
}

func runDeployCommand(stageName string) {
	conf, err := loadDeployConfig()
	if err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}

	stage, err := conf.Deploy.StageFor(stageName)
	if err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}

	projectDir := config.GetHostBundlePath()
	if err := checkDeployerFiles(projectDir); err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}

	repository, err := conf.Deploy.ResolveRepository()
	if err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}

	opts, err := deployOptions(projectDir, stage, !deploySkipRelease)
	if err != nil {
		utils.PrintError(err.Error())
		os.Exit(1)
	}

	plan := pipeline.NewWithOverrides(conf, stage, repository, pipeline.Overrides{
		Ref:            deployRef,
		CacheScope:     deployCacheScope,
		BaseCacheScope: deployBaseCacheScope,
	})
	plan.NoCache = deployNoCache
	plan.SkipQA = deploySkipQA
	plan.SkipTest = deploySkipTest
	plan.SkipRelease = deploySkipRelease
	printDeploySummary(plan)

	// The prompt exists because a release is irreversible. With --skip-release nothing reaches the
	// stage host, so asking would only train the reader to confirm without looking.
	if plan.RunsRelease() && !deployAssumeYes && isTTY() {
		reader := bufio.NewReader(stdin)
		if !utils.AskYesNo(reader, fmt.Sprintf("Release %s to %s?", plan.Ref, stage.Host), false) {
			utils.PrintInfo("Aborted.")
			return
		}
	}

	utils.PrintInfo("Running the deploy pipeline. The first run has no caches and takes a while.")

	artifacts, runErr := pipeline.Run(context.Background(), plan, opts)
	for _, artifact := range artifacts {
		utils.PrintInfo("Artifact: " + artifact)
	}
	if runErr != nil {
		utils.PrintError(runErr.Error())
		os.Exit(1)
	}

	if !plan.RunsRelease() {
		utils.PrintSuccess(fmt.Sprintf("Built %s for %s and exported the artifacts; the release was skipped.", plan.Ref, stage.Name))
		return
	}

	utils.PrintSuccess(fmt.Sprintf("Deployed %s to %s (%s).", plan.Ref, stage.Name, stage.Host))
}

// loadDeployConfig reads the configuration and rejects install types that cannot be deployed.
func loadDeployConfig() (*config.OroConfig, error) {
	var conf config.OroConfig
	if err := viper.Unmarshal(&conf); err != nil {
		return nil, fmt.Errorf("error reading config: %w", err)
	}

	if conf.Type != config.InstallTypeProject {
		return nil, fmt.Errorf("deploy is only available for install type %q; this project is %q, whose checkout is not a deployable application",
			config.InstallTypeProject, conf.Type)
	}
	if !conf.Deploy.Configured() {
		return nil, fmt.Errorf("no deploy configuration found in .orobox.yaml: run 'orobox deploy-init'")
	}

	return &conf, nil
}

// checkDeployerFiles verifies the two files the release step needs from the repository.
// The vendor tree itself is installed inside the pipeline, so it is deliberately not checked:
// a CI clone never has it.
func checkDeployerFiles(projectDir string) error {
	for _, rel := range []string{config.DeployStubRelPath, "vendor-bin/deploy/composer.json"} {
		if _, err := os.Stat(filepath.Join(projectDir, rel)); err != nil {
			return fmt.Errorf("%s is missing: run 'orobox deploy-init' and commit the generated files", rel)
		}
	}
	return nil
}

// deployOptions collects the host-side credentials the pipeline needs. Locally that is the
// SSH agent; in CI it is a key and a git token from the job environment.
//
// requireSSH is false when the run stops before the release. The SSH credentials exist for the
// remote session, so demanding them from a job that never opens one would make --skip-release
// unusable exactly where it is most useful: a CI job with no deploy key.
func deployOptions(projectDir string, stage config.StageConfig, requireSSH bool) (pipeline.Options, error) {
	opts := pipeline.Options{
		ProjectDir:    projectDir,
		Debug:         viper.GetBool("debug"),
		SSHAuthSock:   os.Getenv("SSH_AUTH_SOCK"),
		SSHPrivateKey: os.Getenv("OROBOX_DEPLOY_SSH_KEY"),
		GitHTTPToken:  os.Getenv("OROBOX_DEPLOY_GIT_TOKEN"),
		GitHTTPUser:   os.Getenv("OROBOX_DEPLOY_GIT_USER"),
	}

	// GitLab hands every job a token for cloning over https; use it when nothing else is set.
	if opts.GitHTTPToken == "" {
		if token := os.Getenv("CI_JOB_TOKEN"); token != "" {
			opts.GitHTTPToken = token
			if opts.GitHTTPUser == "" {
				opts.GitHTTPUser = "gitlab-ci-token"
			}
		}
	}

	if requireSSH && opts.SSHAuthSock == "" && opts.SSHPrivateKey == "" {
		return opts, fmt.Errorf("no SSH credentials found: start an agent (eval $(ssh-agent) && ssh-add) or set OROBOX_DEPLOY_SSH_KEY")
	}

	if home, err := os.UserHomeDir(); err == nil {
		knownHosts := filepath.Join(home, ".ssh", "known_hosts")
		if _, err := os.Stat(knownHosts); err == nil {
			opts.KnownHostsPath = knownHosts
		}
	}
	if opts.KnownHostsPath == "" {
		// known_hosts serves two connections: the git clone inside the pipeline container and
		// the release's SSH session. Without it the clone can fail outright.
		utils.PrintWarning(fmt.Sprintf("No ~/.ssh/known_hosts found: the host key of %s will be accepted without verification, and cloning over SSH may fail host key verification.", stage.Host))
	}

	return opts, nil
}

func printDeploySummary(plan *pipeline.Plan) {
	stage := plan.Stage

	utils.PrintTitle("Deploy plan")
	fmt.Printf("  Stage:      %s\n", stage.Name)
	fmt.Printf("  Ref:        %s\n", plan.Ref)
	fmt.Printf("  Repository: %s\n", plan.Repository)
	fmt.Printf("  Cache:      %s\n", plan.CacheScope)
	if plan.BaseCacheScope != "" {
		fmt.Printf("  Cache base: %s\n", plan.BaseCacheScope)
	}
	if plan.SourceDir != "" {
		fmt.Printf("  Source dir: %s\n", plan.SourceDir)
	}
	fmt.Printf("  Target:     %s@%s:%d%s\n", stage.User, stage.Host, stage.SSHPort(), stage.DeployPath)
	fmt.Printf("  Image:      %s\n", plan.Image)
	fmt.Printf("  Suites:     %v\n", stage.Suites())
	if plan.BuildsAssets() {
		fmt.Println("  Assets:     built in the pipeline and uploaded")
	} else {
		fmt.Println("  Assets:     taken from the repository (pre_built_assets_enabled: true)")
	}
	fmt.Printf("  Artifacts:  %v\n", plan.Artifacts())
	if skipped := plan.SkippedSteps(); len(skipped) > 0 {
		fmt.Printf("  Skipping:   %v\n", skipped)
	}
}
