package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/scaffold"
)

// hookRepo builds a git repository and points the scaffold templates at the real ones.
func hookRepo(t *testing.T) string {
	t.Helper()

	old := scaffold.Templates
	scaffold.Templates = os.DirFS("..")
	t.Cleanup(func() { scaffold.Templates = old })

	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return root
}

func TestGitHooksDir(t *testing.T) {
	root := hookRepo(t)

	hooksDir, err := gitHooksDir(root)
	if err != nil {
		t.Fatalf("gitHooksDir: %v", err)
	}
	if want := filepath.Join(root, ".git", "hooks"); hooksDir != want {
		t.Errorf("gitHooksDir = %s, want %s", hooksDir, want)
	}
}

// TestGitHooksDirHonoursCoreHooksPath: a checkout managed by husky or lefthook runs its hooks from
// somewhere else entirely, and a hook written to .git/hooks there would never run.
func TestGitHooksDirHonoursCoreHooksPath(t *testing.T) {
	root := hookRepo(t)
	if out, err := exec.Command("git", "-C", root, "config", "core.hooksPath", ".husky").CombinedOutput(); err != nil {
		t.Fatalf("git config: %v: %s", err, out)
	}

	hooksDir, err := gitHooksDir(root)
	if err != nil {
		t.Fatalf("gitHooksDir: %v", err)
	}
	if want := filepath.Join(root, ".husky"); hooksDir != want {
		t.Errorf("gitHooksDir = %s, want %s", hooksDir, want)
	}
}

func TestGitHooksDirOutsideRepository(t *testing.T) {
	if _, err := gitHooksDir(t.TempDir()); err == nil {
		t.Error("gitHooksDir reported no error outside a git repository")
	}
}

func TestWritePreCommitHook(t *testing.T) {
	root := hookRepo(t)
	hooksDir, err := gitHooksDir(root)
	if err != nil {
		t.Fatalf("gitHooksDir: %v", err)
	}

	if err := writePreCommitHook(hooksDir, root, "/usr/local/bin/orobox"); err != nil {
		t.Fatalf("writePreCommitHook: %v", err)
	}

	hookPath := filepath.Join(hooksDir, preCommitHookFile)
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// git skips a hook it cannot execute, and skips it silently.
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("the hook is not executable: %v", info.Mode())
	}

	hook, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(hook)

	for _, want := range []string{
		"#!/bin/sh",
		`"$OROBOX" qa --staged || exit 1`,
		`"$OROBOX" test || exit 1`,
		"OROBOX_SKIP_PRECOMMIT",
		"cd '" + root + "'",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the hook is missing %q:\n%s", want, body)
		}
	}

	// The test suite is not narrowed, but it is skipped when the commit stages no PHP.
	if !strings.Contains(body, "grep -qi '\\.php$'") {
		t.Errorf("the hook runs the tests unconditionally:\n%s", body)
	}
	// The binary is recorded absolute: a GUI client runs hooks with a stripped-down PATH.
	if !strings.Contains(body, "OROBOX='/usr/local/bin/orobox'") {
		t.Errorf("the hook does not name the orobox binary by absolute path:\n%s", body)
	}
}

// TestOfferPreCommitHookBacksUpExisting: a project that already has a hook keeps it, under .bak.
func TestOfferPreCommitHookBacksUpExisting(t *testing.T) {
	root := hookRepo(t)
	hooksDir := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, preCommitHookFile)
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\necho theirs\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	withAnswers(t, "y\ny\n")
	offerPreCommitHook(root)

	backup, err := os.ReadFile(hookPath + ".bak")
	if err != nil {
		t.Fatalf("the previous hook was not kept: %v", err)
	}
	if !strings.Contains(string(backup), "echo theirs") {
		t.Errorf("the backup holds the wrong hook: %s", backup)
	}
	hook, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hook), "qa --staged") {
		t.Errorf("the new hook was not installed: %s", hook)
	}
}

// TestOfferPreCommitHookKeepsExistingOnNo: declining the replacement leaves both the hook and the
// working directory exactly as they were — no .bak either.
func TestOfferPreCommitHookKeepsExistingOnNo(t *testing.T) {
	root := hookRepo(t)
	hooksDir := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, preCommitHookFile)
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\necho theirs\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	withAnswers(t, "y\nn\n")
	offerPreCommitHook(root)

	hook, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hook), "echo theirs") {
		t.Errorf("the existing hook was replaced: %s", hook)
	}
	if _, err := os.Stat(hookPath + ".bak"); err == nil {
		t.Error("a backup was written for a replacement that was declined")
	}
}

func TestOfferPreCommitHookDeclined(t *testing.T) {
	root := hookRepo(t)

	withAnswers(t, "n\n")
	offerPreCommitHook(root)

	if _, err := os.Stat(filepath.Join(root, ".git", "hooks", preCommitHookFile)); err == nil {
		t.Error("a hook was installed after the question was declined")
	}
}

// TestOfferPreCommitHookOutsideRepository: `orobox create` leaves a checkout that is not a
// repository yet, and there is nothing to install there.
func TestOfferPreCommitHookOutsideRepository(t *testing.T) {
	withAnswers(t, "y\n")
	offerPreCommitHook(t.TempDir()) // must not panic and must write nothing
}

// withAnswers points the command package's stdin seam at canned answers for one test.
func withAnswers(t *testing.T, answers string) {
	t.Helper()
	old := stdin
	stdin = strings.NewReader(answers)
	t.Cleanup(func() { stdin = old })
}
