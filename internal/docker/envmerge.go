package docker

import (
	"bytes"
	"strings"
)

// MergeEnv applies the project's assignments over the rendered template: a key the template
// defines is replaced on its own line; other keys are appended after "# From <source>".
//
// The project file is a sparse override, not a replacement: the template keeps its order,
// comments and every key the project does not mention, so a key added by a later Orobox
// release still reaches a project whose .env was written against an older one. Values are
// copied verbatim — "${...}" references included — because Symfony's Dotenv resolves them
// at load time, against the merged file.
func MergeEnv(template, project []byte, source string) []byte {
	keys, values := parseEnvAssignments(project)
	if len(keys) == 0 {
		return template
	}

	lines := strings.Split(string(template), "\n")
	applied := make(map[string]bool, len(keys))
	for i, line := range lines {
		key, _, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		value, overridden := values[key]
		// Only the first definition is replaced; a later duplicate in the template is left
		// alone because the templates never define a key twice.
		if !overridden || applied[key] {
			continue
		}
		applied[key] = true
		lines[i] = key + "=" + value
	}

	var out bytes.Buffer
	out.WriteString(strings.Join(lines, "\n"))

	first := true
	for _, key := range keys {
		if applied[key] {
			continue
		}
		if first {
			// Separate the appended block from the template with one blank line, closing a
			// final line that has no newline of its own.
			if out.Len() > 0 && !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
				out.WriteByte('\n')
			}
			out.WriteString("\n# From " + source + "\n")
			first = false
		}
		out.WriteString(key + "=" + values[key] + "\n")
	}

	return out.Bytes()
}

// parseEnvAssignments reads a project env file leniently: carriage returns, blank lines,
// comments, an optional leading "export " and lines without "=" are all tolerated. It returns
// the keys in order of first appearance and the value of the last assignment of each.
func parseEnvAssignments(content []byte) ([]string, map[string]string) {
	var keys []string
	values := make(map[string]string)

	for _, line := range strings.Split(string(content), "\n") {
		key, value, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		if _, seen := values[key]; !seen {
			keys = append(keys, key)
		}
		values[key] = value
	}

	return keys, values
}

// parseEnvLine splits one KEY=value line. The value is everything after the first "=",
// untouched, so quotes, spaces and "${...}" references survive the merge byte for byte.
func parseEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimRight(line, "\r")
	trimmed := strings.TrimLeft(line, " \t")
	if strings.TrimSpace(trimmed) == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}

	trimmed = strings.TrimPrefix(trimmed, "export ")
	idx := strings.Index(trimmed, "=")
	if idx < 0 {
		return "", "", false
	}

	key = strings.TrimSpace(trimmed[:idx])
	if key == "" {
		return "", "", false
	}

	// Only the line's leading whitespace was trimmed, so trailing spaces after the value
	// stay part of it, exactly as dotenv would see them.
	return key, trimmed[idx+1:], true
}
