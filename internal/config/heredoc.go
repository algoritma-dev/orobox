package config

import (
	"regexp"
	"strings"
)

// Heredoc is a heredoc a Dockerfile instruction line opens: the delimiter that closes it, and
// whether `<<-` asks for leading tabs to be stripped from the body (and the delimiter line).
type Heredoc struct {
	Delimiter string
	StripTabs bool
}

// HeredocWords returns the heredocs line opens, in order, the way BuildKit finds them: a heredoc
// is a word, outside any quotes, of the form `[digits]<<[-]WORD` — not a `<<<` here-string, not
// a `<<` in the middle of a word (`$((1<<4))`). The delimiter is WORD with its quotes and
// backslashes removed (`<<\EOF`, `<<"EOF"`, `<<”EOF` all close on `EOF`).
//
// It is shared by the image.run validation, which refuses heredocs, and by the Dockerfile parser
// that has to skip heredoc bodies; both must agree with BuildKit, or a heredoc slips through one
// and a valid Dockerfile is refused by the other.
func HeredocWords(line string) []Heredoc {
	var found []Heredoc
	for _, word := range unquotedWords(line) {
		rest := strings.TrimLeft(word, "0123456789")
		rest, ok := strings.CutPrefix(rest, "<<")
		if !ok || strings.HasPrefix(rest, "<") {
			continue
		}
		h := Heredoc{}
		if r, ok := strings.CutPrefix(rest, "-"); ok {
			h.StripTabs, rest = true, r
		}
		h.Delimiter = strings.NewReplacer(`"`, "", `'`, "", `\`, "").Replace(rest)
		if h.Delimiter != "" {
			found = append(found, h)
		}
	}
	return found
}

// HasShellHeredoc reports whether line holds a heredoc for the shell, not only for BuildKit:
// besides the words HeredocWords finds, a `<<` or `<<-` standing alone before the delimiter
// (`cat << 'EOF'`), which BuildKit leaves to the shell but still needs a body on the next lines.
func HasShellHeredoc(line string) bool {
	if len(HeredocWords(line)) > 0 {
		return true
	}
	// `$((1 << 4))` is a shift, not a heredoc.
	words := unquotedWords(arithmeticExpansion.ReplaceAllString(line, "0"))
	// The last word has no delimiter after it, so a `<<` there opens nothing.
	for _, word := range words[:max(len(words)-1, 0)] {
		if rest := strings.TrimLeft(word, "0123456789"); rest == "<<" || rest == "<<-" {
			return true
		}
	}
	return false
}

// arithmeticExpansion matches a shell `$((...))` without nested parentheses.
var arithmeticExpansion = regexp.MustCompile(`\$\(\([^()]*\)\)`)

// unquotedWords splits line on spaces and tabs, returning only the words that start outside any
// quotes; a word that starts inside a quoted string is part of that string, not shell syntax.
func unquotedWords(line string) []string {
	var words []string
	var quote byte
	start := -1
	startedQuoted := false
	for i := 0; i <= len(line); i++ {
		var c byte
		if i < len(line) {
			c = line[i]
		}
		switch {
		case i == len(line) || (quote == 0 && (c == ' ' || c == '\t')):
			if start >= 0 && !startedQuoted {
				words = append(words, line[start:i])
			}
			start = -1
			continue
		}
		if start < 0 {
			start, startedQuoted = i, quote != 0
		}
		switch {
		case quote == 0 && c == '\\' && i+1 < len(line):
			i++
		case quote == 0 && (c == '"' || c == '\''):
			// A quote that opens the word is the word's own quoting (`'<<EOF'` is a quoted
			// string); one inside it (`<<'EOF'`) is part of a heredoc delimiter.
			if i == start {
				startedQuoted = true
			}
			quote = c
		case quote != 0 && c == quote:
			quote = 0
		case quote == '"' && c == '\\' && i+1 < len(line):
			i++
		}
	}
	return words
}
