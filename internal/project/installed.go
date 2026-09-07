package project

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// runDBQuery runs `docker compose <args>` under the same hard 5s timeout as runPS. A variable
// so tests can replace it without a real Postgres.
var runDBQuery = func(args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	return cmd.CombinedOutput()
}

// IsInstalled reports whether oro:install has ever completed against p's database, mirroring
// internal/docker.IsDatabaseInitialized: a table or database that does not exist yet reads as
// "not installed", not as an error — that is the expected state right after `orobox up` on a
// stack `orobox init` has never touched.
func (p Project) IsInstalled() (bool, error) {
	user, _, dbname, service := p.DatabaseCredentials()

	args := append(p.ComposeArgs(false),
		"exec", "-T", service,
		"psql",
		"-U", user,
		"-d", dbname,
		"-c", "SELECT text_value FROM oro_config_value WHERE name = 'is_installed' AND section = 'oro_distribution';",
		"-t", "-A",
	)

	output, err := runDBQuery(args)
	if err != nil {
		out := string(output)
		if strings.Contains(out, `relation "oro_config_value" does not exist`) ||
			strings.Contains(out, `database "`+dbname+`" does not exist`) {
			return false, nil
		}
		return false, err
	}

	return strings.TrimSpace(string(output)) == "1", nil
}
