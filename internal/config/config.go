// Package config provides configuration management for Orobox.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/viper"
	yamlv3 "gopkg.in/yaml.v3"
)

// DomainConfig represents the configuration for a domain.
type DomainConfig struct {
	Host string `yaml:"host" mapstructure:"host"`
	Root string `yaml:"root" mapstructure:"root"`
	Ssl  bool   `yaml:"ssl" mapstructure:"ssl"`
}

// ServicesConfig represents the configuration for various services.
type ServicesConfig struct {
	Redis         bool `yaml:"redis" mapstructure:"redis"`
	Mailpit       bool `yaml:"mailpit" mapstructure:"mailpit"`
	RabbitMQ      bool `yaml:"rabbitmq" mapstructure:"rabbitmq"`
	Elasticsearch bool `yaml:"elasticsearch" mapstructure:"elasticsearch"`
	RedisInsight  bool `yaml:"redisinsight" mapstructure:"redisinsight"`
	Kibana        bool `yaml:"kibana" mapstructure:"kibana"`
	Adminer       bool `yaml:"adminer" mapstructure:"adminer"`
}

// QaConfig represents the configuration for enabled QA tools.
//
// Every field is a pointer, and that is not cosmetic. IsQaToolEnabled reads an unset key as
// enabled, so "unset" and "false" are different states — and a plain bool cannot tell them
// apart. `orobox deploy-init` rewrites the whole config file through SaveConfig, so a value
// field would turn every tool the file never mentioned into an explicit `false` and disable
// the entire QA tool set for good.
type QaConfig struct {
	Phpstan     *bool `yaml:"phpstan,omitempty" mapstructure:"phpstan"`
	Rector      *bool `yaml:"rector,omitempty" mapstructure:"rector"`
	PhpCSFixer  *bool `yaml:"php_cs_fixer,omitempty" mapstructure:"php_cs_fixer"`
	TwigCSFixer *bool `yaml:"twig_cs_fixer,omitempty" mapstructure:"twig_cs_fixer"`
	Eslint      *bool `yaml:"eslint,omitempty" mapstructure:"eslint"`
	Stylelint   *bool `yaml:"stylelint,omitempty" mapstructure:"stylelint"`
}

// TestConfig represents the configuration for the test environment.
type TestConfig struct {
	UseTmpfs  bool   `yaml:"use_tmpfs" mapstructure:"use_tmpfs"`
	TmpfsSize string `yaml:"tmpfs_size" mapstructure:"tmpfs_size"`
	// Qa is a pointer for the same reason its fields are: a struct value would always be
	// serialized, turning a config that never mentioned QA into one that disables it.
	Qa *QaConfig `yaml:"qa,omitempty" mapstructure:"qa"`
}

// CommandConfig represents a custom command that can be run in the container.
type CommandConfig struct {
	Name        string   `yaml:"name" mapstructure:"name"`
	Command     string   `yaml:"command" mapstructure:"command"`
	Description string   `yaml:"description" mapstructure:"description"`
	Service     string   `yaml:"service" mapstructure:"service"`
	Depends     []string `yaml:"depends" mapstructure:"depends"`
}

// ComposerConfig holds Composer-specific configuration for the bundle.
type ComposerConfig struct {
	Repositories []map[string]interface{} `yaml:"repositories" mapstructure:"repositories"`
	// Auth mirrors Composer's COMPOSER_AUTH schema (github-oauth, gitlab-token,
	// http-basic, bearer, ...). It is serialized to JSON and injected as the
	// COMPOSER_AUTH env var only into the containers that run composer, so tokens
	// for private repositories never get committed or baked into long-running services.
	Auth map[string]interface{} `yaml:"auth" mapstructure:"auth"`
	// SSHAgent overrides the auto-detection of SSH agent forwarding. Nil means auto-detect
	// (any SSH-transport repository URL, here or in the project's own composer.json); a
	// non-nil value forces forwarding on or off. It is a pointer because "unset" and
	// "explicitly false" must mean different things, and so an unset value is never written
	// into a generated config file.
	SSHAgent *bool `yaml:"ssh_agent,omitempty" mapstructure:"ssh_agent"`
}

// ImageConfig describes what a project adds on top of the published Orobox image. Orobox runs a
// published image rather than building one per project; anything set here inserts one locally
// built layer between that image and the stack, so a project can install what it depends on
// without owning a Dockerfile. An empty block keeps the stack on the published image with no
// local build.
type ImageConfig struct {
	// Dockerfile is a project-owned Dockerfile, relative to the directory holding
	// .orobox.yaml, that extends the published image. Its final stage must be
	// `FROM ${OROBOX_BASE_IMAGE}` — see DockerfileBaseImageArg — so `oro_version` stays the only
	// thing that decides which Oro image is underneath.
	Dockerfile string `yaml:"dockerfile,omitempty" mapstructure:"dockerfile"`
	// Apk lists Alpine packages to install; a `name=version` entry pins one.
	Apk []string `yaml:"apk,omitempty" mapstructure:"apk"`
	// PhpExtensions lists PHP extensions to install.
	PhpExtensions []string `yaml:"php_extensions,omitempty" mapstructure:"php_extensions"`
	// Npm lists global npm packages to install.
	Npm []string `yaml:"npm,omitempty" mapstructure:"npm"`
	// Run lists shell commands executed in order while building the layer. Unlike the lists
	// above they are free-form: a command is the whole point of the key, so only emptiness is
	// checked.
	Run []string `yaml:"run,omitempty" mapstructure:"run"`
}

// IsEmpty reports whether the block asks for no local layer at all.
func (i ImageConfig) IsEmpty() bool {
	return strings.TrimSpace(i.Dockerfile) == "" &&
		len(i.Apk) == 0 && len(i.PhpExtensions) == 0 && len(i.Npm) == 0 && len(i.Run) == 0
}

// OroVersions defines the versions of components for a specific OroCommerce version.
type OroVersions struct {
	PHP           string
	Postgres      string
	Redis         string
	Node          string
	NPM           string
	PNPM          string
	RabbitMQ      string
	Elasticsearch string
	// Symfony is the Symfony minor line this Oro version ships. QA tools install into
	// an isolated vendor tree, but PHPStan co-loads Oro's classes with the tools'
	// Symfony copies in one process, so those copies must match this line or PHP
	// fatals on incompatible method signatures. See GetQaSymfonyConstraints.
	Symfony string
	// Stylelint is the npm constraint for stylelint itself, and StylelintConfig the exact
	// version of @oroinc/oro-stylelint-config this Oro version's own package.json declares.
	// They travel together: the shareable config carries its own stylelint dependency, and a
	// mismatch is not a warning but a crash. See GetQaStylelint.
	Stylelint       string
	StylelintConfig string
}

// SupportedOroVersions is the list of supported OroCommerce versions.
var SupportedOroVersions = []string{"7.0", "6.1", "6.0", "5.1"}

// GetVersionsForOro returns the component versions for a given OroCommerce version.
func GetVersionsForOro(oroVersion string) OroVersions {
	switch oroVersion {
	case "7.0":
		return OroVersions{
			PHP:             "8.5",
			Postgres:        "17.6-alpine",
			Redis:           "7.4-alpine",
			Node:            "24",
			PNPM:            "10",
			RabbitMQ:        "4.2-management-alpine",
			Elasticsearch:   "9.2.0",
			Symfony:         "6.4",
			Stylelint:       "^16.26.1",
			StylelintConfig: "7.0.1",
		}
	case "6.1":
		return OroVersions{
			PHP:             "8.4",
			Postgres:        "16.1-alpine",
			Redis:           "7.2-alpine",
			Node:            "22",
			NPM:             "10",
			RabbitMQ:        "3.12-management-alpine",
			Elasticsearch:   "8.4.1",
			Symfony:         "6.4",
			Stylelint:       "^16.17.0",
			StylelintConfig: "6.1.0-lts001",
		}
	case "6.0":
		return OroVersions{
			PHP:             "8.3",
			Postgres:        "16.1-alpine",
			Redis:           "7.0-alpine",
			Node:            "20.19",
			NPM:             "10",
			RabbitMQ:        "3.12-management-alpine",
			Elasticsearch:   "8.4.1",
			Symfony:         "6.4",
			Stylelint:       "^15.11.0",
			StylelintConfig: "6.0.0-lts1",
		}
	case "5.1":
		return OroVersions{
			PHP:             "8.2",
			Postgres:        "16.1-alpine",
			Redis:           "6.2-alpine",
			Node:            "18.14",
			NPM:             "9.3",
			RabbitMQ:        "3.11-management-alpine",
			Elasticsearch:   "8.4.1",
			Symfony:         "5.4",
			Stylelint:       "^15.11.0",
			StylelintConfig: "5.1.0-lts002",
		}
	default:
		// Fallback for other versions or default
		if oroVersion >= "7.0" {
			return GetVersionsForOro("7.0")
		}
		if oroVersion >= "6.1" {
			return GetVersionsForOro("6.1")
		}
		if oroVersion >= "6.0" {
			return GetVersionsForOro("6.0")
		}
		return GetVersionsForOro("5.1")
	}
}

// OroConfig is the main configuration structure for Orobox.
type OroConfig struct {
	Type       string          `yaml:"type" mapstructure:"type" default:"bundle"`
	Class      string          `yaml:"class" mapstructure:"class"`
	Namespace  string          `yaml:"namespace" mapstructure:"namespace"`
	OroVersion string          `yaml:"oro_version" mapstructure:"oro_version"`
	Domains    []DomainConfig  `yaml:"domains" mapstructure:"domains"`
	Services   ServicesConfig  `yaml:"services" mapstructure:"services"`
	Test       TestConfig      `yaml:"test" mapstructure:"test"`
	Commands   []CommandConfig `yaml:"commands" mapstructure:"commands"`
	Composer   ComposerConfig  `yaml:"composer" mapstructure:"composer"`
	// Dockerfile is the original, top-level spelling of image.dockerfile.
	//
	// Deprecated: use Image.Dockerfile. The key is still read so existing configs keep working
	// (ImageSettings and GetImageConfig fold it in), and SaveConfig moves it under `image:` on
	// the next rewrite.
	Dockerfile string `yaml:"dockerfile,omitempty" mapstructure:"dockerfile"`
	// Image is a pointer so a project without customization keeps a clean config file: a struct
	// value would always be serialized, empty lists and all.
	Image *ImageConfig `yaml:"image,omitempty" mapstructure:"image"`
	// PhpIni is either a flat map of php.ini directives or the project-relative path of an ini
	// file, so it is an any. It is not part of Image because it is not baked into the image: it
	// is bind-mounted, and a change needs a container recreate rather than a build. Read it
	// through PhpIniSettings or GetPhpIni, never viper.Get — see GetPhpIni for why.
	PhpIni any `yaml:"php_ini,omitempty" mapstructure:"php_ini"`
	// Ports overrides the host port the stack publishes for a service; 0 means "do not publish".
	// Keys are listed in DefaultPorts, and Validate rejects any other. Read it through GetPorts,
	// which fills in the defaults.
	Ports map[string]int `yaml:"ports,omitempty" mapstructure:"ports"`
	// Deploy is a pointer so a project without deployment keeps a clean config file: a struct
	// value would always be serialized, empty stages and all.
	Deploy *DeployConfig `yaml:"deploy,omitempty" mapstructure:"deploy"`
}

// Install types for OroCommerce.
const (
	InstallTypeBundle  = "bundle"
	InstallTypeProject = "project"
	InstallTypeDemo    = "demo"
)

// OroRootDir is the base directory for OroCommerce in the container.
const OroRootDir = "/var/www/oro"

// QaToolsDir is the bamarni/composer-bin-plugin namespace directory for QA tools.
// bamarni uses "vendor-bin/<namespace>" as its default target directory.
// Tools are installed here as an isolated composer project, so they get the latest
// versions without conflicting with OroCommerce's locked dependencies.
// PHPStan can still access the OroCommerce autoloader at OroRootDir/vendor/autoload.php.
const QaToolsDir = OroRootDir + "/vendor-bin/qa"

// CustomBundlePath is the base path for custom bundles.
const CustomBundlePath = "/src/CustomBundle"

// Validate checks if the configuration is valid.
func (c *OroConfig) Validate() error {
	if c.Type == "" {
		c.Type = InstallTypeBundle
	}

	installType, err := InstallTypeFor(c.Type)
	if err != nil {
		return err
	}

	if installType.RequiresBundleNamespace() && c.Namespace == "" {
		return errors.New("config error: field 'namespace' is required (did you use 'bundle_namespace' by mistake?)")
	}

	if c.OroVersion == "" {
		return errors.New("config error: field 'oro_version' is required")
	}
	if len(c.Domains) == 0 {
		return errors.New("config error: at least one domain must be configured")
	}
	for i, domain := range c.Domains {
		if domain.Host == "" {
			return errors.New("config error: 'host' is required for domain at index " + string(rune(i)))
		}
	}
	if err := c.validateImage(); err != nil {
		return err
	}
	if err := validatePhpIni(c.PhpIni); err != nil {
		return err
	}
	if err := validatePorts(c.Ports); err != nil {
		return err
	}
	return c.ValidateDeploy()
}

// imageEntryPattern is the alphabet allowed in apk, php_extensions and npm entries: enough for
// scoped npm packages (@scope/name), version pins (name=1.2-r0, name@1.2) and tags, with no
// whitespace or shell metacharacters. These entries are spliced into a generated Dockerfile
// line, so an entry that is not a plain token could smuggle in a second command.
var imageEntryPattern = regexp.MustCompile(`^[A-Za-z0-9@._+:/=~-]+$`)

// validateImage checks the image block and the deprecated top-level dockerfile key. Every
// message names the offending key, because a config with several lists would otherwise leave the
// user hunting for which entry was rejected.
func (c *OroConfig) validateImage() error {
	if strings.TrimSpace(c.Dockerfile) != "" && c.Image != nil && strings.TrimSpace(c.Image.Dockerfile) != "" {
		return errors.New("config error: 'dockerfile' and 'image.dockerfile' are both set; remove the deprecated 'dockerfile' key and keep 'image.dockerfile'")
	}
	if err := validateProjectPathKey("dockerfile", c.Dockerfile); err != nil {
		return err
	}
	if c.Image == nil {
		return nil
	}
	if err := validateProjectPathKey("image.dockerfile", c.Image.Dockerfile); err != nil {
		return err
	}
	lists := []struct {
		key     string
		entries []string
	}{
		{"image.apk", c.Image.Apk},
		{"image.php_extensions", c.Image.PhpExtensions},
		{"image.npm", c.Image.Npm},
	}
	for _, list := range lists {
		for i, entry := range list.entries {
			if !imageEntryPattern.MatchString(entry) {
				return fmt.Errorf("config error: '%s' entry %d (%s) must be non-empty and contain only letters, digits and @ . _ + : / = ~ -", list.key, i, strconv.Quote(entry))
			}
		}
	}
	for i, entry := range c.Image.Run {
		if strings.TrimSpace(entry) == "" {
			return fmt.Errorf("config error: 'image.run' entry %d must not be empty", i)
		}
		// Each entry is spliced after "RUN " on one Dockerfile line. A line break would let the
		// following text become an instruction of its own (a `FROM` would even start a new final
		// stage, bypassing the base-image check), and a trailing backslash would continue the
		// line into whatever instruction comes next.
		if strings.ContainsAny(entry, "\r\n") || strings.HasSuffix(strings.TrimRight(entry, " \t"), `\`) {
			return fmt.Errorf("config error: 'image.run' entry %d (%s) must be a single line without a trailing backslash; join commands with && or move the script into 'image.dockerfile'", i, strconv.Quote(entry))
		}
	}
	return nil
}

// validateProjectPathKey applies the path rules to a config key that names a file of the
// project: either spelling of the Dockerfile key, or the file form of php_ini. The path is
// joined onto the project directory (and for a Dockerfile its parent becomes a Docker build
// context), so one that escapes the project has to be refused while the config is being read.
func validateProjectPathKey(key, value string) error {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil
	}
	if filepath.IsAbs(raw) {
		return fmt.Errorf("config error: '%s' must be relative to the directory holding .orobox.yaml, got %s", key, strconv.Quote(raw))
	}
	if rel := normalizeDockerfilePath(raw); rel == "" || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("config error: '%s' must point inside the project, got %s", key, strconv.Quote(raw))
	}
	return nil
}

// ImageSettings returns the image block with the deprecated top-level dockerfile folded in, so
// callers read one place whichever spelling the project used. The explicit image.dockerfile wins;
// Validate rejects a config that sets both, so the precedence only matters for unvalidated input.
func (c *OroConfig) ImageSettings() ImageConfig {
	var img ImageConfig
	if c.Image != nil {
		img = *c.Image
	}
	if strings.TrimSpace(img.Dockerfile) == "" {
		img.Dockerfile = c.Dockerfile
	}
	// Normalized like GetImageConfig does for the dev stack, so the deploy pipeline and the local
	// build resolve the same file whatever spelling the YAML used.
	img.Dockerfile = normalizeDockerfilePath(img.Dockerfile)
	return img
}

// GetImageConfig is the viper-backed equivalent of ImageSettings, for commands that read the
// loaded .orobox.yaml rather than holding an OroConfig. The Dockerfile path is returned
// normalized, as GetDockerfile always did.
func GetImageConfig() ImageConfig {
	var img ImageConfig
	_ = viper.UnmarshalKey("image", &img)
	if strings.TrimSpace(img.Dockerfile) == "" {
		img.Dockerfile = viper.GetString("dockerfile")
	}
	img.Dockerfile = normalizeDockerfilePath(img.Dockerfile)
	return img
}

// DeprecatedDockerfileKeyUsed reports whether the project still uses the top-level `dockerfile`
// key, so the root command can tell the user to move it under `image:`.
func DeprecatedDockerfileKeyUsed() bool {
	return strings.TrimSpace(viper.GetString("dockerfile")) != ""
}

// HasCustomLayer reports whether the project asks for a locally built image layer, by any of
// the image keys or the deprecated top-level dockerfile.
func HasCustomLayer() bool {
	return !GetImageConfig().IsEmpty()
}

// DockerfileBaseImageArg is the build argument Orobox sets to the published image a custom
// Dockerfile has to extend. It is a build argument rather than a hardcoded tag so `oro_version`
// remains the single place that decides which Oro image the project runs on: bumping it in
// .orobox.yaml moves the custom layer with it, and a project Dockerfile never has to be edited
// for an Oro upgrade.
const DockerfileBaseImageArg = "OROBOX_BASE_IMAGE"

// normalizeDockerfilePath cleans a configured path into a project-relative one. Separate from
// GetDockerfile so Validate can reject an escaping path without touching viper.
func normalizeDockerfilePath(raw string) string {
	cleaned := path.Clean(strings.TrimSpace(filepath.ToSlash(raw)))
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return strings.TrimPrefix(cleaned, "./")
}

// GetDockerfile returns the project-relative path of the custom Dockerfile, or "" when the
// project runs the published image unchanged.
func GetDockerfile() string {
	return GetImageConfig().Dockerfile
}

// GetDockerfilePath returns the absolute path of the custom Dockerfile, or "" when none is
// configured.
func GetDockerfilePath() string {
	rel := GetDockerfile()
	if rel == "" {
		return ""
	}
	return filepath.Join(GetHostBundlePath(), filepath.FromSlash(rel))
}

// ParseConfig parses a configuration from bytes.
func ParseConfig(data []byte) (*OroConfig, error) {
	var c OroConfig
	decoder := yamlv3.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// LoadConfigFile parses the .orobox.yaml viper loaded, straight from disk.
//
// Commands that rewrite the config file must start from this and not from viper.Unmarshal:
// viper splits keys on "." and lowercases them, so a round trip through it would turn the
// php_ini directive `xdebug.log_level` into a nested map and `Memory_Limit` into `memory_limit`,
// and the file written back would no longer load. Read-only callers can keep using viper.
func LoadConfigFile() (*OroConfig, error) {
	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		return nil, errors.New("no .orobox.yaml is in use; run this command from a project directory")
	}
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", configFile, err)
	}
	conf, err := ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("invalid config file %s:\n%w", configFile, err)
	}
	return conf, nil
}

// SaveConfig saves the configuration to the specified path.
//
// The deprecated top-level dockerfile is written under `image:` instead, so a rewrite (such as
// `orobox deploy-init`) migrates the config rather than carrying the old key forward. The
// migration happens on a copy: the caller's config is left as it was.
func SaveConfig(path string, c *OroConfig) error {
	out := *c
	if strings.TrimSpace(out.Dockerfile) != "" {
		var img ImageConfig
		if out.Image != nil {
			img = *out.Image
		}
		if strings.TrimSpace(img.Dockerfile) == "" {
			img.Dockerfile = out.Dockerfile
		}
		out.Image = &img
		out.Dockerfile = ""
	}
	// A non-nil but empty block would serialize as `image: {}`; omit it like an absent one.
	if out.Image != nil && out.Image.IsEmpty() {
		out.Image = nil
	}
	data, err := yamlv3.Marshal(&out)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// GetNamespace returns the project namespace.
func GetNamespace() string {
	ns := viper.GetString("namespace")
	if ns == "" {
		return "CustomBundle"
	}
	return ns
}

// GetBundlePath returns the relative path to the bundle.
func GetBundlePath() string {
	ns := GetNamespace()
	return strings.ReplaceAll(ns, "\\", "/")
}

// GetBundleRootContainerPath returns the path to the bundle root in the container.
// This is bundle-specific; use GetSourceRootContainerPath when you mean "the user's
// source root" regardless of install type.
func GetBundleRootContainerPath() string {
	return OroRootDir + "/bundles/" + GetBundlePath()
}

// GetSourceRootContainerPath returns the container path of the user's source root for
// the active install type: the bundle subdir for bundle, OroRoot for project.
func GetSourceRootContainerPath() string {
	installType, err := InstallTypeFor(viper.GetString("type"))
	if err != nil {
		// Fall back to bundle semantics on an unknown/unset type; Validate surfaces the error.
		return GetBundleRootContainerPath()
	}
	return installType.SourceRootContainer()
}

// GetQaAnalyzePath returns the source tree PHPStan should analyze for the active
// install type. Passing this explicitly on the PHPStan CLI overrides the config's
// own `paths`, which the algoritma plugin resolves against the isolated QA dir.
func GetQaAnalyzePath() string {
	installType, err := InstallTypeFor(viper.GetString("type"))
	if err != nil {
		return GetBundleRootContainerPath()
	}
	return installType.QaAnalyzePath()
}

// QaAnalyzePathFor returns the tree PHPStan analyzes for an explicit install type, for
// callers that know the type without going through viper — the deploy pipeline, which is
// always a project.
func QaAnalyzePathFor(typeName string) string {
	installType, err := InstallTypeFor(typeName)
	if err != nil {
		return GetBundleRootContainerPath()
	}
	return installType.QaAnalyzePath()
}

// qaSharedPackages are the packages the QA tool set and OroCommerce both need. They are the
// reason the QA namespace needs any dependency bookkeeping at all: the tools live in the
// isolated vendor-bin/qa tree, the application lives in vendor/, and a class present in both
// trees can end up compiled twice in one PHP process. See qatools.SharedVendorScript.
//
// The order is stable so the generated manifest patch and the pinned requirements do not
// reshuffle between runs.
var qaSharedPackages = []string{
	"symfony/console", "symfony/event-dispatcher", "symfony/string", "symfony/finder",
	"symfony/filesystem", "symfony/process", "symfony/options-resolver", "symfony/stopwatch",
	"symfony/service-contracts", "symfony/event-dispatcher-contracts",
	"psr/container", "psr/log",
	// twig/twig is shared for the same reason as the Symfony components, but it fails in a way
	// of its own: Twig's DebugExtension require_once's Resources/debug.php, which declares the
	// global function twig_var_dump(). The QA tree's copy sits under another path, so
	// include_once cannot dedupe it, and PHPStan — the one tool that boots the kernel with the
	// application's autoloader first — dies with "Cannot redeclare twig_var_dump()" as soon as
	// both copies are loaded. Twig dropped those global functions in 3.9, so only the Oro lines
	// still on an older Twig (5.1) reach the fatal; sharing the package removes the second copy
	// on every line instead of on the ones that happen to fail today.
	"twig/twig",
}

// QaSharedPackages returns the names of the packages both trees need, for the manifest patch
// that hands them over to the application's tree. A copy is returned so a caller cannot
// reorder the list the constraints are built from.
func QaSharedPackages() []string {
	out := make([]string, len(qaSharedPackages))
	copy(out, qaSharedPackages)
	return out
}

// GetQaSymfonyConstraints returns Composer constraints that pin the QA tools'
// Symfony components (and the ABI-critical PSR packages) to the same line Oro
// ships. Without them, bamarni resolves the tools against the latest Symfony,
// and PHPStan then fatals when it co-loads Oro's older Symfony classes with the
// tools' newer copies (mismatched method signatures on ServiceLocator,
// Command::execute, TraceableEventDispatcher, ...).
//
// They are the fallback, not the fix: a package the application's own tree ships is removed
// from the QA requirements entirely by the manifest patch (see qatools.SharedVendorScript),
// because two identical copies still fatal — the dumped Symfony container inline-requires
// vendor files by path, and include_once cannot dedupe a second copy under another path.
// The constraints still matter for the packages the application does not ship, and for the
// resolution that happens before the patch has anything installed to read.
func GetQaSymfonyConstraints(oroVersion string) []string {
	sf := GetVersionsForOro(oroVersion).Symfony
	if sf == "" {
		sf = "6.4"
	}

	// Symfony 6.4 pairs with service-contracts v3 / psr-container v2; 5.4 with v2 / v1. psr/log
	// is only capped on 5.4, where symfony/console conflicts with psr/log >=3, which
	// algoritma/php-coding-standards otherwise drags in via composer/composer.
	contracts, container, log := "^3.0", "^2.0", ""
	if sf == "5.4" {
		contracts, container, log = "^2.5", "^1.1", "^2"
	}

	constraints := make([]string, 0, len(qaSharedPackages))
	for _, name := range qaSharedPackages {
		switch name {
		case "symfony/service-contracts", "symfony/event-dispatcher-contracts":
			constraints = append(constraints, name+":"+contracts)
		case "psr/container":
			constraints = append(constraints, name+":"+container)
		case "psr/log":
			if log != "" {
				constraints = append(constraints, name+":"+log)
			}
		case "twig/twig":
			// Twig versions independently of Symfony, so the Symfony line says nothing about it.
			// The constraint is only the fallback for a tree that does not ship Twig at all; an
			// Oro application always does, and the manifest patch then replaces it with the exact
			// line the application installed.
			constraints = append(constraints, name+":^3.0")
		default:
			constraints = append(constraints, name+":^"+sf)
		}
	}
	return constraints
}

// QaStylelint is the stylelint half of the JS tool set, resolved for one Oro line.
//
// The three packages are one decision, not three: @oroinc/oro-stylelint-config carries its own
// stylelint dependency (^15.3 up to Oro 6.0, ^16 from 6.1).
//
// No formatter is named here any more. Which stylelint actually runs is OroCommerce's choice, not
// this table's — the linters are the application's own installation — and a formatter picked from
// a version table is what produced "formatters[STYLELINT_FORMATTER] is not a function" on an Oro
// line whose LTS patch had moved on. The formatter is Orobox's own file instead; see
// qatools/stylelintformatter.go.
type QaStylelint struct {
	// Stylelint is the npm constraint for stylelint itself.
	Stylelint string
	// Config is the exact @oroinc/oro-stylelint-config version this Oro version declares. It is
	// pinned rather than floated for the same reason eslint-config-google is: the version is
	// OroCommerce's choice, and `latest` resolves to whichever line Oro released most recently —
	// which on a pnpm install is overridden anyway by the application's own workspace resolution.
	Config string
}

// GetQaStylelint resolves the stylelint packages for an Oro version.
//
// Both formatters read the report path from the same STYLELINT_CODE_QUALITY_REPORT variable and
// leave the human-readable output on stdout, so which one is installed changes nothing above this
// function.
func GetQaStylelint(oroVersion string) QaStylelint {
	versions := GetVersionsForOro(oroVersion)

	return QaStylelint{
		Stylelint: versions.Stylelint,
		Config:    versions.StylelintConfig,
	}
}

// GetHostBundlePath returns the absolute path to the bundle on the host.
func GetHostBundlePath() string {
	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		// Fallback to current working directory
		dir, _ := os.Getwd()
		return dir
	}
	return filepath.Dir(configFile)
}

// GetProjectName returns the name of the current project.
func GetProjectName() string {
	// The new config doesn't have "name", so we use the directory name
	currDir, _ := os.Getwd()
	return filepath.Base(currDir)
}

// GetInternalDir returns the internal directory for storing Orobox data.
func GetInternalDir() string {
	if os.Getenv("CI") != "" || os.Getenv("OROBOX_LOCAL_CONFIG") != "" {
		return ".orobox"
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return ".orobox"
	}

	projectName := GetProjectName()
	return filepath.Join(configDir, "orobox", projectName)
}

// GetFirstDomainHost returns the host of the first configured domain.
func GetFirstDomainHost() string {
	domains := GetDomains()
	if len(domains) > 0 {
		return domains[0].Host
	}
	return "oro.demo"
}

// GetDomains returns the list of configured domains.
func GetDomains() []DomainConfig {
	var domains []DomainConfig
	_ = viper.UnmarshalKey("domains", &domains)
	return domains
}

// qaToolConfigKeys maps tool names (as used in cmd/qa.go) to their YAML config keys under test.qa.
var qaToolConfigKeys = map[string]string{
	"phpstan":       "phpstan",
	"rector":        "rector",
	"php-cs-fixer":  "php_cs_fixer",
	"twig-cs-fixer": "twig_cs_fixer",
	"eslint":        "eslint",
	"stylelint":     "stylelint",
	"stylelint-css": "stylelint",
}

// IsQaToolEnabled returns whether a QA tool is enabled according to config.
// If the tool is not explicitly configured, it defaults to true (enabled).
func IsQaToolEnabled(toolName string) bool {
	configKey, ok := qaToolConfigKeys[toolName]
	if !ok {
		return true
	}
	key := "test.qa." + configKey
	if !viper.IsSet(key) {
		return true
	}
	return viper.GetBool(key)
}

// FindPhpClass tries to find a PHP class in the specified root directory.
// It returns shortClassName, namespace, foundPath (relative to root), and a boolean indicating if it was found.
func FindPhpClass(root string, className string) (string, string, string, bool) {
	// If the user provides a full namespace like Algoritma\Bundle\ShippyProBundle\AlgoritmaShippyProBundle
	parts := strings.Split(className, "\\")
	shortClassName := parts[len(parts)-1]
	namespace := strings.Join(parts[:len(parts)-1], "\\")

	foundPath := ""
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == shortClassName+".php" {
			relPath, err := filepath.Rel(root, path)
			if err == nil {
				foundPath = relPath
				return filepath.SkipDir // optimization
			}
		}
		return nil
	})

	if err == nil && foundPath != "" {
		return shortClassName, namespace, foundPath, true
	}

	return "", "", "", false
}
