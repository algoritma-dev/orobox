// Package utils provides utility functions.
package utils

import (
	"bufio"
	"io"
	"os"
	"runtime"
	"strings"
)

// CheckHostInEtcHosts checks if a host is present in the /etc/hosts file.
func CheckHostInEtcHosts(host string) bool {
	if host == "localhost" || host == "127.0.0.1" {
		return true
	}

	hostsPath := "/etc/hosts"
	if runtime.GOOS == "windows" {
		hostsPath = os.Getenv("SystemRoot") + `\System32\drivers\etc\hosts`
	}

	file, err := os.Open(hostsPath)
	if err != nil {
		return false
	}
	defer file.Close()

	return CheckHostInReader(host, file)
}

// CheckHostInReader checks if a host is present in a reader following /etc/hosts format.
func CheckHostInReader(host string, r io.Reader) bool {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		// Remove comments
		if idx := strings.Index(line, "#"); idx != -1 {
			line = line[:idx]
		}

		parts := strings.Fields(line)
		// Skip the first part which is the IP
		if len(parts) > 1 {
			for i := 1; i < len(parts); i++ {
				if parts[i] == host {
					return true
				}
			}
		}
	}

	return false
}
