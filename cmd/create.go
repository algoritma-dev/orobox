// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"

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
	createBundleNamespace string
	createBundlePackage   string
	createBundleDir       string
	createProjectVersion  string
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Scaffold a new project or bundle source tree",
	Long: `Create scaffolds a source tree on disk and stops. It does not touch Docker,
configuration, or the OroCommerce install — run 'orobox init' inside the created
directory for that.`,
}

var createProjectCmd = &cobra.Command{
	Use:   "project <name>",
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
	Use:           "bundle <ClassName>",
	Short:         "Scaffold a new OroCommerce bundle skeleton",
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(_ *cobra.Command, args []string) error {
		opts, err := scaffold.ParseBundleArg(args[0], createBundleNamespace, createBundlePackage)
		if err != nil {
			utils.PrintError(err.Error())
			return err
		}

		if createBundleNamespace == "" && opts.Namespace == opts.ClassName {
			utils.PrintWarning(fmt.Sprintf(
				"No namespace given; using top-level namespace %q. Pass a fully-qualified class or --namespace for a vendor namespace.",
				opts.Namespace,
			))
		}

		dest := createBundleDir
		if dest == "" {
			dest = opts.ClassName
		}

		utils.PrintInfo(fmt.Sprintf("Scaffolding bundle %q into %q...", opts.ClassName, dest))
		if err := scaffoldBundle(dest, opts); err != nil {
			utils.PrintError(err.Error())
			return err
		}
		utils.PrintSuccess(fmt.Sprintf("Bundle scaffolded in %q. Run 'orobox init' inside it to provision the environment.", dest))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(createCmd)
	createCmd.AddCommand(createProjectCmd)
	createCmd.AddCommand(createBundleCmd)

	createProjectCmd.Flags().StringVarP(&createProjectVersion, "oro-version", "v", "6.1", "OroCommerce version to scaffold")

	createBundleCmd.Flags().StringVarP(&createBundleNamespace, "namespace", "n", "", "PHP namespace for the bundle (e.g. Acme\\FooBundle)")
	createBundleCmd.Flags().StringVarP(&createBundlePackage, "package", "p", "", "Composer package name (e.g. acme/foo-bundle)")
	createBundleCmd.Flags().StringVar(&createBundleDir, "dir", "", "Target directory (default: bundle class name)")
}
