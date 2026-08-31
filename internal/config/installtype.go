package config

import "fmt"

// InstallType encapsulates everything that differs between Orobox install types.
// Adding a new type is a single new struct implementing this interface plus a case
// in InstallTypeFor — no template or call-site churn.
type InstallType interface {
	Name() string                  // "bundle" | "project" | "demo"
	ImageSuffix() string           // image tag component: ...-<suffix>-latest
	SourceRootContainer() string   // bundle: OroRoot/bundles/<ns>; project/demo: OroRoot
	QaAnalyzePath() string         // source tree PHPStan analyzes: bundle dir; project/demo: OroRoot/src
	RunsComposerRequire() bool     // bundle only
	RunsComposerInstall() bool     // project/demo only (vendor from repo's lock)
	SyncsVendorToHost() bool       // bundle only
	RequiresBundleNamespace() bool // bundle only — gates init prompts + validation
	WarnOnMissingComposerJSON() bool
	BindWholeRepo() bool // project/demo: bind repo onto OroRoot; bundle: false
	// MountsInternalEnvFiles reports whether Orobox bind-mounts its generated .env and
	// .env.test over the app's .env-app.local and .env-app.test. Bundle only: when the
	// whole checkout is bound onto OroRoot it already carries those files and owns them.
	MountsInternalEnvFiles() bool
}

// bundleType is the strategy for a single bundle grafted onto a prebuilt Oro app.
type bundleType struct{}

// Name returns the install type name for bundles.
func (bundleType) Name() string { return InstallTypeBundle }

// ImageSuffix returns the image tag component for bundles.
func (bundleType) ImageSuffix() string { return "bundle" }

// SourceRootContainer returns the bundle root path in the container.
func (bundleType) SourceRootContainer() string { return GetBundleRootContainerPath() }

// QaAnalyzePath returns the source tree path for PHPStan to analyze.
func (bundleType) QaAnalyzePath() string { return GetBundleRootContainerPath() }

// RunsComposerRequire returns true for bundle install type.
func (bundleType) RunsComposerRequire() bool { return true }

// RunsComposerInstall returns false for bundle install type.
func (bundleType) RunsComposerInstall() bool { return false }

// SyncsVendorToHost returns true for bundle install type.
func (bundleType) SyncsVendorToHost() bool { return true }

// RequiresBundleNamespace returns true for bundle install type.
func (bundleType) RequiresBundleNamespace() bool { return true }

// WarnOnMissingComposerJSON returns true for bundle install type.
func (bundleType) WarnOnMissingComposerJSON() bool { return true }

// BindWholeRepo returns false for bundle install type.
func (bundleType) BindWholeRepo() bool { return false }

// MountsInternalEnvFiles returns true for bundle install type.
func (bundleType) MountsInternalEnvFiles() bool { return true }

// projectType is the strategy where the whole checkout IS the Oro app.
type projectType struct{}

// Name returns the install type name for projects.
func (projectType) Name() string { return InstallTypeProject }

// ImageSuffix returns the image tag component for projects.
func (projectType) ImageSuffix() string { return "project" }

// SourceRootContainer returns the Oro root directory path in the container.
func (projectType) SourceRootContainer() string { return OroRootDir }

// QaAnalyzePath returns the source tree path for PHPStan to analyze.
func (projectType) QaAnalyzePath() string { return OroRootDir + "/src" }

// RunsComposerRequire returns false for project install type.
func (projectType) RunsComposerRequire() bool { return false }

// RunsComposerInstall returns true for project install type.
func (projectType) RunsComposerInstall() bool { return true }

// SyncsVendorToHost returns false for project install type.
func (projectType) SyncsVendorToHost() bool { return false }

// RequiresBundleNamespace returns false for project install type.
func (projectType) RequiresBundleNamespace() bool { return false }

// WarnOnMissingComposerJSON returns false for project install type.
func (projectType) WarnOnMissingComposerJSON() bool { return false }

// BindWholeRepo returns true for project install type.
func (projectType) BindWholeRepo() bool { return true }

// MountsInternalEnvFiles returns false for project install type.
func (projectType) MountsInternalEnvFiles() bool { return false }

// demoType is projectType with a production-tuned runtime: ORO_ENV=prod, OPcache enabled
// and no Xdebug compiled into the image. Every other behaviour is identical, so it embeds
// projectType and overrides only its identity and the image tag it resolves to.
type demoType struct{ projectType }

// Name returns the install type name for demo.
func (demoType) Name() string { return InstallTypeDemo }

// ImageSuffix returns the image tag component for demo.
func (demoType) ImageSuffix() string { return "demo" }

// InstallTypeFor resolves a type name to its strategy, or an error for unknown types.
// An empty name defaults to bundle.
func InstallTypeFor(name string) (InstallType, error) {
	switch name {
	case "", InstallTypeBundle:
		return bundleType{}, nil
	case InstallTypeProject:
		return projectType{}, nil
	case InstallTypeDemo:
		return demoType{}, nil
	default:
		return nil, fmt.Errorf("config error: unknown install type %q (expected %q, %q or %q)", name, InstallTypeBundle, InstallTypeProject, InstallTypeDemo)
	}
}
