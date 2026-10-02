package docker

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestMergeEnvKeepsTheTemplateAndAppendsTheProject(t *testing.T) {
	got := string(MergeEnv([]byte("# c\nA=1\nB=2\n"), []byte("B=3\n"), ".env"))
	want := "# c\nA=1\nB=2\n\n# From .env\nB=3\n"
	if got != want {
		t.Errorf("MergeEnv() = %q, want %q", got, want)
	}
	if v := resolvedEnv(got); v["B"] != "3" {
		t.Errorf("B = %q, want 3", v["B"])
	}
}

func TestMergeEnvLeavesTemplateLinesAsWritten(t *testing.T) {
	template := "# head\n\nA=\"${B}/x\"   \nB=2\n# tail\nA=dup\n"
	got := string(MergeEnv([]byte(template), []byte("A=9\n"), ".env"))
	if !strings.HasPrefix(got, template) {
		t.Errorf("template part changed: %q", got)
	}
	if v := resolvedEnv(got); v["A"] != "9" {
		t.Errorf("A = %q, want the project's 9", v["A"])
	}
}

func TestMergeEnvAppendsNewKeys(t *testing.T) {
	got := string(MergeEnv([]byte("# c\nA=1\n"), []byte("Z=9\n"), ".env"))
	want := "# c\nA=1\n\n# From .env\nZ=9\n"
	if got != want {
		t.Errorf("MergeEnv() = %q, want %q", got, want)
	}
}

func TestMergeEnvAppendsAfterTemplateWithoutTrailingNewline(t *testing.T) {
	got := string(MergeEnv([]byte("A=1"), []byte("Z=9\n"), ".env.test"))
	want := "A=1\n\n# From .env.test\nZ=9\n"
	if got != want {
		t.Errorf("MergeEnv() = %q, want %q", got, want)
	}
}

func TestMergeEnvKeepsReferencesVerbatim(t *testing.T) {
	got := string(MergeEnv([]byte("A=1\nX=old\n"), []byte("X=\"${A}/x\"\nY='${A}'\n"), ".env"))
	want := "A=1\nX=old\n\n# From .env\nX=\"${A}/x\"\nY='${A}'\n"
	if got != want {
		t.Errorf("MergeEnv() = %q, want %q", got, want)
	}
}

func TestMergeEnvTolerantParsing(t *testing.T) {
	project := "export K=1\r\nQ=\"a b\"\r\nE=\r\n# note\r\n\r\nnot an assignment\r\n"
	got := string(MergeEnv([]byte("A=1\n"), []byte(project), ".env"))
	want := "A=1\n\n# From .env\nK=1\nQ=\"a b\"\nE=\n"
	if got != want {
		t.Errorf("MergeEnv() = %q, want %q", got, want)
	}
}

// Every project assignment is appended in the project's own order, so the last one of a key is
// also the last line Dotenv and compose read for it.
func TestMergeEnvLastAssignmentWins(t *testing.T) {
	got := string(MergeEnv([]byte("A=1\n"), []byte("A=2\nN=1\nA=3\nN=2\n"), ".env"))
	want := "A=1\n\n# From .env\nA=2\nN=1\nA=3\nN=2\n"
	if got != want {
		t.Errorf("MergeEnv() = %q, want %q", got, want)
	}
	if v := effectiveEnv(t, got); v["A"] != "3" || v["N"] != "2" {
		t.Errorf("effective values = %v, want A=3 N=2", v)
	}
}

func TestMergeEnvValueKeepsEqualsSignsAndSpaces(t *testing.T) {
	got := string(MergeEnv([]byte("A=1\n"), []byte("  A = x=y \n"), ".env"))
	want := "A=1\n\n# From .env\nA= x=y \n"
	if got != want {
		t.Errorf("MergeEnv() = %q, want %q", got, want)
	}
}

func TestMergeEnvWithoutAssignmentsReturnsTemplate(t *testing.T) {
	template := "# c\nA=1\n"
	got := string(MergeEnv([]byte(template), []byte("# only a comment\n\n"), ".env"))
	if got != template {
		t.Errorf("MergeEnv() = %q, want %q", got, template)
	}
}

// A project .env that is a full copy of the template (the old way of customizing) still ends
// up with the template's effective values.
func TestMergeEnvFullCopyKeepsEffectiveValues(t *testing.T) {
	template := "# c\nA=1\nB=\"${A}/b\"\n\n# more\nC=3\n"
	got := string(MergeEnv([]byte(template), []byte(template), ".env"))
	if !strings.HasPrefix(got, template) {
		t.Errorf("template part changed: %q", got)
	}
	want := effectiveEnv(t, template)
	if v := effectiveEnv(t, got); !reflect.DeepEqual(v, want) {
		t.Errorf("effective values = %v, want %v", v, want)
	}
}

// What Dotenv and compose read must be what the project meant: its own keys resolve against each
// other, and the generated keys that reference a key the project changed see the project's
// value. Asserted on the resolved values, not the text.
func TestMergeEnvResolvesReferencesBothWays(t *testing.T) {
	template := "ORO_APP_DOMAIN=localhost\nORO_APP_URL=http://${ORO_APP_DOMAIN}/\n" +
		"ORO_DB_PASSWORD=pass\nORO_DB_DSN=postgres://u:${ORO_DB_PASSWORD}@db/oro\n"
	project := "MY_HOST=shop.test\nORO_APP_DOMAIN=${MY_HOST}\nDB_PASS=secret\nORO_DB_PASSWORD=${DB_PASS}\n"
	v := resolvedEnv(string(MergeEnv([]byte(template), []byte(project), ".env")))

	for key, want := range map[string]string{
		"ORO_APP_DOMAIN":  "shop.test",
		"ORO_APP_URL":     "http://shop.test/",
		"ORO_DB_PASSWORD": "secret",
		"ORO_DB_DSN":      "postgres://u:secret@db/oro",
	} {
		if v[key] != want {
			t.Errorf("%s = %q, want %q", key, v[key], want)
		}
	}
}

// A project value extending the generated one (`${KEY} --more`) extends it once.
func TestMergeEnvSelfReferenceExtendsTheTemplateValueOnce(t *testing.T) {
	template := "ORO_INSTALL_OPTIONS=--a\n"
	project := "ORO_INSTALL_OPTIONS=\"${ORO_INSTALL_OPTIONS} --timeout=0\"\n"
	v := resolvedEnv(string(MergeEnv([]byte(template), []byte(project), ".env")))
	if v["ORO_INSTALL_OPTIONS"] != "--a --timeout=0" {
		t.Errorf("ORO_INSTALL_OPTIONS = %q, want %q", v["ORO_INSTALL_OPTIONS"], "--a --timeout=0")
	}
}

// resolvedEnv reads a dotenv file the way Symfony Dotenv and compose do for these cases: in order,
// the last assignment of a key winning, ${VAR} / $VAR expanded against what was read so far
// (not inside single quotes), surrounding quotes removed.
func resolvedEnv(content string) map[string]string {
	values := map[string]string{}
	ref := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
	for _, e := range parseEnvEntries(content) {
		if !e.assignment {
			continue
		}
		v := strings.TrimSpace(e.value)
		if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
			values[e.key] = v[1 : len(v)-1]
			continue
		}
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = v[1 : len(v)-1]
		}
		values[e.key] = ref.ReplaceAllStringFunc(v, func(m string) string {
			name := strings.Trim(m, "${}")
			return values[name]
		})
	}
	return values
}

// A quoted value may span lines. Its continuation lines are part of the value, never
// assignments of their own, in the template as well as in the project file.
func TestMergeEnvMultiLineQuotedValues(t *testing.T) {
	template := "KEY=\"-----BEGIN\nA=not-a-key\n-----END\"\nA=1\nS='one\ntwo'\n"
	project := "A=2\nKEY=\"new\nB=inside \\\" quote\nend\"\nC='x\ny'\n"
	got := string(MergeEnv([]byte(template), []byte(project), ".env"))

	want := template +
		"\n# From .env\nA=2\nKEY=\"new\nB=inside \\\" quote\nend\"\nC='x\ny'\n"
	if got != want {
		t.Errorf("MergeEnv() =\n%s\nwant\n%s", got, want)
	}
}

func TestParseEnvAssignmentsMultiLine(t *testing.T) {
	keys, values := parseEnvAssignments([]byte("A=\"x\nB=y\"\nC='1\n2'\nD=\"closed\" # c\nE=\"\\\"\"\nF=1\n"))
	if want := []string{"A", "C", "D", "E", "F"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
	if values["A"] != "\"x\nB=y\"" || values["C"] != "'1\n2'" || values["E"] != `"\""` || values["F"] != "1" {
		t.Errorf("values = %q", values)
	}
}

// effectiveEnv reads merged env text the way Dotenv and compose do for these tests: the last
// assignment of a key wins.
func effectiveEnv(t *testing.T, content string) map[string]string {
	t.Helper()
	_, values := parseEnvAssignments([]byte(content))
	return values
}

// Without a loaded config writeEnvFile reads the project file from the current directory, so
// these tests run inside a scratch project directory next to a scratch internal directory.
func TestWriteEnvFileMergesProjectFileOverTemplate(t *testing.T) {
	projectDir := t.TempDir()
	internalDir := t.TempDir()
	t.Chdir(projectDir)

	if err := os.WriteFile(".env", []byte("# mine\nORO_VERSION=9.9\nMY_KEY=xyz\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if !writeEnvFile("templates/docker/.env", internalDir, struct{ OroVersion string }{"6.1"}) {
		t.Fatal("writeEnvFile reported no change on first write")
	}

	got, err := os.ReadFile(filepath.Join(internalDir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	want := "ORO_VERSION=6.1\n\n# From .env\nORO_VERSION=9.9\nMY_KEY=xyz\n"
	if string(got) != want {
		t.Errorf("merged .env = %q, want %q", got, want)
	}

	if writeEnvFile("templates/docker/.env", internalDir, struct{ OroVersion string }{"6.1"}) {
		t.Error("writeEnvFile reported a change although the merged content is identical")
	}
}

func TestWriteEnvFileKeepsTemplateKeysTheProjectFileLacks(t *testing.T) {
	projectDir := t.TempDir()
	internalDir := t.TempDir()
	t.Chdir(projectDir)

	// A project .env written against an older Orobox: it does not know about ORO_VERSION.
	if err := os.WriteFile(".env.test", []byte("MY_KEY=xyz\n"), 0644); err != nil {
		t.Fatal(err)
	}

	writeEnvFile("templates/docker/.env.test", internalDir, struct{ OroVersion string }{"6.1"})

	got, err := os.ReadFile(filepath.Join(internalDir, ".env.test"))
	if err != nil {
		t.Fatal(err)
	}
	want := "ORO_VERSION=6.1\n\n# From .env.test\nMY_KEY=xyz\n"
	if string(got) != want {
		t.Errorf("merged .env.test = %q, want %q", got, want)
	}
}

func TestWriteEnvFileRendersTemplateWithoutProjectFile(t *testing.T) {
	t.Chdir(t.TempDir())
	internalDir := t.TempDir()

	writeEnvFile("templates/docker/.env", internalDir, struct{ OroVersion string }{"6.1"})

	got, err := os.ReadFile(filepath.Join(internalDir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ORO_VERSION=6.1\n" {
		t.Errorf(".env = %q, want the plain rendered template", got)
	}
}

// The project .env sits next to .orobox.yaml, which is not necessarily the working directory
// (a command run from a subdirectory finds the config further up). It has to be read from there.
func TestWriteEnvFileReadsProjectFileNextToConfig(t *testing.T) {
	projectDir := t.TempDir()
	internalDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".orobox.yaml"), []byte("type: bundle\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("MY_KEY=xyz\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// A different working directory with a decoy .env that must not be used.
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte("DECOY=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(filepath.Join(projectDir, ".orobox.yaml"))

	writeEnvFile("templates/docker/.env", internalDir, struct{ OroVersion string }{"6.1"})

	got, err := os.ReadFile(filepath.Join(internalDir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "ORO_VERSION=6.1\n\n# From .env\nMY_KEY=xyz\n"; string(got) != want {
		t.Errorf("merged .env = %q, want %q", got, want)
	}
}
