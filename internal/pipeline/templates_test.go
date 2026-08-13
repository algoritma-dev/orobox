package pipeline

import (
	"os"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
)

func TestMain(m *testing.M) {
	// The real templates live at the repository root; render those rather than fixtures so
	// the tests fail when the shipped PHP changes shape.
	Templates = os.DirFS("../..")
	os.Exit(m.Run())
}

func TestRenderRecipe(t *testing.T) {
	out, err := RenderRecipe()
	if err != nil {
		t.Fatalf("RenderRecipe() error = %v", err)
	}
	rendered := string(out)

	// Deployer's own {{var}} placeholders must survive Go templating untouched.
	for _, want := range []string{"{{release_path}}", "{{deploy_path}}", "{{oro_console}}", "{{bin/php}}"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered recipe lost the Deployer placeholder %s", want)
		}
	}

	if strings.Contains(rendered, "[[") || strings.Contains(rendered, "]]") {
		t.Error("rendered recipe still contains Go template delimiters")
	}

	for _, want := range []string{config.VendorArtifactName, config.AssetsArtifactName} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered recipe does not mention artifact %s", want)
		}
	}

	// The task chain and the conditional update are the contract with the spec.
	for _, want := range []string{
		"task('oro:upload_artifacts'",
		"task('oro:update'",
		"task('oro:assets_install'",
		"task('oro:cache_warmup'",
		"task('oro:restart'",
		"task('oro:cleanup_failed'",
		"fail('deploy', 'oro:cleanup_failed')",
		"oro:platform:update --force",
		"assets:install",
		"--env=prod",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered recipe is missing %q", want)
		}
	}

	// platform:update has no --skip-assets option on any supported Oro version.
	if strings.Contains(rendered, "--skip-assets") {
		t.Error("rendered recipe passes --skip-assets, which no supported Oro version accepts")
	}

	// The non-migration path must not contain the two migration commands.
	nonMigration := rendered[strings.Index(rendered, "oro_non_migration_commands"):]
	nonMigration = nonMigration[:strings.Index(nonMigration, "]);")]
	for _, forbidden := range []string{"oro:migration:load", "oro:migration:data:load"} {
		if strings.Contains(nonMigration, forbidden) {
			t.Errorf("the non-migration command list contains %q", forbidden)
		}
	}
	for _, want := range []string{"oro:cron:definitions:load", "oro:workflow:definitions:load", "oro:translation:dump"} {
		if !strings.Contains(nonMigration, want) {
			t.Errorf("the non-migration command list is missing %q", want)
		}
	}

	// Both permission command spellings must be probed, since the name changed between versions.
	for _, want := range []string{"oro:permission:configuration:load", "security:permission:configuration:load"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered recipe does not probe for %q", want)
		}
	}
}

func TestRenderStub(t *testing.T) {
	out, err := RenderStub()
	if err != nil {
		t.Fatalf("RenderStub() error = %v", err)
	}
	rendered := string(out)

	for _, want := range []string{
		"require 'recipe/common.php';",
		config.DeployRecipeRelPath,
		"OROBOX_DEPLOY_REPOSITORY",
		"OROBOX_DEPLOY_REF",
		"OROBOX_DEPLOY_HOST",
		"OROBOX_DEPLOY_USER",
		"OROBOX_DEPLOY_PORT",
		"OROBOX_DEPLOY_PATH",
		"OROBOX_DEPLOY_KEEP_RELEASES",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered stub is missing %q", want)
		}
	}

	if strings.Contains(rendered, "[[") {
		t.Error("rendered stub still contains Go template delimiters")
	}
}
