package certificates

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"orobox/internal/config"
)

func InstallSslCertificates() {
	configPath := ".orobox.yaml"
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	var oroConfig config.OroConfig
	err = yaml.Unmarshal(data, &oroConfig)
	if err != nil {
		fmt.Printf("Warning: error parsing %s: %v\n", configPath, err)
		return
	}

	hasSsl := false
	for _, domain := range oroConfig.Domains {
		if domain.Ssl {
			hasSsl = true
			break
		}
	}

	if !hasSsl {
		return
	}

	fmt.Println("Installing CA certificates...")
	cmd := exec.Command("mkcert", "-install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()

	certsDirs := filepath.Join(config.GetInternalDir(), "certs")

	err = os.MkdirAll(certsDirs, 0755)
	if err != nil {
		fmt.Printf("Warning: could not create certs directory: %v\n", err)
		return
	}

	for _, domain := range oroConfig.Domains {
		if !domain.Ssl {
			continue
		}

		certFile := filepath.Join(certsDirs, domain.Host+".pem")
		keyFile := filepath.Join(certsDirs, domain.Host+"-key.pem")

		if _, err := os.Stat(certFile); err == nil {
			if _, err := os.Stat(keyFile); err == nil {
				fmt.Printf("Certificates for %s already exist, skipping...\n", domain.Host)
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
