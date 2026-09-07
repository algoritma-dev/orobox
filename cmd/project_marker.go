// Package cmd contains the CLI commands for Orobox.
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/project"
	"github.com/algoritma-dev/orobox/internal/utils"
)

// writeProjectMarker (re)writes project.json into the current project's internal directory,
// making it self-describing for anything that later enumerates ~/.config/orobox/* — the tray's
// discovery, primarily — without a registry of its own. Called from init and up, idempotently:
// every run just overwrites it with the current values.
//
// The host path recorded is the CWD, the same source config.GetProjectName() derives the
// compose project name from — not GetHostBundlePath(), which can diverge from the CWD when
// --config points elsewhere and would record a host_path inconsistent with Name.
//
// Best-effort: a failure here must never fail init or up, since project.json is metadata for
// the tray, not something the CLI itself depends on.
func writeProjectMarker(conf *config.OroConfig) {
	hostPath, err := os.Getwd()
	if err != nil {
		return
	}

	marker := project.Marker{
		Schema:        project.CurrentSchema,
		Name:          config.GetProjectName(),
		HostPath:      hostPath,
		Type:          conf.Type,
		OroVersion:    conf.OroVersion,
		OroboxVersion: Version,
		UpdatedAt:     time.Now().UTC(),
	}

	if err := project.WriteMarker(config.GetInternalDir(), marker); err != nil {
		utils.PrintWarning(fmt.Sprintf("Could not write project.json: %v", err))
	}
}
