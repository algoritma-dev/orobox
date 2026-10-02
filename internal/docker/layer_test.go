package docker

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
)

func TestRenderLayerDeclarativeOnly(t *testing.T) {
	got := RenderLayerDockerfile(nil, config.ImageConfig{
		Apk:           []string{"a", "b"},
		PhpExtensions: []string{"redis"},
		Npm:           []string{"x"},
		Run:           []string{"echo 1", "echo 2"},
	})

	want := "ARG OROBOX_BASE_IMAGE\n" +
		"FROM ${OROBOX_BASE_IMAGE}\n" +
		"USER root\n" +
		"RUN apk add --no-cache a b\n" +
		installPhpExtensionsBootstrap + "\n" +
		"RUN install-php-extensions redis\n" +
		"RUN npm install -g x\n" +
		"RUN echo 1\n" +
		"RUN echo 2\n"

	if string(got) != want {
		t.Errorf("unexpected Dockerfile.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderLayerAppendsToProjectDockerfile(t *testing.T) {
	project := "ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}\nRUN true"

	got := RenderLayerDockerfile([]byte(project), config.ImageConfig{Apk: []string{"a"}})

	// The header must not be repeated: the project Dockerfile already declares it.
	want := project + "\nUSER root\nRUN apk add --no-cache a\n"
	if string(got) != want {
		t.Errorf("unexpected Dockerfile.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderLayerAppendsAfterProjectTrailingNewline(t *testing.T) {
	project := "ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}\nRUN true\n"

	got := RenderLayerDockerfile([]byte(project), config.ImageConfig{Npm: []string{"x"}})

	// No blank line is inserted when the project file already ends with a newline.
	want := project + "USER root\nRUN npm install -g x\n"
	if string(got) != want {
		t.Errorf("unexpected Dockerfile.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderLayerProjectOnly(t *testing.T) {
	project := []byte("ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}\nRUN true")

	got := RenderLayerDockerfile(project, config.ImageConfig{})

	if string(got) != string(project) {
		t.Errorf("project Dockerfile must pass through unchanged.\ngot:\n%s\nwant:\n%s", got, project)
	}
}

func TestRenderLayerSkipsEmptyKeys(t *testing.T) {
	got := string(RenderLayerDockerfile(nil, config.ImageConfig{Npm: []string{"x"}}))

	want := "ARG OROBOX_BASE_IMAGE\nFROM ${OROBOX_BASE_IMAGE}\nUSER root\nRUN npm install -g x\n"
	if got != want {
		t.Errorf("unexpected Dockerfile.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// A base image published before the installer was shipped in the final stage has no
// install-php-extensions, so the layer fetches the pinned release when the command is missing.
func TestRenderLayerBootstrapsExtensionInstaller(t *testing.T) {
	got := string(RenderLayerDockerfile(nil, config.ImageConfig{PhpExtensions: []string{"redis"}}))

	want := "RUN command -v install-php-extensions >/dev/null 2>&1 || " +
		"(curl -fsSL https://github.com/mlocati/docker-php-extension-installer/releases/download/" +
		InstallPhpExtensionsVersion + "/install-php-extensions -o /usr/local/bin/install-php-extensions" +
		" && chmod +x /usr/local/bin/install-php-extensions)\n" +
		"RUN install-php-extensions redis\n"
	if !strings.Contains(got, want) {
		t.Errorf("layer does not bootstrap the installer before using it:\n%s", got)
	}

	// Without extensions there is nothing to install, so nothing is downloaded.
	if got := string(RenderLayerDockerfile(nil, config.ImageConfig{Apk: []string{"a"}})); strings.Contains(got, "install-php-extensions") {
		t.Errorf("layer without php_extensions mentions the installer:\n%s", got)
	}
}

// The base image and the layer bootstrap must fetch the same installer release.
func TestInstallPhpExtensionsVersionMatchesDockerfile(t *testing.T) {
	content, err := os.ReadFile("../../templates/docker/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^ARG INSTALL_PHP_EXTENSIONS_VERSION=(\S+)$`).FindSubmatch(content)
	if m == nil {
		t.Fatal("templates/docker/Dockerfile declares no ARG INSTALL_PHP_EXTENSIONS_VERSION default")
	}
	if string(m[1]) != InstallPhpExtensionsVersion {
		t.Errorf("Dockerfile pins %s, InstallPhpExtensionsVersion is %s", m[1], InstallPhpExtensionsVersion)
	}
}

func TestRenderPhpIni(t *testing.T) {
	got, err := RenderPhpIni(map[string]any{
		"b": true, "a": false, "n": -1, "f": 1.5, "s": "Europe/Rome", "q": "a;b", "w": " x", "e": "E_ALL & ~E_DEPRECATED",
	})
	if err != nil {
		t.Fatalf("RenderPhpIni failed: %v", err)
	}

	want := "a = Off\n" +
		"b = On\n" +
		"e = E_ALL & ~E_DEPRECATED\n" +
		"f = 1.5\n" +
		"n = -1\n" +
		"q = \"a;b\"\n" +
		"s = Europe/Rome\n" +
		"w = \" x\"\n"
	if got != want {
		t.Errorf("unexpected ini.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// These characters mean something to php.ini in an unquoted value (comments, assignment,
// quoting, sections, variables); a value holding one is double-quoted. `'` is among them because
// an unquoted `it's` opens a raw string that never closes, and PHP then drops every directive
// after it (verified against php:8.2-cli).
func TestRenderPhpIniQuotesSpecialChars(t *testing.T) {
	for _, char := range []string{";", "=", `"`, "{", "}", "[", "]", "#", "'"} {
		got, err := RenderPhpIni(map[string]any{"k": "a" + char + "b"})
		if err != nil {
			t.Fatalf("RenderPhpIni failed for %q: %v", char, err)
		}
		escaped := strings.ReplaceAll(char, `"`, `\"`)
		if want := "k = \"a" + escaped + "b\"\n"; got != want {
			t.Errorf("value with %q: got %q, want %q", char, got, want)
		}
	}
}

// A well-formed php.ini expression of constants and numbers is written raw so PHP evaluates it:
// quoted, `E_ALL & ~E_DEPRECATED` would be a literal string and error_reporting would read it as 0.
func TestRenderPhpIniWritesExpressionsRaw(t *testing.T) {
	for _, value := range []string{
		"E_ALL & ~E_DEPRECATED",
		"E_ALL & ~E_DEPRECATED & ~E_STRICT",
		"E_ALL&~E_NOTICE",
		"1|2",
		"E_A^E_B",
		"!(1|0)",
		"~ ~E_ALL",
		"-1 & 3",
		"1 | (2 & 3)",
	} {
		got, err := RenderPhpIni(map[string]any{"k": value})
		if err != nil {
			t.Fatalf("RenderPhpIni failed for %q: %v", value, err)
		}
		if want := "k = " + value + "\n"; got != want {
			t.Errorf("expression %q: got %q, want %q", value, got, want)
		}
	}
}

// Only constants and numbers are expression operands. A lowercase word joined by an operator is a
// literal string (`dev&test`, an Xdebug trigger value) that PHP would silently evaluate to 0, and
// a php.ini keyword as an operand is a syntax error that drops every later directive.
func TestRenderPhpIniQuotesNonConstantOperands(t *testing.T) {
	for _, value := range []string{"dev&test", "a.b|c.d", "x^y", "on|1", "Yes|1", "null|1", "TRUE|1", "off&on"} {
		got, err := RenderPhpIni(map[string]any{"k": value})
		if err != nil {
			t.Fatalf("RenderPhpIni(%q): %v", value, err)
		}
		if want := "k = \"" + value + "\"\n"; got != want {
			t.Errorf("%q: got %q, want %q", value, got, want)
		}
	}
}

// Raw, a value with several words is a syntax error as soon as one of them is a php.ini keyword
// (`msmtp -a default --read-envelope-from on`), and PHP then drops every directive after it.
// Quoted, a multi-word value reads back exactly as written.
func TestRenderPhpIniQuotesMultiWordValues(t *testing.T) {
	for _, value := range []string{"msmtp -a default --read-envelope-from on", "Yes please", "foo none", "1 on"} {
		got, err := RenderPhpIni(map[string]any{"k": value})
		if err != nil {
			t.Fatalf("RenderPhpIni(%q): %v", value, err)
		}
		if want := "k = \"" + value + "\"\n"; got != want {
			t.Errorf("%q: got %q, want %q", value, got, want)
		}
	}
}

// Operator characters outside a well-formed expression are a php.ini syntax error when written
// raw (`Hello!` stops the parser at the `!` and every later directive is lost), so such a value
// is quoted and stays the literal string the project wrote.
func TestRenderPhpIniQuotesMalformedExpressions(t *testing.T) {
	for _, value := range []string{"Hello!", "a!b", "a~b", "foo(bar)", "a)b", "(", "~", "a |", "| a", "1 & - 3", " E_ALL & ~E_NOTICE"} {
		got, err := RenderPhpIni(map[string]any{"k": value})
		if err != nil {
			t.Fatalf("RenderPhpIni failed for %q: %v", value, err)
		}
		if want := "k = \"" + value + "\"\n"; got != want {
			t.Errorf("value %q: got %q, want %q", value, got, want)
		}
	}
}

// Unquoted, php.ini turns these keywords into "" or "1"; `session.cookie_samesite: None` would
// silently become an empty setting. Written as a YAML string, the project meant the word.
func TestRenderPhpIniQuotesKeywordStrings(t *testing.T) {
	for _, value := range []string{"None", "null", "YES", "no", "On", "off", "true", "False"} {
		got, err := RenderPhpIni(map[string]any{"k": value})
		if err != nil {
			t.Fatal(err)
		}
		if want := "k = \"" + value + "\"\n"; got != want {
			t.Errorf("keyword %q: got %q, want %q", value, got, want)
		}
	}
	got, err := RenderPhpIni(map[string]any{"session.cookie_samesite": "None"})
	if err != nil || got != "session.cookie_samesite = \"None\"\n" {
		t.Errorf("got %q, %v", got, err)
	}
	// A YAML boolean is still a php.ini boolean.
	if got, _ := RenderPhpIni(map[string]any{"k": true}); got != "k = On\n" {
		t.Errorf("bool true: got %q", got)
	}
}

// php.ini expands ${VAR} in raw and double-quoted values; single quotes are its only literal
// form, so a value holding `$` is single-quoted.
func TestRenderPhpIniSingleQuotesDollarValues(t *testing.T) {
	got, err := RenderPhpIni(map[string]any{"x": "a${HOME}", "y": "a$b;c"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "x = 'a${HOME}'\ny = 'a$b;c'\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A single-quoted php.ini string has no escapes, so `$` and `'` together have no spelling.
	if _, err := RenderPhpIni(map[string]any{"x": "a$b'c"}); err == nil {
		t.Error("expected a value with both $ and ' to be rejected")
	}
}

func TestRenderPhpIniKeepsDirectiveNamesVerbatim(t *testing.T) {
	got, err := RenderPhpIni(map[string]any{"xdebug.log_level": 0, "Memory_Limit": "4G"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "Memory_Limit = 4G\nxdebug.log_level = 0\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderPhpIniEmpty(t *testing.T) {
	got, err := RenderPhpIni(nil)
	if err != nil || got != "" {
		t.Errorf("expected an empty file, got %q, %v", got, err)
	}
}

// A value that is not a scalar has no php.ini spelling; Validate rejects it earlier, but the
// renderer is also reachable from the pipeline, so it must not guess.
func TestRenderPhpIniRejectsNonScalars(t *testing.T) {
	if _, err := RenderPhpIni(map[string]any{"k": []any{"a"}}); err == nil {
		t.Error("expected a list value to be rejected")
	}
	if _, err := RenderPhpIni(map[string]any{"k": nil}); err == nil {
		t.Error("expected a null value to be rejected")
	}
}

// Inside double quotes php.ini reads \\ and \" as escapes, so a quoted value has to escape its
// backslashes too or a trailing one would swallow the closing quote.
func TestRenderPhpIniEscapesBackslashesInQuotedValues(t *testing.T) {
	got, err := RenderPhpIni(map[string]any{"a": `C:\tmp\;x\`, "b": `C:\tmp`})
	if err != nil {
		t.Fatal(err)
	}
	if want := `a = "C:\\tmp\\;x\\"` + "\n" + `b = C:\tmp` + "\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderPhpIniQuotesEmptyAndRejectsLineBreaks(t *testing.T) {
	got, err := RenderPhpIni(map[string]any{"error_log": ""})
	if err != nil || got != "error_log = \"\"\n" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := RenderPhpIni(map[string]any{"k": "a\nb"}); err == nil {
		t.Error("expected a multi-line value to be rejected")
	}
}
