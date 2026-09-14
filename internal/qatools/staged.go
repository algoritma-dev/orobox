package qatools

import (
	"path/filepath"
	"strings"

	"github.com/algoritma-dev/orobox/internal/utils"
)

// stagedExtensions lists, per tool, the file extensions that tool has anything to say about.
//
// A tool absent from this map is not narrowed at all — PHPStan is the one that matters. Its
// analysis is a whole-project one by nature: a changed class breaks the type checks of callers the
// commit never touched, and handing it a file list would report a clean tree that is not clean.
// The same reasoning keeps a file list away from PHPUnit; see the pre-commit hook template.
var stagedExtensions = map[string][]string{
	"rector":        {".php"},
	"php-cs-fixer":  {".php"},
	"twig-cs-fixer": {".twig"},
	"eslint":        {".js"},
	"stylelint":     {".scss", ".less", ".sass", ".html"},
	"stylelint-css": {".css"},
}

// stagedGlobs names the tools whose default arguments already carry a positional target, which a
// file list replaces rather than joins: the glob and the list are the same argument, and a tool
// given both would check the whole tree again.
var stagedGlobs = map[string]string{
	"eslint":        jsTarget,
	"stylelint":     scssTarget,
	"stylelint-css": cssTarget,
}

// Restrict narrows a tool list to the given files, which are container paths.
//
// A tool with nothing to check is dropped rather than run with an empty file list: an empty
// invocation means "the whole tree" to some of these CLIs and "no input, fail" to others, and
// neither is what a commit of three templates asked for.
//
// The paths are absolute because they outlive the working directory they were produced in: Rector
// runs from OroRoot through Tool.WorkDir while the rest run from the source root, so a relative
// path would name a different file for one tool than for the others.
func Restrict(tools []Tool, files []string) []Tool {
	restricted := make([]Tool, 0, len(tools))

	for _, tool := range tools {
		extensions, narrowed := stagedExtensions[tool.Name]
		if !narrowed {
			restricted = append(restricted, tool)
			continue
		}

		matched := filesWithExtensions(files, extensions)
		if len(matched) == 0 {
			continue
		}

		tool.Args = argsForFiles(tool.Name, tool.Args, matched)
		restricted = append(restricted, tool)
	}

	return restricted
}

// filesWithExtensions keeps the files one tool can read, matching case-insensitively because the
// extension comes from a filename on disk and .PHP is a file PHP-CS-Fixer still fixes.
func filesWithExtensions(files, extensions []string) []string {
	var matched []string
	for _, file := range files {
		extension := strings.ToLower(filepath.Ext(file))
		for _, want := range extensions {
			if extension == want {
				matched = append(matched, file)
				break
			}
		}
	}
	return matched
}

// argsForFiles puts the file list where the tool expects its input.
//
// --path-mode=intersection is what keeps PHP-CS-Fixer's own configuration in force: with an
// explicit path its default mode overrides the config's Finder outright, so a staged file the
// project deliberately excluded — anything under vendor-oro on a bundle checkout — would be fixed
// after all. Intersection runs only the files that are in both.
func argsForFiles(name string, args, files []string) []string {
	quoted := make([]string, 0, len(files))
	for _, file := range files {
		quoted = append(quoted, utils.ShellQuote(file))
	}

	if glob, hasGlob := stagedGlobs[name]; hasGlob {
		narrowed := make([]string, 0, len(args)+len(quoted))
		for _, arg := range args {
			if arg == glob {
				narrowed = append(narrowed, quoted...)
				continue
			}
			narrowed = append(narrowed, arg)
		}
		return narrowed
	}

	narrowed := make([]string, 0, len(args)+len(quoted)+1)
	narrowed = append(narrowed, args...)
	if name == "php-cs-fixer" {
		narrowed = append(narrowed, "--path-mode=intersection")
	}
	return append(narrowed, quoted...)
}
