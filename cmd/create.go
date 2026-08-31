// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"os"

	"github.com/algoritma-dev/orobox/internal/scaffold"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
)

// Indirected for testing so command tests never touch the network or filesystem.
var (
	scaffoldBundle  = scaffold.Bundle
	scaffoldProject = scaffold.Project
)

var (
	createBundlePath       string
	createBundlePackage    string
	createBundleClass      string
	createBundleStandalone bool
	createProjectVersion   string
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Scaffold a new project or bundle source tree",
	Long: `Create lays down a source tree on disk and stops. It does not touch Docker,
configuration, or the OroCommerce install — run 'orobox init' in the created
directory for that.`,
}

var createProjectCmd = &cobra.Command{
	Use:   "project <path>",
	Short: "Scaffold a new OroCommerce project checkout",
	Args:  cobra.ExactArgs(1),
	// Errors are already reported via utils.PrintError; don't let cobra re-print them
	// or dump usage on a runtime failure.
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(_ *cobra.Command, args []string) error {
		dest := args[0]
		utils.PrintInfo(fmt.Sprintf("Scaffolding OroCommerce project into %q...", dest))
		if err := scaffoldProject(dest, createProjectVersion); err != nil {
			utils.PrintError(err.Error())
			return err
		}
		utils.PrintSuccess(fmt.Sprintf("Project scaffolded in %q. Run 'orobox init' inside it to provision the environment.", dest))
		return nil
	},
}

var createBundleCmd = &cobra.Command{
	Use:   "bundle <namespace>",
	Short: "Scaffold a new OroCommerce bundle skeleton",
	Long: `Bundle generates an Oro bundle skeleton for a PHP namespace.

Where it lands comes from the composer.json in the current directory. An OroCommerce
application autoloads "": "src/", so inside a project checkout

    orobox create bundle 'Acme\Bundle\FooBundle'

writes src/Acme/Bundle/FooBundle/ — already autoloaded by the project and discovered by
Oro's kernel, so the bundle needs no composer.json of its own. Outside a PHP project, or
with --standalone, the bundle becomes its own composer package instead. Use --path to put
it somewhere else.`,
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(_ *cobra.Command, args []string) error {
		opts, err := scaffold.ParseBundleArg(args[0], createBundleClass, createBundlePackage)
		if err != nil {
			utils.PrintError(err.Error())
			return err
		}

		root, err := os.Getwd()
		if err != nil {
			utils.PrintError(err.Error())
			return err
		}

		placement, err := scaffold.ResolveBundlePlacement(root, opts, createBundlePath, createBundleStandalone)
		if err != nil {
			utils.PrintError(err.Error())
			return err
		}
		opts.Standalone = placement.Standalone

		if placement.Standalone {
			utils.PrintWarning(fmt.Sprintf(
				"No composer.json here autoloads %s, so the bundle is generated as its own composer package %q.",
				opts.Namespace, opts.PackageName,
			))
		} else {
			utils.PrintInfo(fmt.Sprintf(
				"composer.json autoloads %q from %s/, so %s belongs in %s.",
				placement.Psr4.Prefix, placement.Psr4.Dir, opts.Namespace, placement.Dir,
			))
		}

		utils.PrintInfo(fmt.Sprintf("Scaffolding bundle %s into %q...", opts.ClassName, placement.Dir))
		if err := scaffoldBundle(placement.Dest(root), opts); err != nil {
			utils.PrintError(err.Error())
			return err
		}

		if placement.Standalone {
			utils.PrintSuccess(fmt.Sprintf("Bundle scaffolded in %q. Run 'orobox init' inside it to provision the environment.", placement.Dir))
		} else {
			utils.PrintSuccess(fmt.Sprintf("Bundle scaffolded in %q. It is autoloaded by this project already — clear the cache to let Oro pick it up.", placement.Dir))
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(createCmd)
	createCmd.AddCommand(createProjectCmd)
	createCmd.AddCommand(createBundleCmd)

	createProjectCmd.Flags().StringVarP(&createProjectVersion, "oro-version", "v", "6.1", "OroCommerce version to scaffold")

	createBundleCmd.Flags().StringVar(&createBundlePath, "path", "", "Target directory (default: the PSR-4 path for the namespace, or the class name when standalone)")
	createBundleCmd.Flags().StringVarP(&createBundlePackage, "package", "p", "", "Composer package name for a standalone bundle (e.g. acme/foo-bundle)")
	createBundleCmd.Flags().StringVar(&createBundleClass, "class", "", "Bundle class name (default: derived from the namespace, e.g. AcmeFooBundle)")
	createBundleCmd.Flags().BoolVar(&createBundleStandalone, "standalone", false, "Generate the bundle as its own composer package, ignoring this directory's PSR-4 map")
}
