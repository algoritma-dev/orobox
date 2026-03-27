// Package certificates provides functions for managing SSL certificates.
package certificates

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/algoritma-dev/orobox/internal/config"
)

// InstallSslCertificates installs the SSL certificates for the configured domains.
func InstallSslCertificates() {
	domains := config.GetDomains()

	hasSsl := false
	for _, domain := range domains {
		if domain.Ssl {
			hasSsl = true
			break
		}
	}

	if !hasSsl {
		return
	}

	certsDirs := filepath.Join(config.GetInternalDir(), "certs")

	err := os.MkdirAll(certsDirs, 0755)
	if err != nil {
		fmt.Printf("Warning: could not create certs directory: %v\n", err)
		return
	}

	// 1. Ensure mkcert CA is installed (silent)
	_ = exec.Command("mkcert", "-install").Run()

	// 2. Generate certificates for each domain if missing
	for _, domain := range domains {
		if !domain.Ssl {
			continue
		}

		certFile := filepath.Join(certsDirs, domain.Host+".pem")
		keyFile := filepath.Join(certsDirs, domain.Host+"-key.pem")

		if _, err := os.Stat(certFile); err == nil {
			if _, err := os.Stat(keyFile); err == nil {
				// Certs already exist, skip silently
				continue
			}
		}

		fmt.Printf("Generating certificates for %s...\n", domain.Host)
		cmd := exec.Command("mkcert", "-cert-file", certFile, "-key-file", keyFile, domain.Host)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			fmt.Printf("Warning: could not generate certificates for %s: %v\n", domain.Host, err)
		}
	}
}
