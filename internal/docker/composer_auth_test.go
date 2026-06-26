package docker

import (
	"runtime"
	"strings"
	"testing"
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
	args := CredentialRunArgs(auth, nil)
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
	args := CredentialRunArgs(nil, repos)
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

func TestCredentialRunArgs_None(t *testing.T) {
	repos := []map[string]interface{}{
		{"type": "vcs", "url": "https://github.com/org/public.git"},
	}
	if args := CredentialRunArgs(nil, repos); args != nil {
		t.Errorf("expected no args for public https repo + no auth, got %v", args)
	}
}
