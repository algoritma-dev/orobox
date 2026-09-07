package project

import (
	"os"
	"path/filepath"
)

// ComposeArgs returns the docker compose arguments for p, replicating
// internal/docker.GetBaseComposeArgs but parametrically: no viper, no CWD, so a caller holding
// many Projects can build each one's arguments independently.
func (p Project) ComposeArgs(includeTest bool) []string {
	composeFile := filepath.Join(p.InternalDir, "docker-compose.yml")
	args := []string{"-p", p.Name, "--project-directory", p.InternalDir, "-f", composeFile}

	setupFile := filepath.Join(p.InternalDir, "docker-compose.setup.yml")
	if fileExists(setupFile) {
		args = append(args, "-f", setupFile)
	}

	if includeTest {
		testFile := filepath.Join(p.InternalDir, "docker-compose.test.yml")
		if fileExists(testFile) {
			args = append(args, "-f", testFile)
		}
	}

	return args
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
