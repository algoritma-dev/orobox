package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"

	"github.com/spf13/viper"
)

// withTempProject chdirs into a fresh temp dir holding an .orobox internal directory with
// generated env files. OROBOX_LOCAL_CONFIG makes config.GetInternalDir resolve to the
// project-local ".orobox", and the viper reset makes config.GetHostBundlePath fall back to
// the working directory instead of a stale config path left by another test.
func withTempProject(t *testing.T) {
	t.Helper()

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	t.Setenv("OROBOX_LOCAL_CONFIG", "1")
	viper.Reset()

	if err := os.MkdirAll(".orobox", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".orobox", ".env"), []byte("ORO_DB_NAME=oro_db\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".orobox", ".env.test"), []byte("ORO_DB_NAME=oro_db_test\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSeedProjectEnvFilesIsNoOpForBundle(t *testing.T) {
	withTempProject(t)

	strategy, err := config.InstallTypeFor(config.InstallTypeBundle)
	if err != nil {
		t.Fatal(err)
	}
	seedProjectEnvFiles(strategy)

	if _, err := os.Stat(".env-app.local"); !os.IsNotExist(err) {
		t.Error("bundle installs bind-mount the env files, so init must not seed .env-app.local")
	}
	if _, err := os.Stat(".env-app.test"); !os.IsNotExist(err) {
		t.Error("bundle installs bind-mount the env files, so init must not seed .env-app.test")
	}
}

func TestSeedProjectEnvFilesSeedsBothFiles(t *testing.T) {
	withTempProject(t)

	strategy, err := config.InstallTypeFor(config.InstallTypeProject)
	if err != nil {
		t.Fatal(err)
	}
	seedProjectEnvFiles(strategy)

	local, err := os.ReadFile(".env-app.local")
	if err != nil {
		t.Fatalf(".env-app.local was not seeded: %v", err)
	}
	if string(local) != "ORO_DB_NAME=oro_db\n" {
		t.Errorf(".env-app.local = %q, want the internal .env contents", local)
	}

	testEnv, err := os.ReadFile(".env-app.test")
	if err != nil {
		t.Fatalf(".env-app.test was not seeded: %v", err)
	}
	if string(testEnv) != "ORO_DB_NAME=oro_db_test\n" {
		t.Errorf(".env-app.test = %q, want the internal .env.test contents", testEnv)
	}
}

func TestSeedProjectEnvFilesNeverOverwrites(t *testing.T) {
	withTempProject(t)

	if err := os.WriteFile(".env-app.local", []byte("ORO_DB_NAME=my_own_db\n"), 0644); err != nil {
		t.Fatal(err)
	}

	strategy, err := config.InstallTypeFor(config.InstallTypeDemo)
	if err != nil {
		t.Fatal(err)
	}
	seedProjectEnvFiles(strategy)

	local, err := os.ReadFile(".env-app.local")
	if err != nil {
		t.Fatal(err)
	}
	if string(local) != "ORO_DB_NAME=my_own_db\n" {
		t.Errorf(".env-app.local was overwritten with %q; the project owns it after the first init", local)
	}
	if _, err := os.Stat(".env-app.test"); err != nil {
		t.Errorf(".env-app.test was missing and should still have been seeded: %v", err)
	}
}

func TestSeedProjectEnvFilesToleratesMissingInternalFiles(t *testing.T) {
	withTempProject(t)

	if err := os.Remove(filepath.Join(".orobox", ".env")); err != nil {
		t.Fatal(err)
	}

	strategy, err := config.InstallTypeFor(config.InstallTypeProject)
	if err != nil {
		t.Fatal(err)
	}
	seedProjectEnvFiles(strategy)

	if _, err := os.Stat(".env-app.local"); !os.IsNotExist(err) {
		t.Error("a missing internal .env must not produce an empty .env-app.local")
	}
	if _, err := os.Stat(".env-app.test"); err != nil {
		t.Errorf("the remaining internal .env.test should still have been seeded: %v", err)
	}
}
