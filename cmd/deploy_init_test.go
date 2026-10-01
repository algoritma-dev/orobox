package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// deploy-init rewrites the config the command was started with: with --config that is not
// ./.orobox.yaml, and writing there instead would leave the real config without the stages
// and drop a second, partial config into the working directory.
func TestDeployInitConfigPathFollowsTheLoadedConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	if got := deployInitConfigPath(); got != ".orobox.yaml" {
		t.Errorf("without a loaded config: got %q, want .orobox.yaml", got)
	}

	custom := filepath.Join(t.TempDir(), "staging.orobox.yaml")
	viper.SetConfigFile(custom)
	if got := deployInitConfigPath(); got != custom {
		t.Errorf("with --config: got %q, want %q", got, custom)
	}
}
