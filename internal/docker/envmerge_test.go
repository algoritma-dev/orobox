package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeEnvReplacesInPlace(t *testing.T) {
	got := string(MergeEnv([]byte("# c\nA=1\nB=2\n"), []byte("B=3\n"), ".env"))
	want := "# c\nA=1\nB=3\n"
	if got != want {
		t.Errorf("MergeEnv() = %q, want %q", got, want)
	}
}

func TestMergeEnvReplacesOnlyTheFirstOccurrenceAndKeepsOtherLines(t *testing.T) {
	template := "# head\n\nA=\"${B}/x\"   \nB=2\n# tail\nA=dup\n"
	got := string(MergeEnv([]byte(template), []byte("A=9\n"), ".env"))
	want := "# head\n\nA=9\nB=2\n# tail\nA=dup\n"
	if got != want {
		t.Errorf("MergeEnv() = %q, want %q", got, want)
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
	want := "A=1\nX=\"${A}/x\"\n\n# From .env\nY='${A}'\n"
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

func TestMergeEnvLastAssignmentWins(t *testing.T) {
	got := string(MergeEnv([]byte("A=1\n"), []byte("A=2\nN=1\nA=3\nN=2\n"), ".env"))
	want := "A=3\n\n# From .env\nN=2\n"
	if got != want {
		t.Errorf("MergeEnv() = %q, want %q", got, want)
	}
}

func TestMergeEnvValueKeepsEqualsSignsAndSpaces(t *testing.T) {
	got := string(MergeEnv([]byte("A=1\n"), []byte("  A = x=y \n"), ".env"))
	want := "A= x=y \n"
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

func TestMergeEnvFullCopyIsIdentity(t *testing.T) {
	template := "# c\nA=1\nB=\"${A}/b\"\n\n# more\nC=3\n"
	got := string(MergeEnv([]byte(template), []byte(template), ".env"))
	if got != template {
		t.Errorf("MergeEnv() = %q, want %q", got, template)
	}
}

// writeEnvFile reads the project file from the current directory, so these tests run inside
// a scratch project directory next to a scratch internal directory.
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
	want := "ORO_VERSION=9.9\n\n# From .env\nMY_KEY=xyz\n"
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
