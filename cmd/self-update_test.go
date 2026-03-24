package cmd

import (
	"runtime"
	"testing"
)

func TestFindBestAsset(t *testing.T) {
	r := &release{
		TagName: "v0.0.2",
		Assets: []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		}{
			{Name: "orobox-linux-amd64", BrowserDownloadURL: "url-linux-amd64"},
			{Name: "orobox-darwin-arm64", BrowserDownloadURL: "url-darwin-arm64"},
			{Name: "orobox-windows-amd64.exe", BrowserDownloadURL: "url-windows-amd64"},
			{Name: "orobox_Linux_x86_64", BrowserDownloadURL: "url-linux-x86_64"},
			{Name: "orobox_Linux_x86_64.tar.gz", BrowserDownloadURL: "url-linux-x86_64-archive"},
			{Name: "orobox_Windows_x86_64.zip", BrowserDownloadURL: "url-windows-x86_64-archive"},
		},
	}

	t.Run("MatchCurrentPlatform", func(t *testing.T) {
		url, name := findBestAsset(r)
		// This test depends on the platform Junie is running on (likely linux/amd64)
		if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
			// Now it should match either linux-amd64 OR linux-x86_64
			// But since orobox-linux-amd64 comes first in our mock list, it will pick that.
			if url != "url-linux-amd64" && url != "url-linux-x86_64" {
				t.Errorf("expected linux-amd64 or linux-x86_64, got %s, %s", name, url)
			}
			// It should NEVER match the archive
			if url == "url-linux-x86_64-archive" {
				t.Errorf("expected binary, got archive")
			}
		}
	})

	t.Run("MatchX86_64ForAMD64", func(t *testing.T) {
		rX86_64 := &release{
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
			}{
				{Name: "orobox_Linux_x86_64", BrowserDownloadURL: "url-linux-x86_64"},
				{Name: "orobox_Linux_x86_64.tar.gz", BrowserDownloadURL: "url-linux-x86_64-archive"},
			},
		}

		if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
			url, name := findBestAsset(rX86_64)
			if url != "url-linux-x86_64" {
				t.Errorf("expected url-linux-x86_64, got %s (name: %s)", url, name)
			}
		}
	})

	t.Run("IgnoreArchives", func(t *testing.T) {
		rArchives := &release{
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
			}{
				{Name: "orobox_Linux_x86_64.tar.gz", BrowserDownloadURL: "url-linux-archive"},
				{Name: "orobox_Linux_x86_64.zip", BrowserDownloadURL: "url-linux-zip"},
			},
		}
		url, _ := findBestAsset(rArchives)
		if url != "" {
			t.Errorf("expected no binary found (all archives), got %s", url)
		}
	})

	t.Run("NoMatch", func(t *testing.T) {
		rEmpty := &release{
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
			}{
				{Name: "other-file", BrowserDownloadURL: "other-url"},
			},
		}
		url, name := findBestAsset(rEmpty)
		if url != "" || name != "" {
			t.Errorf("expected empty results, got %s, %s", name, url)
		}
	})
}
