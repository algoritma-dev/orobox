package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/spf13/viper"
)

func TestResolveQaEnv(t *testing.T) {
	// A developer's stack has dev installed and the test database only after `orobox test-init`,
	// so only CI analyses against test — and only once that install actually happened. Warming
	// PHPStan's cache in an environment that was never installed fails the tool outright, which
	// grades as "phpstan could not run" rather than as a finding.
	for _, tc := range []struct {
		name        string
		ci          string
		testInstall bool
		want        qatools.Env
	}{
		{name: "outside CI the tools run in dev", testInstall: true, want: qatools.EnvDev},
		{name: "in CI with test installed they run in test", ci: "true", testInstall: true, want: qatools.EnvTest},
		{name: "in CI without a test install they fall back to dev", ci: "true", want: qatools.EnvDev},
		{name: "an empty CI variable is not CI", ci: "", want: qatools.EnvDev},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CI", tc.ci)
			original := qaTestEnvInstalled
			qaTestEnvInstalled = func() bool { return tc.testInstall }
			defer func() { qaTestEnvInstalled = original }()

			if got := resolveQaEnv(); got != tc.want {
				t.Errorf("resolveQaEnv() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveEngine(t *testing.T) {
	tests := []struct {
		name        string
		flag        string
		ci          string
		installType string
		want        string
		wantErr     bool
	}{
		{name: "default outside CI is compose", installType: "project", want: engineCompose},
		{name: "default in CI is dagger", ci: "true", installType: "project", want: engineDagger},
		{name: "an empty CI variable is not CI", ci: "", installType: "project", want: engineCompose},
		{name: "a bundle in CI stays on compose", ci: "true", installType: "bundle", want: engineCompose},
		{name: "explicit compose wins in CI", flag: engineCompose, ci: "true", installType: "project", want: engineCompose},
		{name: "explicit dagger wins outside CI", flag: engineDagger, installType: "project", want: engineDagger},
		{name: "explicit dagger on a bundle is an error", flag: engineDagger, installType: "bundle", wantErr: true},
		{name: "an unknown engine is an error", flag: "podman", installType: "project", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CI", tc.ci)
			viper.Set("type", tc.installType)
			defer viper.Set("type", nil)

			got, err := resolveEngine(tc.flag)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveEngine(%q) = %q, want an error", tc.flag, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveEngine(%q) returned %v", tc.flag, err)
			}
			if got != tc.want {
				t.Errorf("resolveEngine(%q) = %q, want %q", tc.flag, got, tc.want)
			}
		})
	}
}

func TestResolveReport(t *testing.T) {
	if got, err := resolveReport(""); err != nil || got != qatools.ReportNone {
		t.Errorf(`resolveReport("") = %v, %v`, got, err)
	}
	if got, err := resolveReport("gitlab"); err != nil || got != qatools.ReportGitLab {
		t.Errorf(`resolveReport("gitlab") = %v, %v`, got, err)
	}

	_, err := resolveReport("junit")
	if err == nil {
		t.Fatal("an unknown format must be an error")
	}
	if !strings.Contains(err.Error(), "gitlab") {
		t.Errorf("the error must name the accepted values: %v", err)
	}
}

func TestResolveCacheScopePrefersTheCIBranch(t *testing.T) {
	t.Setenv("CI_COMMIT_REF_NAME", "feature/reports")

	if got := resolveCacheScope(""); got != "feature/reports" {
		t.Errorf("resolveCacheScope = %q, want the CI branch", got)
	}
	if got := resolveCacheScope("explicit"); got != "explicit" {
		t.Errorf("an explicit scope must win: %q", got)
	}
}

func TestResolveReportPathStaysInsideTheProject(t *testing.T) {
	dir := t.TempDir()
	viper.Set("type", "project")
	defer viper.Set("type", nil)

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()

	got, err := resolveReportPath("", reportsRelDir+"/code-quality.json")
	if err != nil {
		t.Fatalf("resolveReportPath returned %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join("var", "orobox", "reports", "code-quality.json")) {
		t.Errorf("default path = %q", got)
	}

	if _, err := resolveReportPath("/etc/orobox.json", reportsRelDir+"/code-quality.json"); err == nil {
		t.Error("a path outside the project must be rejected: the compose engine writes it through a bind mount")
	}
}

func TestReportStatusFailed(t *testing.T) {
	dir := t.TempDir()

	if reportStatusFailed(dir) {
		t.Error("a missing status file must not be read as a failure")
	}

	if err := os.WriteFile(filepath.Join(dir, qatools.StatusFile), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !reportStatusFailed(dir) {
		t.Error("status 1 means at least one tool reported violations")
	}

	if err := os.WriteFile(filepath.Join(dir, qatools.StatusFile), []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}
	if reportStatusFailed(dir) {
		t.Error("status 0 means everything was clean")
	}
}
