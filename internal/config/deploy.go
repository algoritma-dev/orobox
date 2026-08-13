package config

import (
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strings"
)

// Test suite names accepted in a stage's test_suites list.
const (
	TestSuiteUnit       = "unit"
	TestSuiteFunctional = "functional"
)

// DefaultKeepReleases is the number of releases Deployer keeps on the remote host
// when a stage does not say otherwise.
const DefaultKeepReleases = 5

// DefaultSSHPort is the SSH port used when a stage does not set one.
const DefaultSSHPort = 22

// DeployToolsDir is the bamarni/composer-bin-plugin namespace directory for PHP Deployer.
// Deployer lives in its own isolated composer project for the same reason the QA tools do:
// it gets its own dependency graph without fighting OroCommerce's locked versions.
const DeployToolsDir = OroRootDir + "/vendor-bin/deploy"

// DeployRecipeRelPath is the repo-relative path of the Oro recipe Orobox owns and
// rewrites on every deploy-init. deploy.php requires it and is never touched again,
// so recipe improvements reach existing projects without a manual merge.
const DeployRecipeRelPath = "vendor-bin/deploy/orobox/oro.php"

// DeployStubRelPath is the repo-relative path of the user-owned Deployer entry point.
const DeployStubRelPath = "deploy.php"

// DeployArtifactsDir is the host directory, relative to the project root, where the
// pipeline exports the release tarballs. A GitLab job lists it under artifacts:paths.
const DeployArtifactsDir = "var/orobox/deploy"

// Artifact file names produced by the pipeline.
const (
	VendorArtifactName = "vendor.tar.gz"
	AssetsArtifactName = "assets.tar.gz"
)

// StageConfig describes a single deploy target. One stage means one host: the ref it
// builds is also the ref Deployer checks out remotely, so the uploaded artifacts can
// never belong to a different commit than the deployed code.
type StageConfig struct {
	Name           string   `yaml:"name" mapstructure:"name"`
	Ref            string   `yaml:"ref" mapstructure:"ref"`
	Host           string   `yaml:"host" mapstructure:"host"`
	User           string   `yaml:"user" mapstructure:"user"`
	Port           int      `yaml:"port,omitempty" mapstructure:"port"`
	DeployPath     string   `yaml:"deploy_path" mapstructure:"deploy_path"`
	KeepReleases   int      `yaml:"keep_releases,omitempty" mapstructure:"keep_releases"`
	TestSuites     []string `yaml:"test_suites,omitempty" mapstructure:"test_suites"`
	RestartCommand string   `yaml:"restart_command,omitempty" mapstructure:"restart_command"`
}

// DeployConfig is the `deploy` block of .orobox.yaml. It is the only source of truth for
// deployment: deploy.php reads host, user, port, path, repository and ref from the
// OROBOX_DEPLOY_* environment variables Orobox injects, so the two cannot drift.
type DeployConfig struct {
	PreBuiltAssetsEnabled bool   `yaml:"pre_built_assets_enabled" mapstructure:"pre_built_assets_enabled"`
	Repository            string `yaml:"repository,omitempty" mapstructure:"repository"`
	// SourceDir is the repository-relative directory that holds the OroCommerce application.
	// Empty means the repository root, which is the normal case; a monorepo keeping the
	// application beside other projects sets it to that subdirectory, e.g. "b2b". The pipeline
	// builds from it and Deployer extracts only it into the release, so a release never carries
	// the sibling projects.
	SourceDir string        `yaml:"source_dir,omitempty" mapstructure:"source_dir"`
	Stages    []StageConfig `yaml:"stages" mapstructure:"stages"`
}

// Configured reports whether a usable deploy block exists.
func (d *DeployConfig) Configured() bool {
	return d != nil && len(d.Stages) > 0
}

// Source returns the normalized repository-relative application directory: a clean relative
// path, or "" when the repository root is the application. Both the pipeline and the
// OROBOX_DEPLOY_SUB_DIRECTORY variable read it, so Dagger and Deployer can never disagree
// about which directory a release is built from.
func (d *DeployConfig) Source() string {
	if d == nil {
		return ""
	}
	dir := path.Clean(strings.Trim(strings.TrimSpace(d.SourceDir), "/"))
	if dir == "." || dir == "" {
		return ""
	}
	return dir
}

// SSHPort returns the stage's SSH port, or DefaultSSHPort when unset.
func (s StageConfig) SSHPort() int {
	if s.Port == 0 {
		return DefaultSSHPort
	}
	return s.Port
}

// Releases returns how many releases to keep, or DefaultKeepReleases when unset.
func (s StageConfig) Releases() int {
	if s.KeepReleases == 0 {
		return DefaultKeepReleases
	}
	return s.KeepReleases
}

// Suites returns the PHPUnit suites to run for this stage. Unit tests need no database
// install, so they are the default: a stage must opt into the far slower functional run.
func (s StageConfig) Suites() []string {
	if len(s.TestSuites) == 0 {
		return []string{TestSuiteUnit}
	}
	return s.TestSuites
}

// RunsFunctionalTests reports whether this stage needs a full oro:install in the test
// container before PHPUnit can run.
func (s StageConfig) RunsFunctionalTests() bool {
	for _, suite := range s.Suites() {
		if suite == TestSuiteFunctional {
			return true
		}
	}
	return false
}

// StageFor resolves a stage by name. An empty name is allowed only when a single stage
// is configured, so `orobox deploy` stays short for one-target projects while never
// guessing between staging and production.
func (d *DeployConfig) StageFor(name string) (StageConfig, error) {
	if !d.Configured() {
		return StageConfig{}, errors.New("no deploy stages configured: run 'orobox deploy-init'")
	}

	if name == "" {
		if len(d.Stages) == 1 {
			return d.Stages[0], nil
		}
		return StageConfig{}, fmt.Errorf("stage name is required, one of: %s", strings.Join(d.StageNames(), ", "))
	}

	for _, stage := range d.Stages {
		if stage.Name == name {
			return stage, nil
		}
	}
	return StageConfig{}, fmt.Errorf("unknown deploy stage %q, configured stages: %s", name, strings.Join(d.StageNames(), ", "))
}

// StageNames lists the configured stage names in declaration order.
func (d *DeployConfig) StageNames() []string {
	names := make([]string, 0, len(d.Stages))
	for _, stage := range d.Stages {
		names = append(names, stage.Name)
	}
	return names
}

// ValidateDeploy checks the deploy block against the install type. Deployment targets the
// whole application checkout, which only the project type provides.
func (c *OroConfig) ValidateDeploy() error {
	if c.Deploy == nil {
		return nil
	}

	if !c.Deploy.Configured() {
		// A present block always needs stages, whatever else it says.
		return errors.New("config error: 'deploy' requires at least one entry under 'stages'")
	}

	if c.Type != InstallTypeProject {
		return fmt.Errorf("config error: 'deploy' is only supported for install type %q, got %q", InstallTypeProject, c.Type)
	}

	// An absolute path or one escaping the checkout would make the Dagger build and Deployer's
	// archive extraction disagree, and both would fail far from the cause.
	if raw := strings.TrimSpace(c.Deploy.SourceDir); raw != "" {
		if strings.HasPrefix(raw, "/") {
			return fmt.Errorf("config error: 'deploy.source_dir' must be relative to the repository root, got %q", raw)
		}
		if dir := c.Deploy.Source(); dir == "" || strings.HasPrefix(dir, "..") {
			return fmt.Errorf("config error: 'deploy.source_dir' must be a directory inside the repository, got %q", raw)
		}
	}

	seen := make(map[string]bool, len(c.Deploy.Stages))
	for i, stage := range c.Deploy.Stages {
		for field, value := range map[string]string{
			"name":        stage.Name,
			"ref":         stage.Ref,
			"host":        stage.Host,
			"user":        stage.User,
			"deploy_path": stage.DeployPath,
		} {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("config error: deploy stage at index %d is missing required field %q", i, field)
			}
		}

		if seen[stage.Name] {
			return fmt.Errorf("config error: duplicate deploy stage name %q", stage.Name)
		}
		seen[stage.Name] = true

		for _, suite := range stage.TestSuites {
			if suite != TestSuiteUnit && suite != TestSuiteFunctional {
				return fmt.Errorf("config error: deploy stage %q has unknown test suite %q (expected %q or %q)",
					stage.Name, suite, TestSuiteUnit, TestSuiteFunctional)
			}
		}
	}

	return nil
}

// ResolveRepository returns the configured repository URL, falling back to the checkout's
// origin remote. Deployer clones this on the remote host, so an empty result is fatal and
// reported by the caller rather than silently deploying nothing.
func (d *DeployConfig) ResolveRepository() (string, error) {
	if d.Repository != "" {
		return d.Repository, nil
	}
	return GitOriginURL()
}

// GitOriginURL returns the origin remote of the current checkout.
func GitOriginURL() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", errors.New("could not determine the git 'origin' remote: set 'deploy.repository' in .orobox.yaml")
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", errors.New("git 'origin' remote is empty: set 'deploy.repository' in .orobox.yaml")
	}
	return url, nil
}
