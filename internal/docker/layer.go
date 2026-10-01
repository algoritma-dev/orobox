package docker

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
)

// InstallPhpExtensionsVersion is the mlocati/docker-php-extension-installer release the layer
// falls back to. It must equal the `ARG INSTALL_PHP_EXTENSIONS_VERSION=` default in
// templates/docker/Dockerfile (a test holds them together), so a layer never builds with a
// different installer than the base image ships.
const InstallPhpExtensionsVersion = "2.12.0"

// installPhpExtensionsBootstrap installs the extension installer when the base image lacks it.
// Base images published before it was shipped in the final stage do not have it, and a cached
// copy of one can stay on a machine long after newer ones exist; on a current image the
// `command -v` check makes this a no-op that downloads nothing.
const installPhpExtensionsBootstrap = "RUN command -v install-php-extensions >/dev/null 2>&1 || " +
	"(curl -fsSL https://github.com/mlocati/docker-php-extension-installer/releases/download/" +
	InstallPhpExtensionsVersion + "/install-php-extensions -o /usr/local/bin/install-php-extensions" +
	" && chmod +x /usr/local/bin/install-php-extensions)"

// RenderLayerDockerfile returns the Dockerfile the custom layer is built from: the project's
// Dockerfile (nil when none) followed by one RUN per non-empty image.* key. Pure: no I/O.
//
// It is a pure function over bytes so the local build and the Dagger pipeline, which obtain the
// project Dockerfile differently, render exactly the same layer — and so the result can be
// hashed to decide whether the layer is stale. Entries are expected to have passed
// config.Validate; nothing here validates or escapes them.
func RenderLayerDockerfile(projectDockerfile []byte, img config.ImageConfig) []byte {
	runs := layerRunLines(img)

	// Nothing declarative to add: the project Dockerfile is the whole layer and is returned
	// byte for byte, so a Dockerfile-only project hashes and builds exactly as written.
	if len(runs) == 0 {
		return projectDockerfile
	}

	var out bytes.Buffer

	if len(projectDockerfile) == 0 {
		// No project Dockerfile to supply the base: open one on the published image. The base
		// is a build argument so `oro_version` stays the only thing choosing the Oro image.
		out.WriteString("ARG " + config.DockerfileBaseImageArg + "\n")
		out.WriteString("FROM ${" + config.DockerfileBaseImageArg + "}\n")
	} else {
		// The project file already declares ARG/FROM — repeating them would start a second
		// stage and drop the project's own instructions from the final image.
		out.Write(projectDockerfile)
		if projectDockerfile[len(projectDockerfile)-1] != '\n' {
			out.WriteByte('\n')
		}
	}

	// The generated RUN lines (apk, install-php-extensions, global npm) need root, so the layer
	// ends as root even when the project Dockerfile ended on another USER. The user the
	// containers actually run as comes from the compose `user:` setting where one is set, not
	// from the image's final USER.
	out.WriteString("USER root\n")

	for _, line := range runs {
		out.WriteString(line + "\n")
	}

	return out.Bytes()
}

// layerRunLines turns the declarative image.* keys into RUN instructions, skipping empty keys.
// Each `run` entry stays its own RUN, in order, so a failing command is identifiable in the
// build output and Docker caches the preceding ones.
func layerRunLines(img config.ImageConfig) []string {
	var runs []string

	if len(img.Apk) > 0 {
		runs = append(runs, "RUN apk add --no-cache "+strings.Join(img.Apk, " "))
	}

	if len(img.PhpExtensions) > 0 {
		runs = append(runs, installPhpExtensionsBootstrap)
		runs = append(runs, "RUN install-php-extensions "+strings.Join(img.PhpExtensions, " "))
	}

	if len(img.Npm) > 0 {
		runs = append(runs, "RUN npm install -g "+strings.Join(img.Npm, " "))
	}

	for _, cmd := range img.Run {
		runs = append(runs, "RUN "+cmd)
	}

	return runs
}

// phpIniQuoted lists the characters that make an unquoted php.ini value mean something other
// than the text itself: `;` starts a comment, `=` an assignment, `"` and `'` open strings (an
// unclosed `'` swallows every directive after it), and braces and brackets belong to variable
// and section syntax. A value containing any of them is double-quoted.
const phpIniQuoted = `;="'{}[]#`

// phpIniOperators are the operators of php.ini's expression syntax. Raw, a value using them is
// evaluated (`E_ALL & ~E_DEPRECATED` becomes a number); quoted, it is a literal string.
const phpIniOperators = "|&^~!"

// phpIniKeywords are the words php.ini turns into "1" or "" when they appear unquoted. A YAML
// string holding one is quoted so it reaches PHP as the word itself: `session.cookie_samesite:
// None` must not become an empty setting. A YAML boolean is a different value and becomes On/Off.
var phpIniKeywords = map[string]bool{
	"none": true, "null": true, "yes": true, "no": true,
	"on": true, "off": true, "true": true, "false": true,
}

// RenderPhpIni turns a flat directive map into the text of zz-project.ini: keys sorted, one
// `key = value` per line, a trailing newline. Pure: no I/O.
//
// It lives beside RenderLayerDockerfile because the local stack and the Dagger pipeline must
// produce the same file. Directive names are written verbatim — php.ini names are
// case-sensitive and dotted. Booleans become On/Off and numbers are written as-is; strings are
// spelled by renderPhpIniString. Entries are expected to have passed config.Validate; a value
// that has no php.ini spelling is still refused rather than guessed.
func RenderPhpIni(values map[string]any) (string, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out strings.Builder
	for _, key := range keys {
		value, err := renderPhpIniValue(values[key])
		if err != nil {
			return "", fmt.Errorf("php_ini.%s: %w", key, err)
		}
		out.WriteString(key + " = " + value + "\n")
	}
	return out.String(), nil
}

func renderPhpIniValue(value any) (string, error) {
	switch v := value.(type) {
	case bool:
		if v {
			return "On", nil
		}
		return "Off", nil
	case string:
		return renderPhpIniString(v)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(v), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	}
	return "", fmt.Errorf("unsupported value of type %T; use a string, number or boolean", value)
}

// renderPhpIniString spells a string value so PHP reads back what the project meant:
//   - empty → `""`, since a bare `key =` is easy to misread;
//   - a php.ini keyword (none, on, yes, …) → double-quoted, or PHP would turn it into "1"/"";
//   - anything with `$` → single-quoted, php.ini's only literal form: raw and double-quoted
//     values expand ${VAR};
//   - a well-formed expression of constants and numbers (`E_ALL & ~E_DEPRECATED`) → raw, so PHP
//     evaluates it;
//   - special characters, operators outside a valid expression, or surrounding whitespace →
//     double-quoted, with `\` and `"` escaped;
//   - anything else → raw.
func renderPhpIniString(v string) (string, error) {
	if strings.ContainsAny(v, "\r\n") {
		// A quoted value cannot span lines in php.ini, so there is no spelling for it.
		return "", errors.New("a value cannot contain a line break")
	}

	switch {
	case v == "":
		return `""`, nil
	case phpIniKeywords[strings.ToLower(v)]:
		return `"` + v + `"`, nil
	case strings.Contains(v, "$"):
		if strings.Contains(v, "'") {
			return "", errors.New("a value cannot contain both '$' and a single quote")
		}
		return "'" + v + "'", nil
	case isPhpIniExpression(v):
		return v, nil
	case strings.ContainsAny(v, phpIniQuoted+phpIniOperators+"()") || strings.TrimSpace(v) != v:
		// Inside double quotes php.ini reads \" and \\ as escapes, so both are escaped to keep
		// the value byte-for-byte what the project wrote.
		escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v)
		return `"` + escaped + `"`, nil
	}
	return v, nil
}

// isPhpIniExpression reports whether v is a well-formed php.ini expression that uses at least
// one operator, so that writing it raw makes PHP evaluate it rather than fail. A full parse is
// needed, not a character check: `Hello!` uses only expression characters, yet raw it is a
// syntax error that makes PHP drop every directive after it.
//
// The grammar is php.ini's, over the operands this renderer allows:
//
//	expr    = unary { ("|" | "&" | "^") unary }
//	unary   = { "~" | "!" } primary
//	primary = operand | "(" expr ")"
//	operand = ["-" digit] { letter | digit | "_" | "." }   (at least one character)
//
// Surrounding whitespace is not accepted: it would be lost unquoted.
func isPhpIniExpression(v string) bool {
	if strings.TrimSpace(v) != v || !strings.ContainsAny(v, phpIniOperators) {
		return false
	}
	p := phpIniExprParser{s: v}
	if !p.expr() {
		return false
	}
	p.skipSpace()
	return p.pos == len(p.s)
}

type phpIniExprParser struct {
	s   string
	pos int
}

func (p *phpIniExprParser) peek() byte {
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}
	return 0
}

func (p *phpIniExprParser) skipSpace() {
	for p.peek() == ' ' || p.peek() == '\t' {
		p.pos++
	}
}

func (p *phpIniExprParser) expr() bool {
	if !p.unary() {
		return false
	}
	for {
		p.skipSpace()
		switch p.peek() {
		case '|', '&', '^':
			p.pos++
			if !p.unary() {
				return false
			}
		default:
			return true
		}
	}
}

func (p *phpIniExprParser) unary() bool {
	p.skipSpace()
	for p.peek() == '~' || p.peek() == '!' {
		p.pos++
		p.skipSpace()
	}
	return p.primary()
}

func (p *phpIniExprParser) primary() bool {
	p.skipSpace()
	if p.peek() == '(' {
		p.pos++
		if !p.expr() {
			return false
		}
		p.skipSpace()
		if p.peek() != ')' {
			return false
		}
		p.pos++
		return true
	}
	return p.operand()
}

func (p *phpIniExprParser) operand() bool {
	start := p.pos
	// A negative number is one token in php.ini; a lone `-` is not an operator there.
	if p.peek() == '-' {
		if p.pos+1 >= len(p.s) || p.s[p.pos+1] < '0' || p.s[p.pos+1] > '9' {
			return false
		}
		p.pos++
	}
	for {
		c := p.peek()
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '.' {
			p.pos++
			continue
		}
		break
	}
	return p.pos > start
}
