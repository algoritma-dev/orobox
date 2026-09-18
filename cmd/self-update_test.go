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

// TestFindBestAssetSkipsDistroPackages pins the real asset list of release 1.0.0. The .apk,
// .deb and .rpm packages all contain "linux" and "amd64" in their names and sort before the
// raw binary, so an earlier substring match downloaded orobox_1.0.0_linux_amd64.apk — a gzip
// stream — and installing it produced "exec format error".
func TestFindBestAssetSkipsDistroPackages(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("asset list is pinned to linux/amd64")
	}

	names := []string{
		"checksums.txt",
		"orobox_1.0.0_linux_amd64.apk",
		"orobox_1.0.0_linux_amd64.deb",
		"orobox_1.0.0_linux_amd64.rpm",
		"orobox_1.0.0_linux_arm64.apk",
		"orobox_1.0.0_linux_arm64.deb",
		"orobox_1.0.0_linux_arm64.rpm",
		"orobox_Darwin_arm64",
		"orobox_Darwin_arm64.tar.gz",
		"orobox_Darwin_x86_64",
		"orobox_Darwin_x86_64.tar.gz",
		"orobox_Linux_arm64",
		"orobox_Linux_arm64.tar.gz",
		"orobox_Linux_x86_64",
		"orobox_Linux_x86_64.tar.gz",
		"orobox_Windows_arm64.exe",
		"orobox_Windows_arm64.zip",
		"orobox_Windows_x86_64.exe",
		"orobox_Windows_x86_64.zip",
	}

	r := &release{TagName: "1.0.0"}
	for _, n := range names {
		r.Assets = append(r.Assets, struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		}{Name: n, BrowserDownloadURL: "url-" + n})
	}

	url, name := findBestAsset(r)
	if name != "orobox_Linux_x86_64" {
		t.Errorf("expected orobox_Linux_x86_64, got %s (%s)", name, url)
	}
}

func TestExpectedBinaryName(t *testing.T) {
	cases := map[[2]string]string{
		{"linux", "amd64"}:   "orobox_Linux_x86_64",
		{"linux", "arm64"}:   "orobox_Linux_arm64",
		{"darwin", "amd64"}:  "orobox_Darwin_x86_64",
		{"darwin", "arm64"}:  "orobox_Darwin_arm64",
		{"windows", "amd64"}: "orobox_Windows_x86_64.exe",
		{"windows", "386"}:   "orobox_Windows_i386.exe",
	}

	for in, want := range cases {
		if got := expectedBinaryName(in[0], in[1]); got != want {
			t.Errorf("expectedBinaryName(%s, %s) = %s, want %s", in[0], in[1], got, want)
		}
	}
}
