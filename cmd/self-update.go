package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

var selfUpdateCmd = &cobra.Command{
	Use:   "self-update",
	Short: "Update orobox to the latest version",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Printf("Current version: %s\n", Version)
		fmt.Println("Checking for updates...")

		latest, err := getLatestRelease()
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		if latest.TagName == Version {
			fmt.Println("You are already using the latest version.")
			return nil
		}

		fmt.Printf("New version available: %s\n", latest.TagName)

		assetURL, assetName := findBestAsset(latest)
		if assetURL == "" {
			return fmt.Errorf("no suitable binary found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, latest.TagName)
		}

		fmt.Printf("Downloading %s...\n", assetName)
		if err := applyUpdate(assetURL); err != nil {
			return fmt.Errorf("failed to apply update: %w", err)
		}

		fmt.Printf("Successfully updated to %s\n", latest.TagName)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(selfUpdateCmd)
}

func getLatestRelease() (*release, error) {
	resp, err := httpClient.Get("https://api.github.com/repos/algoritma-dev/orobox/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status: %s", resp.Status)
	}

	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}

	return &r, nil
}

func findBestAsset(r *release) (url, name string) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Map architecture aliases
	archs := []string{goarch}
	if goarch == "amd64" {
		archs = append(archs, "x86_64")
	} else if goarch == "arm64" {
		archs = append(archs, "aarch64")
	}

	// Look for an asset that contains both the OS and a matching architecture in its name.
	for _, asset := range r.Assets {
		nameLower := strings.ToLower(asset.Name)

		// Skip archives and checksum files to ensure we get the raw binary
		if strings.HasSuffix(nameLower, ".tar.gz") ||
			strings.HasSuffix(nameLower, ".zip") ||
			strings.HasSuffix(nameLower, ".tgz") ||
			strings.HasSuffix(nameLower, ".sha256") ||
			strings.HasSuffix(nameLower, ".sig") {
			continue
		}

		if !strings.Contains(nameLower, goos) {
			continue
		}

		matchedArch := false
		for _, arch := range archs {
			if strings.Contains(nameLower, arch) {
				matchedArch = true
				break
			}
		}

		if matchedArch {
			return asset.BrowserDownloadURL, asset.Name
		}
	}
	return "", ""
}

func applyUpdate(url string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download update: %s", resp.Status)
	}

	executablePath, err := os.Executable()
	if err != nil {
		return err
	}

	// Resolve symlinks to find the real path of the binary.
	if evalPath, err := filepath.EvalSymlinks(executablePath); err == nil {
		executablePath = evalPath
	}

	// Create a temporary file in the same directory as the executable to ensure
	// os.Rename works (it often fails across different filesystems).
	tmpFile := filepath.Join(filepath.Dir(executablePath), "."+filepath.Base(executablePath)+".tmp")
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w (try running with sudo?)", err)
	}
	defer f.Close()
	defer os.Remove(tmpFile)

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("failed to write to temporary file: %w", err)
	}
	f.Close()

	// Replace the old binary with the new one.
	// On Unix-like systems, we can rename over a running executable.
	if err := os.Rename(tmpFile, executablePath); err != nil {
		return fmt.Errorf("failed to replace binary: %w (try running with sudo?)", err)
	}

	return nil
}
