package certificates

import (
	"os"
	"testing"

	"github.com/spf13/viper"
)

func TestInstallSslCertificates_NoConfig(_ *testing.T) {
	viper.Reset()
	// Should not panic or error if config is missing
	InstallSslCertificates()
}

func TestInstallSslCertificates_NoSsl(_ *testing.T) {
	viper.Reset()
	viper.Set("domains", []map[string]any{
		{"host": "localhost", "ssl": false},
	})

	// Should return early and not try to call mkcert
	InstallSslCertificates()
}

func TestInstallSslCertificates_Path(t *testing.T) {
	// Use a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "certs-path-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Mock CI environment to use .orobox
	os.Setenv("OROBOX_LOCAL_CONFIG", "1")
	defer os.Unsetenv("OROBOX_LOCAL_CONFIG")

	viper.Reset()
	viper.Set("domains", []map[string]any{
		{"host": "test.local", "ssl": true},
	})

	// This will still try to call mkcert, which might fail if not installed,
	// but we only care about the directory creation.
	// To avoid actual mkcert calls, we'd need to mock the exec.Command,
	// but let's see if we can at least verify MkdirAll.

	InstallSslCertificates()

	if _, err := os.Stat(".orobox/certs"); os.IsNotExist(err) {
		t.Errorf("Expected directory .orobox/certs to be created")
	}
}
