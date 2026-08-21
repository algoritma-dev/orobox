package cmd

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/algoritma-dev/orobox/internal/docker"
	"github.com/algoritma-dev/orobox/internal/qatools"
	"github.com/spf13/viper"
)

func TestTestComposeReportModeLogsJUnitAndMerges(t *testing.T) {
	oldRun := docker.RunComposeCommand
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	defer func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandWithOutput = oldRunWithOutput
	}()

	var calls [][]string
	docker.RunComposeCommand = func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			return []byte(`{"Service": "application", "State": "running"}`), nil
		}
		return []byte("[]"), nil
	}

	// The command refuses to run against a database it believes is not installed; that check is
	// not what this test is about.
	docker.SetDatabaseInitializedCache(true, true)
	defer docker.SetDatabaseInitializedCache(true, false)

	viper.Set("type", "project")
	defer viper.Set("type", nil)

	dir := t.TempDir()
	previous, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()

	format, err := resolveReport("gitlab")
	if err != nil {
		t.Fatal(err)
	}

	runTestOnCompose(format)

	var flat string
	for _, args := range calls {
		flat += strings.Join(args, " ") + "\n"
	}
	if !strings.Contains(flat, "--log-junit") {
		t.Errorf("report mode must ask PHPUnit for a JUnit log:\n%s", flat)
	}

	// PHPUnit is mocked out, so it wrote nothing: the merged document must exist all the same, or
	// the CI job reports a missing artifact instead of an empty run.
	merged := filepath.Join(dir, "var", "orobox", "reports", "junit.xml")
	data, err := os.ReadFile(merged)
	if err != nil {
		t.Fatalf("the merged JUnit document was not written: %v", err)
	}

	var parsed struct {
		XMLName xml.Name `xml:"testsuites"`
	}
	if err := xml.Unmarshal(data, &parsed); err != nil {
		t.Errorf("the merged document is not valid XML: %v", err)
	}
}

func TestTestComposeWithoutReportIsUnchanged(t *testing.T) {
	oldRun := docker.RunComposeCommand
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	defer func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandWithOutput = oldRunWithOutput
	}()

	var calls [][]string
	docker.RunComposeCommand = func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			return []byte(`{"Service": "application", "State": "running"}`), nil
		}
		return []byte("[]"), nil
	}

	docker.SetDatabaseInitializedCache(true, true)
	defer docker.SetDatabaseInitializedCache(true, false)

	viper.Set("type", "project")
	defer viper.Set("type", nil)

	runTestOnCompose(qatools.ReportNone)

	var flat string
	for _, args := range calls {
		flat += strings.Join(args, " ") + "\n"
	}
	if strings.Contains(flat, "--log-junit") {
		t.Errorf("without --report PHPUnit must not be asked for a log:\n%s", flat)
	}
}

func TestTestComposeJoinsSeveralSuites(t *testing.T) {
	oldRun := docker.RunComposeCommand
	oldRunWithOutput := docker.RunComposeCommandWithOutput
	oldSuites := testsuites
	defer func() {
		docker.RunComposeCommand = oldRun
		docker.RunComposeCommandWithOutput = oldRunWithOutput
		testsuites = oldSuites
	}()

	var calls [][]string
	docker.RunComposeCommand = func(_ string, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	docker.RunComposeCommandWithOutput = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "ps" {
			return []byte(`{"Service": "application", "State": "running"}`), nil
		}
		return []byte("[]"), nil
	}

	docker.SetDatabaseInitializedCache(true, true)
	defer docker.SetDatabaseInitializedCache(true, false)

	viper.Set("type", "project")
	defer viper.Set("type", nil)

	testsuites = []string{"unit", "functional"}

	runTestOnCompose(qatools.ReportNone)

	var flat string
	for _, args := range calls {
		flat += strings.Join(args, " ") + "\n"
	}
	// PHPUnit takes a comma-separated list; the Dagger engine runs one invocation per suite
	// instead, because each one writes its own JUnit log.
	if !strings.Contains(flat, "--testsuite unit,functional") {
		t.Errorf("several suites must reach PHPUnit as one comma-separated list:\n%s", flat)
	}
}
