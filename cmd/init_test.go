package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestGenerateConfig(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "orobox-init-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Simulate interactive input:
	// 1. Installation type (selection 1 = bundle)
	// 2. Bundle class: Algoritma\Bundle\TestBundle\TestBundle
	// 3. OroCommerce version (selection 1 = 7.0)
	// 4. Host: test.local
	// 5. Root: public
	// 6. SSL: n
	// 7. Redis: y
	// 8. RedisInsight: y
	// 9. Mailpit: y
	// 10. RabbitMQ: y
	// 11. Elasticsearch: y
	// 12. Kibana: y
	// 13. Adminer: y
	input := "1\nAlgoritma\\Bundle\\TestBundle\\TestBundle\n1\ntest.local\npublic\nn\ny\ny\ny\ny\ny\ny\ny\n"

	oldType := installType
	installType = ""
	defer func() { installType = oldType }()

	oldStdin := stdin
	stdin = strings.NewReader(input)
	defer func() { stdin = oldStdin }()

	generateConfig()

	// Check if .orobox.yaml was created
	if _, err := os.Stat(".orobox.yaml"); os.IsNotExist(err) {
		t.Fatalf(".orobox.yaml was not created")
	}

	// Verify content if needed
	data, err := os.ReadFile(".orobox.yaml")
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "oro_version: \"7.0\"") {
		t.Errorf("Expected oro_version 7.0 in config, got:\n%s", content)
	}
	if !strings.Contains(content, "host: test.local") {
		t.Errorf("Expected host test.local in config, got:\n%s", content)
	}
}

func TestGenerateConfigProject(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "orobox-init-project-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Project install skips the bundle class/namespace prompt entirely.
	// 1. Installation type (selection 2 = project)
	// 2. OroCommerce version (selection 1 = 7.0)
	// 3. Host: proj.local
	// 4. Root: public
	// 5. SSL: n
	// 6. Redis: n
	// 7. Mailpit: y
	// 8. RabbitMQ: n
	// 9. Elasticsearch: n
	// 10. Adminer: y
	input := "2\n1\nproj.local\npublic\nn\nn\ny\nn\nn\ny\n"

	oldType := installType
	installType = ""
	defer func() { installType = oldType }()

	oldStdin := stdin
	stdin = strings.NewReader(input)
	defer func() { stdin = oldStdin }()

	generateConfig()

	data, err := os.ReadFile(".orobox.yaml")
	if err != nil {
		t.Fatalf(".orobox.yaml was not created: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "type: project") {
		t.Errorf("Expected type project in config, got:\n%s", content)
	}
	if !strings.Contains(content, "host: proj.local") {
		t.Errorf("Expected host proj.local in config, got:\n%s", content)
	}
	// No bundle namespace/class should have been collected (fields stay empty).
	if !strings.Contains(content, `namespace: ""`) || !strings.Contains(content, `class: ""`) {
		t.Errorf("Project config should have empty bundle namespace/class, got:\n%s", content)
	}
}

func TestValidateConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "orobox-validate-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Test missing file
	if validateConfig() {
		t.Errorf("validateConfig should return false for missing file")
	}

	// Test invalid file
	os.WriteFile(".orobox.yaml", []byte("invalid yaml"), 0644)
	if validateConfig() {
		t.Errorf("validateConfig should return false for invalid yaml")
	}

	// Test valid file
	validYaml := `
type: bundle
namespace: MyNamespace
oro_version: "6.1"
domains:
  - host: localhost
`
	os.WriteFile(".orobox.yaml", []byte(validYaml), 0644)
	if !validateConfig() {
		t.Errorf("validateConfig should return true for valid config")
	}
}

func TestBundlePathFlagRemoved(t *testing.T) {
	if f := initCmd.Flags().Lookup("bundle-path"); f != nil {
		t.Error("init should no longer register the --bundle-path flag (folder creation moved to 'create')")
	}
}

func TestForceInstallFlagRegistered(t *testing.T) {
	flag := initCmd.Flags().Lookup("force-install")
	if flag == nil {
		t.Fatal("init command should register the --force-install flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--force-install default should be false, got %q", flag.DefValue)
	}
}
