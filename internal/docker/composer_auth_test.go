package docker

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
)

func TestIsSSHRepoURL(t *testing.T) {
	cases := map[string]bool{
		"git@github.com:org/repo.git":             true,
		"ssh://git@gitlab.com/org/repo.git":       true,
		"git@bitbucket.org:team/repo.git":         true,
		"https://github.com/org/repo.git":         false,
		"http://example.com/repo.git":             false,
		"https://user:token@github.com/org/r.git": false,
		"/local/path/to/repo":                     false,
		"":                                        false,
	}
	for url, want := range cases {
		if got := IsSSHRepoURL(url); got != want {
			t.Errorf("IsSSHRepoURL(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestComposerAuthEnv(t *testing.T) {
	if got := composerAuthEnv(nil); got != "" {
		t.Errorf("expected empty env for nil auth, got %q", got)
	}

	auth := map[string]interface{}{
		"github-oauth": map[string]interface{}{"github.com": "tok123"},
	}
	got := composerAuthEnv(auth)
	if !strings.HasPrefix(got, "COMPOSER_AUTH=") {
		t.Fatalf("expected COMPOSER_AUTH= prefix, got %q", got)
	}
	if !strings.Contains(got, "tok123") || !strings.Contains(got, "github-oauth") {
		t.Errorf("serialized auth missing expected content: %q", got)
	}
}

func TestCredentialRunArgs_AuthOnly(t *testing.T) {
	auth := map[string]interface{}{"bearer": map[string]interface{}{"repo.example.com": "tok"}}
	args := CredentialRunArgs(config.ComposerConfig{Auth: auth})
	if len(args) != 2 || args[0] != "-e" || !strings.HasPrefix(args[1], "COMPOSER_AUTH=") {
		t.Fatalf("expected single -e COMPOSER_AUTH flag, got %v", args)
	}
}

func TestCredentialRunArgs_SSH(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	}
	repos := []map[string]interface{}{
		{"type": "vcs", "url": "git@github.com:org/private.git"},
	}
	args := CredentialRunArgs(config.ComposerConfig{Repositories: repos})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "SSH_AUTH_SOCK="+containerSSHAgentSocket) {
		t.Errorf("expected SSH_AUTH_SOCK env, got %v", args)
	}
	if !strings.Contains(joined, ":"+containerSSHAgentSocket) {
		t.Errorf("expected socket bind mount, got %v", args)
	}
	if !strings.Contains(joined, "GIT_SSH_COMMAND=") {
		t.Errorf("expected GIT_SSH_COMMAND, got %v", args)
	}
}

// On Linux the agent socket is only connectable by its owning uid, so the
// credential run must execute as the host user, not the image default
// (www-data cannot connect to a socket owned by uid 1000).
func TestCredentialRunArgs_SSHRunsAsHostUser(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only behavior")
	}
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	repos := []map[string]interface{}{
		{"type": "vcs", "url": "git@github.com:org/private.git"},
	}
	args := CredentialRunArgs(config.ComposerConfig{Repositories: repos})
	joined := strings.Join(args, " ")
	want := fmt.Sprintf("--user %d:%d", os.Getuid(), os.Getgid())
	if !strings.Contains(joined, want) {
		t.Errorf("expected %q in credential run args, got %v", want, args)
	}
}

func TestCredentialRunArgs_None(t *testing.T) {
	repos := []map[string]interface{}{
		{"type": "vcs", "url": "https://github.com/org/public.git"},
	}
	if args := CredentialRunArgs(config.ComposerConfig{Repositories: repos}); args != nil {
		t.Errorf("expected no args for public https repo + no auth, got %v", args)
	}
}

// boolPtr is a local helper for the tri-state composer.ssh_agent field.
func boolPtr(b bool) *bool { return &b }

func TestNeedsSSHForwardingIn(t *testing.T) {
	sshRepo := []map[string]interface{}{{"type": "vcs", "url": "git@github.com:org/private.git"}}
	httpsRepo := []map[string]interface{}{{"type": "composer", "url": "https://repo.packagist.com/org/"}}

	sshManifest := writeManifest(t, `{"repositories": [{"type": "vcs", "url": "git@github.com:org/from-manifest.git"}]}`)
	httpsManifest := writeManifest(t, `{"repositories": [{"type": "composer", "url": "https://repo.packagist.com/org/"}]}`)
	noManifest := t.TempDir()

	cases := []struct {
		name  string
		dir   string
		conf  config.ComposerConfig
		extra []string
		want  bool
	}{
		{
			name: "explicit false beats an ssh url in .orobox.yaml",
			dir:  sshManifest,
			conf: config.ComposerConfig{Repositories: sshRepo, SSHAgent: boolPtr(false)},
			want: false,
		},
		{
			name: "explicit true forwards with no ssh url anywhere",
			dir:  noManifest,
			conf: config.ComposerConfig{SSHAgent: boolPtr(true)},
			want: true,
		},
		{
			name: "unset detects an ssh url in .orobox.yaml",
			dir:  noManifest,
			conf: config.ComposerConfig{Repositories: sshRepo},
			want: true,
		},
		{
			name: "unset detects an ssh url in the checkout composer.json",
			dir:  sshManifest,
			conf: config.ComposerConfig{Repositories: httpsRepo},
			want: true,
		},
		{
			name: "unset with only https urls does not forward",
			dir:  httpsManifest,
			conf: config.ComposerConfig{Repositories: httpsRepo},
			want: false,
		},
		{
			name:  "unset detects an ssh url passed as an extra url",
			dir:   noManifest,
			conf:  config.ComposerConfig{},
			extra: []string{"git@github.com:oroinc/app.git"},
			want:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsSSHForwardingIn(tc.dir, tc.conf, tc.extra...); got != tc.want {
				t.Errorf("needsSSHForwardingIn = %v, want %v", got, tc.want)
			}
		})
	}
}

// composer.ssh_agent: true must forward even though no repository URL says SSH.
func TestCredentialRunArgs_SSHAgentFlag(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	}
	args := CredentialRunArgs(config.ComposerConfig{SSHAgent: boolPtr(true)})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "SSH_AUTH_SOCK="+containerSSHAgentSocket) {
		t.Errorf("expected SSH_AUTH_SOCK env, got %v", args)
	}
}

// composer.ssh_agent: false must suppress forwarding for an SSH repository URL.
func TestCredentialRunArgs_SSHAgentFlagOff(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	}
	conf := config.ComposerConfig{
		Repositories: []map[string]interface{}{{"type": "vcs", "url": "git@github.com:org/private.git"}},
		SSHAgent:     boolPtr(false),
	}
	if args := CredentialRunArgs(conf); args != nil {
		t.Errorf("expected no args when ssh_agent is false, got %v", args)
	}
}
