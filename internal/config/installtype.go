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

func (bundleType) Name() string                    { return InstallTypeBundle }
func (bundleType) ImageSuffix() string             { return "bundle" }
func (bundleType) SourceRootContainer() string     { return GetBundleRootContainerPath() }
func (bundleType) QaAnalyzePath() string           { return GetBundleRootContainerPath() }
func (bundleType) RunsComposerRequire() bool       { return true }
func (bundleType) RunsComposerInstall() bool       { return false }
func (bundleType) SyncsVendorToHost() bool         { return true }
func (bundleType) RequiresBundleNamespace() bool   { return true }
func (bundleType) WarnOnMissingComposerJSON() bool { return true }
func (bundleType) BindWholeRepo() bool             { return false }
func (bundleType) MountsInternalEnvFiles() bool    { return true }

// projectType is the strategy where the whole checkout IS the Oro app.
type projectType struct{}

func (projectType) Name() string                    { return InstallTypeProject }
func (projectType) ImageSuffix() string             { return "project" }
func (projectType) SourceRootContainer() string     { return OroRootDir }
func (projectType) QaAnalyzePath() string           { return OroRootDir + "/src" }
func (projectType) RunsComposerRequire() bool       { return false }
func (projectType) RunsComposerInstall() bool       { return true }
func (projectType) SyncsVendorToHost() bool         { return false }
func (projectType) RequiresBundleNamespace() bool   { return false }
func (projectType) WarnOnMissingComposerJSON() bool { return false }
func (projectType) BindWholeRepo() bool             { return true }
func (projectType) MountsInternalEnvFiles() bool    { return false }

// demoType is projectType with a production-tuned runtime: ORO_ENV=prod, OPcache enabled
// and no Xdebug compiled into the image. Every other behaviour is identical, so it embeds
// projectType and overrides only its identity and the image tag it resolves to.
type demoType struct{ projectType }

func (demoType) Name() string        { return InstallTypeDemo }
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
