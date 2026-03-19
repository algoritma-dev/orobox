package certificates

import (
	"os"
	"testing"
)

func TestInstallSslCertificates_NoConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "certs-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Should not panic or error if config is missing
	InstallSslCertificates()
}

func TestInstallSslCertificates_NoSsl(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "certs-test-no-ssl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	configData := `
domains:
  - host: localhost
    ssl: false
`
	err = os.WriteFile(".orobox.yaml", []byte(configData), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Should return early and not try to call mkcert
	InstallSslCertificates()
}
