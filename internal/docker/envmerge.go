package docker

import (
	"bytes"
	"strings"
)

// MergeEnv applies the project's assignments over the rendered template, in two places:
//
//   - in the template part, the first definition of every key the project sets is replaced on
//     its own line, so template keys that reference it (`URL=https://${ORO_APP_DOMAIN}`) are
//     read with the project's value;
//   - after "# From <source>", every project assignment is appended in the project file's own
//     order, overrides included. Dotenv and compose let the last assignment of a key win, and
//     Dotenv resolves "${...}" against what it has read so far, so this block reads exactly as
//     the project file would on its own: `ORO_APP_DOMAIN=${MY_HOST}` resolves even when
//     MY_HOST is a key the template does not have.
//
// The project file is a sparse override, not a replacement: the template keeps its order,
// comments and every key the project does not mention, so a key added by a later Orobox
// release still reaches a project whose .env was written against an older one. Values are
// copied verbatim — quotes, multi-line quoted values and "${...}" references included; only a
// leading "export ", leading whitespace and carriage returns are dropped.
func MergeEnv(template, project []byte, source string) []byte {
	projectEntries := parseEnvEntries(string(project))
	values := make(map[string]string)
	hasAssignments := false
	for _, e := range projectEntries {
		if e.assignment {
			values[e.key] = e.value
			hasAssignments = true
		}
	}
	if !hasAssignments {
		return template
	}

	templateEntries := parseEnvEntries(string(template))
	applied := make(map[string]bool, len(values))
	parts := make([]string, len(templateEntries))
	for i, e := range templateEntries {
		parts[i] = e.raw
		if !e.assignment {
			continue
		}
		value, overridden := values[e.key]
		// Only the first definition is replaced; a later duplicate in the template is left
		// alone because the templates never define a key twice.
		if !overridden || applied[e.key] {
			continue
		}
		applied[e.key] = true
		parts[i] = e.key + "=" + value
	}

	var out bytes.Buffer
	out.WriteString(strings.Join(parts, "\n"))

	// Separate the appended block from the template with one blank line, closing a final line
	// that has no newline of its own.
	if out.Len() > 0 && !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
		out.WriteByte('\n')
	}
	out.WriteString("\n# From " + source + "\n")
	for _, e := range projectEntries {
		if e.assignment {
			out.WriteString(e.key + "=" + e.value + "\n")
		}
	}

	return out.Bytes()
}

// envEntry is one logical unit of an env file: a single line, or an assignment whose quoted
// value spans several lines.
type envEntry struct {
	// raw is the unit as written, its lines joined by "\n" (without a final one), so joining
	// every entry with "\n" gives the file back byte for byte.
	raw        string
	assignment bool
	key        string
	// value is everything after the first "=", untouched apart from carriage returns at line
	// ends; a multi-line value keeps its inner newlines.
	value string
}

// parseEnvEntries splits an env file into entries. An assignment whose value opens a quote
// ('"' or "'") that the line does not close continues on the following lines until the quote
// closes — Dotenv reads such a value as one, so a continuation line that looks like KEY=value is
// part of it, not an assignment. A quote that never closes extends to the end of the file.
func parseEnvEntries(content string) []envEntry {
	lines := strings.Split(content, "\n")
	var entries []envEntry
	for i := 0; i < len(lines); i++ {
		key, value, ok := parseEnvLine(lines[i])
		if !ok {
			entries = append(entries, envEntry{raw: lines[i]})
			continue
		}

		raw := []string{lines[i]}
		valueLines := []string{value}
		if quote, open := openQuote(value); open {
			body := strings.TrimLeft(value, " \t")[1:]
			for !quoteCloses(body, quote) && i+1 < len(lines) {
				i++
				raw = append(raw, lines[i])
				line := strings.TrimRight(lines[i], "\r")
				valueLines = append(valueLines, line)
				body += "\n" + line
			}
		}
		entries = append(entries, envEntry{
			raw:        strings.Join(raw, "\n"),
			assignment: true,
			key:        key,
			value:      strings.Join(valueLines, "\n"),
		})
	}
	return entries
}

// openQuote reports whether a value starts a quoted string, and with which quote character.
func openQuote(value string) (byte, bool) {
	v := strings.TrimLeft(value, " \t")
	if v == "" || (v[0] != '"' && v[0] != '\'') {
		return 0, false
	}
	return v[0], !quoteCloses(v[1:], v[0])
}

// quoteCloses reports whether body (the text after an opening quote) contains the closing
// quote. Inside double quotes a backslash escapes the next character, as in Dotenv; single
// quotes have no escapes.
func quoteCloses(body string, quote byte) bool {
	for i := 0; i < len(body); i++ {
		switch {
		case quote == '"' && body[i] == '\\':
			i++
		case body[i] == quote:
			return true
		}
	}
	return false
}

// parseEnvAssignments reads a project env file leniently: carriage returns, blank lines,
// comments, an optional leading "export ", lines without "=" and multi-line quoted values are
// all tolerated. It returns the keys in order of first appearance and the value of the last
// assignment of each.
func parseEnvAssignments(content []byte) ([]string, map[string]string) {
	var keys []string
	values := make(map[string]string)

	for _, e := range parseEnvEntries(string(content)) {
		if !e.assignment {
			continue
		}
		if _, seen := values[e.key]; !seen {
			keys = append(keys, e.key)
		}
		values[e.key] = e.value
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
