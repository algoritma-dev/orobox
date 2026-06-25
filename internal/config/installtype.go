package config

import "fmt"

// InstallType encapsulates everything that differs between Orobox install types.
// Adding a new type (e.g. "demo") is a single new struct implementing this
// interface plus a case in InstallTypeFor — no template or call-site churn.
type InstallType interface {
	Name() string                  // "bundle" | "project"
	ImageSuffix() string           // image tag component: ...-<suffix>-latest
	SourceRootContainer() string   // bundle: OroRoot/bundles/<ns>; project: OroRoot
	RunsComposerRequire() bool     // bundle only
	RunsComposerInstall() bool     // project only (vendor from repo's lock)
	SyncsVendorToHost() bool       // bundle only
	RequiresBundleNamespace() bool // bundle only — gates init prompts + validation
	WarnOnMissingComposerJSON() bool
	BindWholeRepo() bool // project: bind repo onto OroRoot; bundle: false
}

// bundleType is the strategy for a single bundle grafted onto a prebuilt Oro app.
type bundleType struct{}

func (bundleType) Name() string                    { return InstallTypeBundle }
func (bundleType) ImageSuffix() string             { return "bundle" }
func (bundleType) SourceRootContainer() string     { return GetBundleRootContainerPath() }
func (bundleType) RunsComposerRequire() bool       { return true }
func (bundleType) RunsComposerInstall() bool       { return false }
func (bundleType) SyncsVendorToHost() bool         { return true }
func (bundleType) RequiresBundleNamespace() bool   { return true }
func (bundleType) WarnOnMissingComposerJSON() bool { return true }
func (bundleType) BindWholeRepo() bool             { return false }

// projectType is the strategy where the whole checkout IS the Oro app.
type projectType struct{}

func (projectType) Name() string                    { return InstallTypeProject }
func (projectType) ImageSuffix() string             { return "project" }
func (projectType) SourceRootContainer() string     { return OroRootDir }
func (projectType) RunsComposerRequire() bool       { return false }
func (projectType) RunsComposerInstall() bool       { return true }
func (projectType) SyncsVendorToHost() bool         { return false }
func (projectType) RequiresBundleNamespace() bool   { return false }
func (projectType) WarnOnMissingComposerJSON() bool { return false }
func (projectType) BindWholeRepo() bool             { return true }

// InstallTypeFor resolves a type name to its strategy, or an error for unknown types.
// An empty name defaults to bundle.
func InstallTypeFor(name string) (InstallType, error) {
	switch name {
	case "", InstallTypeBundle:
		return bundleType{}, nil
	case InstallTypeProject:
		return projectType{}, nil
	default:
		return nil, fmt.Errorf("config error: unknown install type %q (expected %q or %q)", name, InstallTypeBundle, InstallTypeProject)
	}
}
