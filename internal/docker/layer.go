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

// phpIniQuoted lists the characters php.ini treats specially in an unquoted value: comments,
// assignment, quoting, and the operators of its expression syntax. A value containing any of
// them is double-quoted.
const phpIniQuoted = `;="{}|&~![()^`

// RenderPhpIni turns a flat directive map into the text of zz-project.ini: keys sorted, one
// `key = value` per line, a trailing newline. Pure: no I/O.
//
// It lives beside RenderLayerDockerfile because the local stack and the Dagger pipeline must
// produce the same file. Directive names are written verbatim — php.ini names are
// case-sensitive and dotted. Booleans become On/Off, numbers are written as-is, and a string is
// double-quoted when it holds a character php.ini would otherwise interpret (or surrounding
// whitespace, which an unquoted value loses). Entries are expected to have passed
// config.Validate; a value that has no php.ini spelling is still refused rather than guessed.
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
		if strings.ContainsAny(v, "\r\n") {
			// A quoted value cannot span lines in php.ini, so there is no spelling for it.
			return "", errors.New("a value cannot contain a line break")
		}
		if v == "" || strings.ContainsAny(v, phpIniQuoted) || strings.TrimSpace(v) != v {
			// Inside double quotes php.ini reads \" and \\ as escapes, so both are escaped
			// to keep the value byte-for-byte what the project wrote.
			escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v)
			return `"` + escaped + `"`, nil
		}
		return v, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(v), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	}
	return "", fmt.Errorf("unsupported value of type %T; use a string, number or boolean", value)
}
