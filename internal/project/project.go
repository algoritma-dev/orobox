package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/algoritma-dev/orobox/internal/config"
)

// Project is a discovered orobox environment. It is a value: no global state, no CWD
// dependency, so a single process can hold many of them at once.
type Project struct {
	Name        string            // compose project name == filepath.Base(HostPath)
	HostPath    string            // absolute path of the repo holding .orobox.yaml
	InternalDir string            // ~/.config/orobox/<Name>
	Config      *config.OroConfig // nil when .orobox.yaml is missing or fails to parse

	// Stale is true when HostPath no longer exists on the filesystem.
	Stale bool
	// ConfigError explains why Config is nil, when it is.
	ConfigError error
	// Conflict is true when the internal directory's marker disagrees with the host path
	// recorded in its own generated docker-compose.yml — the signature of two different
	// repos sharing the same directory basename (see package doc).
	Conflict bool
	// ConflictHostPath is the host path found in the generated docker-compose.yml's bind
	// mount, kept for the tooltip when Conflict is true.
	ConflictHostPath string
}

// State is the tray-facing status of a Project.
type State int

const (
	StateUnknown        State = iota
	StateDown                 // no kernel service running
	StatePartial              // some running, some not
	StateUp                   // every KernelServices entry running and healthy
	StateUpNotInstalled       // kernel services healthy, but oro:install has never run
	StateBusy                 // an action started by the tray is in flight
	StateError                // compose query failed, or a kernel service is unhealthy/exited
	StateConflict             // two host paths map to the same Name
)

// Discover returns every environment orobox has generated files for, by walking the
// per-project directories GetInternalDir creates. Environments whose host path no longer
// exists are returned with Stale set rather than dropped, so a caller can offer to clean them
// up instead of silently forgetting them.
func Discover() ([]Project, error) {
	base, err := registryBase()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var projects []Project
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		internalDir := filepath.Join(base, entry.Name())
		composeFile := filepath.Join(internalDir, "docker-compose.yml")
		if !fileExists(composeFile) {
			continue
		}
		projects = append(projects, discoverOne(entry.Name(), internalDir, composeFile))
	}

	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	return projects, nil
}

func discoverOne(name, internalDir, composeFile string) Project {
	p := Project{Name: name, InternalDir: internalDir}

	marker, markerErr := ReadMarker(internalDir)
	bindMountPath := extractBindMountHostPath(composeFile)

	if markerErr == nil {
		p.HostPath = marker.HostPath
		if bindMountPath != "" && bindMountPath != marker.HostPath {
			p.Conflict = true
			p.ConflictHostPath = bindMountPath
		}
	} else {
		p.HostPath = bindMountPath
	}

	if p.HostPath == "" {
		p.ConfigError = fmt.Errorf("could not determine host path for %s: no project.json and no bind mount found in %s", name, composeFile)
		return p
	}

	if !fileExists(p.HostPath) {
		p.Stale = true
		return p
	}

	data, err := os.ReadFile(filepath.Join(p.HostPath, ".orobox.yaml"))
	if err != nil {
		p.ConfigError = err
		return p
	}
	cfg, err := config.ParseConfig(data)
	if err != nil {
		p.ConfigError = err
		return p
	}
	p.Config = cfg
	return p
}

// Load builds a Project directly from a known host path, without going through Discover's
// registry scan. Unlike Discover, it errors rather than tolerating a missing or unparseable
// .orobox.yaml: a caller using Load already knows this is a real project directory.
func Load(hostPath string) (*Project, error) {
	abs, err := filepath.Abs(hostPath)
	if err != nil {
		return nil, err
	}
	if !fileExists(abs) {
		return nil, fmt.Errorf("host path does not exist: %s", abs)
	}

	name := filepath.Base(abs)
	internalDir, err := internalDirFor(name, abs)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(abs, ".orobox.yaml"))
	if err != nil {
		return nil, fmt.Errorf("reading .orobox.yaml: %w", err)
	}
	cfg, err := config.ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parsing .orobox.yaml: %w", err)
	}

	return &Project{Name: name, HostPath: abs, InternalDir: internalDir, Config: cfg}, nil
}

// registryBase is ~/.config/orobox (or $XDG_CONFIG_HOME/orobox), the directory Discover scans.
func registryBase() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "orobox"), nil
}

// internalDirFor mirrors internal/config.GetInternalDir(), but parametrically on name and
// hostPath instead of the CWD, so it agrees with the CLI's own resolution without depending on
// the caller's working directory.
func internalDirFor(name, hostPath string) (string, error) {
	if os.Getenv("CI") != "" || os.Getenv("OROBOX_LOCAL_CONFIG") != "" {
		return filepath.Join(hostPath, ".orobox"), nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "orobox", name), nil
}

// extractBindMountHostPath reads the generated docker-compose.yml at composeFile and returns
// the host side of the first absolute bind mount inside the volumes-oro anchor block — the
// project's host path, when project.json is missing or untrusted. Fragile by nature: it reads
// generated YAML as text rather than parsing it, tracking the anchor's compose.go template
// exactly (see templates/docker/docker-compose.yml).
func extractBindMountHostPath(composeFile string) string {
	data, err := os.ReadFile(composeFile)
	if err != nil {
		return ""
	}

	inAnchor := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inAnchor {
			if strings.Contains(line, "&volumes-oro") {
				inAnchor = true
			}
			continue
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(trimmed, "-") {
			break
		}

		item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		item = strings.Trim(item, `"`)
		if !strings.HasPrefix(item, "/") {
			continue
		}
		if idx := strings.Index(item, ":"); idx > 0 {
			return item[:idx]
		}
	}
	return ""
}
