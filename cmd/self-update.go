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

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
		utils.PrintInfo(fmt.Sprintf("Current version: %s", Version))
		utils.StartLoader("Checking for updates...")
		latest, err := getLatestRelease()
		utils.StopLoader()
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		if latest.TagName != Version {
			utils.PrintSuccess(fmt.Sprintf("New version available: %s", latest.TagName))

			assetURL, assetName := findBestAsset(latest)
			if assetURL == "" {
				return fmt.Errorf("no suitable binary found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, latest.TagName)
			}

			utils.StartLoader(fmt.Sprintf("Downloading %s...", assetName))
			if err := applyUpdate(assetURL); err != nil {
				utils.StopLoader()
				return fmt.Errorf("failed to apply update: %w", err)
			}
			utils.StopLoader()
			utils.PrintSuccess(fmt.Sprintf("Successfully updated to %s", latest.TagName))
		} else {
			utils.PrintSuccess("You are already using the latest version of orobox.")

			return nil
		}

		// Pull latest Docker images
		utils.PrintInfo("Checking for Docker image updates...")

		anyUpdated := false
		// Update all local orobox images
		updatedLocal, err := docker.PullAllLocalOrobotImages()
		if err != nil {
			utils.PrintWarning(fmt.Sprintf("Failed to pull local orobox images: %v", err))
		}
		if updatedLocal {
			anyUpdated = true
		}

		// Update project-specific services if we are in a project
		if viper.ConfigFileUsed() != "" {
			docker.EnsureDockerCompose()
			updatedProject, err := docker.PullProjectImages()
			if err != nil {
				utils.PrintWarning(fmt.Sprintf("Failed to pull project images: %v", err))
			}
			if updatedProject {
				anyUpdated = true
			}
		}

		if anyUpdated {
			utils.PrintSuccess("Docker images updated successfully.")
		} else {
			utils.PrintInfo("Docker images are already up to date.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(selfUpdateCmd)
}

func getLatestRelease() (*release, error) {
	return getLatestReleaseWith(httpClient)
}

// getLatestReleaseWith takes the client as an argument so a caller with different patience can
// supply its own: the update notice runs beside the user's command and cannot wait 30 seconds.
func getLatestReleaseWith(client *http.Client) (*release, error) {
	resp, err := client.Get("https://api.github.com/repos/algoritma-dev/orobox/releases/latest")
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

// binaryAssetExtensions lists the only extensions a raw, directly executable release asset may
// carry. The check is an allowlist rather than a list of things to skip on purpose: the release
// also publishes .deb/.rpm/.apk packages (the nfpms block in .goreleaser.yaml), and a skip list
// silently accepts every format added there later. That is how orobox_1.0.0_linux_amd64.apk —
// a gzip stream whose name contains both "linux" and "amd64" — was once written over the
// installed binary, leaving "exec format error" on the next run.
var binaryAssetExtensions = map[string]bool{
	"":     true,
	".exe": true,
}

// expectedBinaryName mirrors the `binaries` archive name_template in .goreleaser.yaml.
func expectedBinaryName(goos, goarch string) string {
	arch := goarch
	switch goarch {
	case "amd64":
		arch = "x86_64"
	case "386":
		arch = "i386"
	}

	name := fmt.Sprintf("orobox_%s_%s", strings.ToUpper(goos[:1])+goos[1:], arch)
	if goos == "windows" {
		name += ".exe"
	}

	return name
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

	// Prefer the asset GoReleaser publishes for this platform by its exact name, so asset
	// ordering in the GitHub API response cannot decide which file we install.
	expected := strings.ToLower(expectedBinaryName(goos, goarch))
	for _, asset := range r.Assets {
		if strings.ToLower(asset.Name) == expected {
			return asset.BrowserDownloadURL, asset.Name
		}
	}

	// Otherwise fall back to an asset that names both the OS and a matching architecture.
	for _, asset := range r.Assets {
		nameLower := strings.ToLower(asset.Name)

		if !binaryAssetExtensions[strings.ToLower(filepath.Ext(nameLower))] {
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

	head := make([]byte, 4)
	n, err := io.ReadFull(resp.Body, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return fmt.Errorf("failed to read update: %w", err)
	}
	head = head[:n]

	if !isExecutable(head) {
		return fmt.Errorf("downloaded file is not an executable for this platform; refusing to install it")
	}

	if _, err := f.Write(head); err != nil {
		return fmt.Errorf("failed to write to temporary file: %w", err)
	}

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

// isExecutable reports whether head — the first bytes of a download — starts with the magic
// number of a format this platform can exec. It is the last guard before an arbitrary file is
// renamed over the running binary: a release asset that is an archive or a distro package gets
// rejected here instead of turning the next `orobox` run into "exec format error".
func isExecutable(head []byte) bool {
	magics := map[string][][]byte{
		"linux":   {{0x7f, 'E', 'L', 'F'}},
		"darwin":  {{0xcf, 0xfa, 0xed, 0xfe}, {0xce, 0xfa, 0xed, 0xfe}, {0xca, 0xfe, 0xba, 0xbe}, {0xbe, 0xba, 0xfe, 0xca}},
		"windows": {{'M', 'Z'}},
	}

	for _, magic := range magics[runtime.GOOS] {
		if len(head) >= len(magic) && string(head[:len(magic)]) == string(magic) {
			return true
		}
	}

	return false
}
