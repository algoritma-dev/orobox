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
	// 1. Bundle class: Algoritma\Bundle\TestBundle\TestBundle
	// 2. OroCommerce version: 7.0
	// 3. Host: test.local
	// 4. Root: public
	// 5. SSL: n
	// 6. Redis: y
	// 7. Mailpit: y
	// 8. RabbitMQ: y
	// 9. Elasticsearch: y
	input := "Algoritma\\Bundle\\TestBundle\\TestBundle\n7.0\ntest.local\npublic\nn\ny\ny\ny\ny\n"

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
