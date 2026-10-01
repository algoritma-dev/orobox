// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/output"
	"github.com/algoritma-dev/orobox/internal/scaffold"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
)

// extendLocal and extendForce are package-level because cobra binds the flags to them; tests that
// run the command more than once reset them, since cobra does not between Execute calls.
var (
	extendLocal bool
	extendForce bool
)

var extendCmd = &cobra.Command{
	Use:   "extend",
	Short: "Scaffold the files that customize the environment",
	Long: `Extend writes the files that customize the stack, with the required headers and
commented examples, so nobody has to remember the file names or the syntax.

It never overwrites a file: an existing one is left alone and reported as skipped.
Every file it touches is reported on its own line as "<action> <path>", where the
action is created, updated or skipped.`,
}

var extendImageCmd = &cobra.Command{
	Use:   "image",
	Short: "Create docker/Dockerfile and set image.dockerfile in .orobox.yaml",
	Long: `Creates a project Dockerfile that extends the Orobox image, with commented examples,
and points image.dockerfile in .orobox.yaml at it. The comments in .orobox.yaml are kept.

When .orobox.yaml already names a Dockerfile, that path is used and the config is not
changed. For a package or a PHP extension, image.apk and image.php_extensions in
.orobox.yaml are simpler than a Dockerfile.`,
	Args: cobra.NoArgs,
	// Errors are already reported via utils.PrintError; don't let cobra re-print them
	// or dump usage on a runtime failure.
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(_ *cobra.Command, _ []string) error {
		receipts, err := scaffold.ExtendImage(config.GetHostBundlePath())
		printReceipts(receipts)
		if err != nil {
			utils.PrintError(err.Error())
			return err
		}
		utils.PrintInfo("Add your instructions to the Dockerfile; 'orobox up' rebuilds the image when it changes.")
		return nil
	},
}

var extendComposeCmd = &cobra.Command{
	Use:   "compose",
	Short: "Create .orobox.compose.yaml, or the git-ignored personal one with --local",
	Long: `Creates .orobox.compose.yaml with commented examples: a new service, an environment
variable on a core service, an extra mount and a replaced port list.

With --local it creates .orobox.compose.local.yaml instead, the file for one developer's
own tweaks, and adds it to .gitignore when that file exists and does not list it yet.`,
	Args:          cobra.NoArgs,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(_ *cobra.Command, _ []string) error {
		receipts, err := scaffold.ExtendCompose(config.GetHostBundlePath(), extendLocal)
		printReceipts(receipts)
		if err != nil {
			utils.PrintError(err.Error())
			return err
		}
		utils.PrintInfo("Edit the file and run 'orobox up' to apply it.")
		return nil
	},
}

var extendAddCmd = &cobra.Command{
	Use:   "add [recipe...]",
	Short: "Add a ready-made service to the stack, or list the recipes",
	Long: `Without arguments, lists the available recipes with a one-line description each.

With recipe names, adds each one in order: its services are merged into
.orobox.compose.yaml, its settings into .orobox.yaml (lists appended without duplicates,
other keys only when missing) and its variables into the project .env (only the ones it
does not define). Its files are copied to docker/<recipe>/. A value the project already
has always wins, and an existing file is never overwritten.

A recipe whose service .orobox.compose.yaml already defines is refused; --force replaces
that service and nothing else. After the receipts, each recipe prints what is left to do.`,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(_ *cobra.Command, args []string) error {
		recipes, err := scaffold.EmbeddedRecipes()
		if err != nil {
			utils.PrintError(err.Error())
			return err
		}
		if len(args) == 0 {
			// The list is what the command produces here, so it goes to the payload stream.
			for _, r := range recipes {
				fmt.Fprintf(output.Payload(), "%s — %s\n", r.Name, r.Description)
			}
			return nil
		}

		// Every name is checked before the first recipe is applied: a typo late in the list must
		// not leave the ones before it added.
		selected, err := selectRecipes(recipes, args)
		if err != nil {
			utils.PrintError(err.Error())
			return err
		}

		root := config.GetHostBundlePath()
		var added []scaffold.Recipe
		for _, r := range selected {
			receipts, err := scaffold.AddRecipe(root, r, extendForce)
			printReceipts(receipts)
			if err != nil {
				printRecipeNotes(added)
				err = fmt.Errorf("could not add the %s recipe: %w", r.Name, err)
				utils.PrintError(err.Error())
				return err
			}
			added = append(added, r)
		}
		printRecipeNotes(added)
		return nil
	},
}

func init() {
	extendComposeCmd.Flags().BoolVar(&extendLocal, "local", false,
		"create .orobox.compose.local.yaml (personal, git-ignored) instead")
	extendAddCmd.Flags().BoolVar(&extendForce, "force", false,
		"replace the recipe's services when .orobox.compose.yaml already defines them")
	extendCmd.AddCommand(extendImageCmd, extendComposeCmd, extendAddCmd)
	rootCmd.AddCommand(extendCmd)
}

// selectRecipes returns the named recipes in the order given, each once, or an error naming the
// unknown ones and every valid name.
func selectRecipes(recipes []scaffold.Recipe, names []string) ([]scaffold.Recipe, error) {
	byName := make(map[string]scaffold.Recipe, len(recipes))
	valid := make([]string, 0, len(recipes))
	for _, r := range recipes {
		byName[r.Name] = r
		valid = append(valid, r.Name)
	}

	var selected []scaffold.Recipe
	var unknown []string
	seen := map[string]bool{}
	for _, name := range names {
		r, ok := byName[name]
		switch {
		case !ok:
			unknown = append(unknown, name)
		case !seen[name]:
			seen[name] = true
			selected = append(selected, r)
		}
	}
	if len(unknown) > 0 {
		return nil, errors.New("unknown recipe " + strings.Join(unknown, ", ") + "; available: " + strings.Join(valid, ", "))
	}
	return selected, nil
}

// printRecipeNotes prints what each added recipe leaves to the user. They are hints around the
// receipts, so agent mode drops them.
func printRecipeNotes(recipes []scaffold.Recipe) {
	for _, r := range recipes {
		if r.Notes == "" {
			continue
		}
		utils.PrintTitle(r.Name)
		utils.PrintPlain(r.Notes)
	}
}

// printReceipts writes one "<action> <path>" line per receipt to the payload stream: the
// receipts are what the command produces, so agent mode keeps them while dropping the hints
// printed around them. Shared with the subcommands that report through the same type.
func printReceipts(receipts []scaffold.Receipt) {
	for _, r := range receipts {
		fmt.Fprintln(output.Payload(), r)
	}
}
