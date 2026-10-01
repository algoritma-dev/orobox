package docker

import (
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

func TestRenderPhpIni(t *testing.T) {
	got, err := RenderPhpIni(map[string]any{
		"b": true, "a": false, "n": -1, "f": 1.5, "s": "Europe/Rome", "q": "a;b", "w": " x",
	})
	if err != nil {
		t.Fatalf("RenderPhpIni failed: %v", err)
	}

	want := "a = Off\n" +
		"b = On\n" +
		"f = 1.5\n" +
		"n = -1\n" +
		"q = \"a;b\"\n" +
		"s = Europe/Rome\n" +
		"w = \" x\"\n"
	if got != want {
		t.Errorf("unexpected ini.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderPhpIniQuotesSpecialChars(t *testing.T) {
	for _, char := range []string{";", "=", `"`, "{", "}", "|", "&", "~", "!", "[", "(", ")", "^"} {
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
