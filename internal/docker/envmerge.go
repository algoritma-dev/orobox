package docker

import (
	"bytes"
	"regexp"
	"strings"
)

// MergeEnv layers the project's assignments over the rendered template, so that what Dotenv and
// compose read is what the project meant. Both read a file in order, let the last assignment of
// a key win, and resolve "${...}" against what they have read so far. The merged file is:
//
//   - the template, unchanged;
//   - after "# From <source>", every project assignment, in the project file's own order. Its
//     references resolve as they would in the project file alone, against the template above:
//     `ORO_APP_DOMAIN=${MY_HOST}` works when MY_HOST is a key only the project has, and
//     `OPTS=${OPTS} --more` extends the generated value once;
//   - after that, again, every template assignment that references a key the project set
//     (directly or through another such assignment) and that the project does not set itself.
//     Read in the template part, they used the generated values; read again here, they use the
//     project's — `ORO_APP_URL=http://${ORO_APP_DOMAIN}/` follows the project's domain.
//
// The project file is a sparse override, not a replacement: every key it does not mention keeps
// the template's value, so a key added by a later Orobox release still reaches a project whose
// .env was written against an older one. Values are copied verbatim — quotes, multi-line quoted
// values and "${...}" references included; only a leading "export ", leading whitespace and
// carriage returns are dropped.
func MergeEnv(template, project []byte, source string) []byte {
	projectEntries := parseEnvEntries(string(project))
	set := make(map[string]bool)
	for _, e := range projectEntries {
		if e.assignment {
			set[e.key] = true
		}
	}
	if len(set) == 0 {
		return template
	}

	var out bytes.Buffer
	out.Write(template)
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

	// Template assignments that depend on a changed key, in template order: a template key only
	// references keys defined above it, so one pass also catches dependencies of dependencies.
	changed := make(map[string]bool, len(set))
	for key := range set {
		changed[key] = true
	}
	var reread []envEntry
	for _, e := range parseEnvEntries(string(template)) {
		if !e.assignment || set[e.key] || !referencesAny(e.value, changed) {
			continue
		}
		reread = append(reread, e)
		changed[e.key] = true
	}
	if len(reread) > 0 {
		out.WriteString("\n# Generated values that use the keys above, read again\n")
		for _, e := range reread {
			out.WriteString(e.key + "=" + e.value + "\n")
		}
	}

	return out.Bytes()
}

// envReference matches a ${NAME} (or ${NAME:-default}, ${NAME-default}) or $NAME reference.
var envReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)|\$([A-Za-z_][A-Za-z0-9_]*)`)

// referencesAny reports whether a dotenv value references one of keys. A single-quoted value is a
// literal in Dotenv and compose, so it references nothing.
func referencesAny(value string, keys map[string]bool) bool {
	v := strings.TrimSpace(value)
	if strings.HasPrefix(v, "'") {
		return false
	}
	for _, m := range envReference.FindAllStringSubmatch(v, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		if keys[name] {
			return true
		}
	}
	return false
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
