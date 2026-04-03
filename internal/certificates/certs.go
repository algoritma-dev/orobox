// Package certificates provides functions for managing SSL certificates.
package certificates

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/utils"
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

	// 0. Check if mkcert is installed
	certsDirs := filepath.Join(config.GetInternalDir(), "certs")

	if _, err := exec.LookPath("mkcert"); err != nil {
		utils.PrintWarning("mkcert is not installed. SSL certificates will not be generated.")
		utils.PrintInfo("Please install mkcert (https://github.com/FiloSottile/mkcert) and run 'mkcert -install'")
		if runtime.GOOS == "darwin" {
			utils.PrintInfo("On macOS, you can use: brew install mkcert")
		} else if runtime.GOOS == "linux" {
			utils.PrintInfo("On Linux, you can follow: https://github.com/FiloSottile/mkcert#linux")
		}

		// Ensure the directory is created anyway for consistency (e.g., tests or manual generation)
		_ = os.MkdirAll(certsDirs, 0755)
		return
	}

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
