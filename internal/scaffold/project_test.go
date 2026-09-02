package scaffold

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectClonesResolvedTagAndStripsGit(t *testing.T) {
	oldTag, oldClone := latestTag, cloneRepo
	defer func() { latestTag, cloneRepo = oldTag, oldClone }()

	var gotTagRepo, gotTagPrefix string
	latestTag = func(repoURL, versionPrefix string) (string, error) {
		gotTagRepo, gotTagPrefix = repoURL, versionPrefix
		return "6.1.0", nil
	}

	var gotCloneRepo, gotCloneRef, gotCloneDest string
	cloneRepo = func(repoURL, ref, dest string) error {
		gotCloneRepo, gotCloneRef, gotCloneDest = repoURL, ref, dest
		// Simulate a clone: a checkout plus a .git dir that must be stripped.
		if err := os.MkdirAll(filepath.Join(dest, ".git"), 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, "composer.json"), []byte("{}"), 0644)
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "myproj")

	if err := Project(dest, "6.1"); err != nil {
		t.Fatalf("Project: %v", err)
	}

	if gotTagRepo != OroApplicationRepo || gotTagPrefix != "6.1" {
		t.Errorf("latestTag called with (%q, %q), want (%q, %q)", gotTagRepo, gotTagPrefix, OroApplicationRepo, "6.1")
	}
	if gotCloneRepo != OroApplicationRepo || gotCloneRef != "6.1.0" || gotCloneDest != dest {
		t.Errorf("cloneRepo called with (%q, %q, %q), want (%q, %q, %q)", gotCloneRepo, gotCloneRef, gotCloneDest, OroApplicationRepo, "6.1.0", dest)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); !os.IsNotExist(err) {
		t.Error(".git directory should be stripped after clone")
	}
	if _, err := os.Stat(filepath.Join(dest, "composer.json")); err != nil {
		t.Errorf("cloned composer.json should remain: %v", err)
	}
}

func TestProjectRefusesNonEmptyDir(t *testing.T) {
	oldClone := cloneRepo
	defer func() { cloneRepo = oldClone }()

	cloneCalled := false
	cloneRepo = func(_, _, _ string) error {
		cloneCalled = true
		return nil
	}

	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "x.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Project(dest, "6.1"); err == nil {
		t.Fatal("expected error for non-empty dir, got nil")
	}
	if cloneCalled {
		t.Error("cloneRepo must not run when the target dir is non-empty")
	}
}
