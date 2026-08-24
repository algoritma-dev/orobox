package e2e

import (
	"os"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
	"github.com/algoritma-dev/orobox/internal/docker"
)

func TestProjectNameIsDockerSafe(t *testing.T) {
	c := Case{Version: "6.1", Type: TypeProject}
	got := c.ProjectName()
	if got != "oroboxe2e-project-61" {
		t.Fatalf("ProjectName() = %q, want %q", got, "oroboxe2e-project-61")
	}
	if strings.ContainsAny(got, ". ") || strings.ToLower(got) != got {
		t.Fatalf("ProjectName() %q not docker-safe", got)
	}
}

func TestHostIsUnique(t *testing.T) {
	a := Case{Version: "7.0", Type: TypeProject}.Host()
	b := Case{Version: "7.0", Type: TypeBundle}.Host()
	if a == b {
		t.Fatalf("hosts collide: %q", a)
	}
}

func TestParseMatrixDefaults(t *testing.T) {
	cases, err := ParseMatrix("", "")
	if err != nil {
		t.Fatal(err)
	}
	want := len(config.SupportedOroVersions) * 2
	if len(cases) != want {
		t.Fatalf("got %d cases, want %d", len(cases), want)
	}
	// version-outer, type-inner
	if cases[0].Type != TypeProject || cases[1].Type != TypeBundle {
		t.Fatalf("unexpected ordering: %+v", cases[:2])
	}
}

func TestParseMatrixSubsetAndTrim(t *testing.T) {
	cases, err := ParseMatrix(" 6.1 , 7.0 ", "bundle")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(cases))
	}
	for _, c := range cases {
		if c.Type != TypeBundle {
			t.Fatalf("unexpected type %q", c.Type)
		}
	}
}

func TestParseMatrixRejectsUnknownType(t *testing.T) {
	if _, err := ParseMatrix("6.1", "demo"); err == nil {
		t.Fatal("expected error for unknown type 'demo'")
	}
}

func TestRenderConfigSubstitutes(t *testing.T) {
	out, err := RenderConfig("oro_version: \"{{.Version}}\"\nhost: {{.Host}}\n", Case{Version: "6.0", Type: TypeProject})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `oro_version: "6.0"`) || !strings.Contains(out, "oro-project-60.e2e.test") {
		t.Fatalf("render missing substitutions:\n%s", out)
	}
}

func TestResolveBinaryPrefersEnv(t *testing.T) {
	path, needBuild := ResolveBinary(func(k string) string {
		if k == "OROBOX_BIN" {
			return "/custom/orobox"
		}
		return ""
	})
	if path != "/custom/orobox" || needBuild {
		t.Fatalf("ResolveBinary env = (%q,%v)", path, needBuild)
	}
	_, needBuild = ResolveBinary(func(string) string { return "" })
	if !needBuild {
		t.Fatal("expected needBuild when OROBOX_BIN unset")
	}
}

// TestCaseBaseURLUsesThePublishedPort guards against asserting HTTP on port 80: orobox
// publishes nginx on 8080 by default, so a bare host name never answers and every case
// failed with "connection refused" no matter how healthy the stack was.
func TestCaseBaseURLUsesThePublishedPort(t *testing.T) {
	c := Case{Version: "6.1", Type: TypeBundle}

	// The default must track orobox's own default, or the suite polls the wrong port.
	httpPort, _ := docker.GetNginxPorts()
	if want := "http://" + c.Host() + ":" + httpPort; c.BaseURL(func(string) string { return "" }) != want {
		t.Errorf("BaseURL default = %q, want %q (orobox publishes %s)", c.BaseURL(func(string) string { return "" }), want, httpPort)
	}

	// An explicit port wins, using the same variable orobox reads.
	got := c.BaseURL(func(k string) string {
		if k == "ORO_NGINX_HTTP_PORT" {
			return "80"
		}
		return ""
	})
	if want := "http://" + c.Host() + ":80"; got != want {
		t.Errorf("BaseURL with ORO_NGINX_HTTP_PORT = %q, want %q", got, want)
	}
}

func TestFailedDetectsErrorMarkerDespiteZeroExit(t *testing.T) {
	// Orobox init prints "✘ OroCommerce installation failed" but exits 0.
	res := RunResult{Stdout: "\x1b[31m✘ OroCommerce installation failed: exit status 1\x1b[0m\n", ExitCode: 0}
	if !failed(res) {
		t.Fatal("failed() must catch the error marker on a zero exit")
	}
}

func TestFailedOnNonzeroExit(t *testing.T) {
	if !failed(RunResult{ExitCode: 1}) {
		t.Fatal("failed() must catch a nonzero exit")
	}
}

func TestFailedFalseOnCleanSuccess(t *testing.T) {
	res := RunResult{Stdout: "✔ Orobox is up and running!\n", ExitCode: 0}
	if failed(res) {
		t.Fatal("failed() must not flag a clean success (✔ only)")
	}
}

func TestFixturesRenderValid(t *testing.T) {
	for _, f := range []struct {
		path string
		c    Case
	}{
		{"fixtures/project.orobox.yaml", Case{Version: "6.1", Type: TypeProject}},
		{"fixtures/bundle.orobox.yaml", Case{Version: "6.1", Type: TypeBundle}},
	} {
		raw, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatalf("read %s: %v", f.path, err)
		}
		out, err := RenderConfig(string(raw), f.c)
		if err != nil {
			t.Fatalf("render %s: %v", f.path, err)
		}
		if !strings.Contains(out, `oro_version: "6.1"`) {
			t.Fatalf("%s missing oro_version:\n%s", f.path, out)
		}
		if strings.Contains(out, "{{") {
			t.Fatalf("%s has unrendered template markers:\n%s", f.path, out)
		}

		// The suite runs `orobox run e2e-cache-clear`, which dispatches a command defined
		// here by name. A rename on either side used to surface only as a mid-run
		// "command 'x' not found in .orobox.yaml" after a full install, so pin the
		// contract: the fixture must parse and must define exactly that command.
		conf, err := config.ParseConfig([]byte(out))
		if err != nil {
			t.Fatalf("%s does not parse: %v", f.path, err)
		}
		var found bool
		for _, c := range conf.Commands {
			if c.Name == e2eRunCommand {
				found = true
				if c.Command == "" {
					t.Errorf("%s defines %q with an empty command", f.path, e2eRunCommand)
				}
			}
		}
		if !found {
			t.Errorf("%s must define the %q command the suite runs", f.path, e2eRunCommand)
		}
	}
}
