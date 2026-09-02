package scaffold

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/algoritma-dev/orobox/internal/utils"
)

// OroApplicationRepo is the public OroCommerce application skeleton cloned by
// `orobox create project`. Being public, it needs no credentials at create time.
const OroApplicationRepo = "https://github.com/oroinc/orocommerce-application.git"

// latestTag and cloneRepo are indirected so tests can exercise Project without
// touching the network.
var (
	latestTag = utils.GetLatestTag
	cloneRepo = defaultClone
)

// Project clones the OroCommerce application skeleton into destDir at the tag
// resolved from version, then removes the cloned .git so the user starts fresh. It
// refuses a target directory that already exists and is non-empty.
func Project(destDir, version string) error {
	if err := ensureEmptyDir(destDir); err != nil {
		return err
	}

	tag, err := latestTag(OroApplicationRepo, version)
	if err != nil {
		// Fall back to the raw version so an offline tag lookup does not block a clone
		// of an explicit tag.
		tag = version
	}

	if err := cloneRepo(OroApplicationRepo, tag, destDir); err != nil {
		return fmt.Errorf("cloning OroCommerce application: %w", err)
	}

	if err := os.RemoveAll(filepath.Join(destDir, ".git")); err != nil {
		return fmt.Errorf("removing cloned .git: %w", err)
	}
	return nil
}

func defaultClone(repoURL, ref, dest string) error {
	cmd := exec.Command("git", "clone", "--depth", "1", "-b", ref, repoURL, dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
