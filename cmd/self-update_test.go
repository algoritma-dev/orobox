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
		},
	}

	t.Run("MatchCurrentPlatform", func(t *testing.T) {
		url, name := findBestAsset(r)
		// This test depends on the platform Junie is running on (likely linux/amd64)
		if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
			if url != "url-linux-amd64" || name != "orobox-linux-amd64" {
				t.Errorf("expected linux-amd64, got %s, %s", name, url)
			}
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
