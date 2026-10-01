package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// loadProject writes a .orobox.yaml (plus any extra files) into a temp project directory and
// loads it through viper the way the root command does. It returns the project directory.
func loadProject(t *testing.T, yaml string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(dir, ".orobox.yaml")
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	return dir
}

const phpIniBase = "type: project\noro_version: \"6.1\"\ndomains:\n  - host: example.com\n"

// viper splits keys on "." and lowercases them, so reading php_ini through it would turn
// `xdebug.log_level` into a nested map and `Memory_Limit` into `memory_limit`. php.ini
// directive names are written verbatim, so GetPhpIni has to bypass viper for the values.
func TestGetPhpIniKeepsDottedKeys(t *testing.T) {
	loadProject(t, phpIniBase+"php_ini:\n  xdebug.log_level: 0\n  Memory_Limit: 4G\n", nil)

	got, err := GetPhpIni()
	if err != nil {
		t.Fatalf("GetPhpIni failed: %v", err)
	}
	if got.File != "" {
		t.Errorf("expected no file, got %q", got.File)
	}
	if len(got.Values) != 2 {
		t.Fatalf("expected 2 values, got %#v", got.Values)
	}
	if v, ok := got.Values["xdebug.log_level"]; !ok || v != 0 {
		t.Errorf("xdebug.log_level = %#v (present=%v), want 0", v, ok)
	}
	if v, ok := got.Values["Memory_Limit"]; !ok || v != "4G" {
		t.Errorf("Memory_Limit = %#v (present=%v), want 4G", v, ok)
	}
}

func TestGetPhpIniUnset(t *testing.T) {
	t.Run("no config file", func(t *testing.T) {
		viper.Reset()
		defer viper.Reset()
		got, err := GetPhpIni()
		if err != nil || got.File != "" || got.Values != nil {
			t.Errorf("expected the zero value, got %#v, %v", got, err)
		}
	})

	t.Run("no key", func(t *testing.T) {
		loadProject(t, phpIniBase, nil)
		got, err := GetPhpIni()
		if err != nil || got.File != "" || got.Values != nil {
			t.Errorf("expected the zero value, got %#v, %v", got, err)
		}
	})
}

func TestGetPhpIniFileForm(t *testing.T) {
	loadProject(t, phpIniBase+"php_ini: ./docker/php.ini\n", map[string]string{"docker/php.ini": "memory_limit = 1G\n"})

	got, err := GetPhpIni()
	if err != nil {
		t.Fatalf("GetPhpIni failed: %v", err)
	}
	if got.File != "docker/php.ini" || got.Values != nil {
		t.Errorf("unexpected settings %#v", got)
	}
}

// The file has to exist for the mount to mean anything: Docker would otherwise create an empty
// directory at the missing bind source and PHP would silently ignore it.
func TestGetPhpIniMissingFile(t *testing.T) {
	loadProject(t, phpIniBase+"php_ini: docker/php.ini\n", nil)

	_, err := GetPhpIni()
	if err == nil || !strings.Contains(err.Error(), "php_ini") {
		t.Errorf("expected an error naming php_ini, got %v", err)
	}
}

func TestGetPhpIniRejectsADirectory(t *testing.T) {
	loadProject(t, phpIniBase+"php_ini: docker\n", map[string]string{"docker/php.ini": ""})

	if _, err := GetPhpIni(); err == nil {
		t.Error("expected a directory to be rejected")
	}
}

func TestValidatePhpIni(t *testing.T) {
	valid := map[string]any{
		"unset":         nil,
		"flat map":      map[string]any{"memory_limit": "4G", "max_execution_time": 0, "display_errors": true},
		"relative file": "docker/php.ini",
		"dot slash":     "./php.ini",
	}
	for name, raw := range valid {
		if err := validatePhpIni(raw); err != nil {
			t.Errorf("%s: expected to be accepted, got %v", name, err)
		}
	}

	invalid := map[string]any{
		"nested map":     map[string]any{"xdebug": map[string]any{"mode": "debug"}},
		"list value":     map[string]any{"opcache.preload": []any{"a", "b"}},
		"list":           []any{"memory_limit=1G"},
		"number":         42,
		"absolute path":  "/etc/php.ini",
		"escaping path":  "../x",
		"dot":            ".",
		"empty string":   "  ",
		"key with ;":     map[string]any{"memory_limit;x": "1G"},
		"key with quote": map[string]any{`a"b`: "1G"},
		"key with =":     map[string]any{"a=b": "1G"},
		"empty key":      map[string]any{"": "1G"},
		"value with LF":  map[string]any{"memory_limit": "1G\nauto_prepend_file=x"},
		"value with CR":  map[string]any{"memory_limit": "1G\rx"},
	}
	for name, raw := range invalid {
		err := validatePhpIni(raw)
		if err == nil {
			t.Errorf("%s: expected to be rejected", name)
			continue
		}
		if !strings.Contains(err.Error(), "php_ini") {
			t.Errorf("%s: expected the error to name php_ini, got %v", name, err)
		}
	}
}

// A line break in a value used to be caught only when the ini was rendered, so dev dropped every
// setting with a warning while the pipeline failed. Validate has to name the key up front.
func TestValidatePhpIniRejectsLineBreakNamingTheKey(t *testing.T) {
	err := validatePhpIni(map[string]any{"memory_limit": "1G\nx"})
	if err == nil || !strings.Contains(err.Error(), "php_ini.memory_limit") {
		t.Errorf("expected an error naming php_ini.memory_limit, got %v", err)
	}
}

func TestValidatePhpIniNamesTheRejectedKey(t *testing.T) {
	for _, key := range []string{"a;b", `a"b`} {
		err := validatePhpIni(map[string]any{key: 1})
		if err == nil || !strings.Contains(err.Error(), strconv.Quote(key)) {
			t.Errorf("expected the error for %q to name the key, got %v", key, err)
		}
	}
	if err := validatePhpIni(map[string]any{"xdebug.log_level": 0, "Memory_Limit": "4G", "a-b_c": true}); err != nil {
		t.Errorf("expected dotted, mixed-case and dashed names to be accepted, got %v", err)
	}
}

// Validate cannot see the disk; ValidateFiles is where a missing ini file is refused, early,
// with the config-load error instead of a mount Docker would fill with an empty directory.
func TestValidateFilesPhpIni(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docker", "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker", "php.ini"), []byte("a=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{"unset", "", false},
		{"map form", "php_ini:\n  memory_limit: 4G\n", false},
		{"existing file", "php_ini: docker/php.ini\n", false},
		{"missing file", "php_ini: docker/missing.ini\n", true},
		{"directory", "php_ini: docker/sub\n", true},
	}
	for _, tc := range cases {
		conf, err := ParseConfig([]byte(phpIniBase + tc.yaml))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		err = conf.ValidateFiles(dir)
		if tc.wantErr {
			if err == nil || !strings.Contains(err.Error(), "php_ini") {
				t.Errorf("%s: expected an error naming php_ini, got %v", tc.name, err)
			}
		} else if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
	}
}

func TestValidateRejectsAnInvalidPhpIni(t *testing.T) {
	conf, err := ParseConfig([]byte(phpIniBase + "php_ini:\n  xdebug:\n    mode: debug\n"))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if err := conf.Validate(); err == nil || !strings.Contains(err.Error(), "php_ini") {
		t.Errorf("expected Validate to reject a nested php_ini, got %v", err)
	}
}

func TestPhpIniSettings(t *testing.T) {
	conf, err := ParseConfig([]byte(phpIniBase + "php_ini:\n  memory_limit: 4G\n"))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	got, err := conf.PhpIniSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Values["memory_limit"] != "4G" || got.File != "" {
		t.Errorf("unexpected settings %#v", got)
	}

	conf, _ = ParseConfig([]byte(phpIniBase + "php_ini: ./docker//php.ini\n"))
	got, err = conf.PhpIniSettings()
	if err != nil || got.File != "docker/php.ini" {
		t.Errorf("expected the normalized file docker/php.ini, got %#v, %v", got, err)
	}
}

// deploy-init rewrites the whole file, so the key has to survive SaveConfig untouched.
func TestSaveConfigRoundTripsPhpIni(t *testing.T) {
	conf, err := ParseConfig([]byte(phpIniBase + "php_ini:\n  xdebug.log_level: 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), ".orobox.yaml")
	if err := SaveConfig(path, conf); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	back, err := ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	got, err := back.PhpIniSettings()
	if err != nil || got.Values["xdebug.log_level"] != 0 {
		t.Errorf("php_ini lost in the round trip: %#v, %v\n%s", got, err, data)
	}

	conf, _ = ParseConfig([]byte(phpIniBase))
	if err := SaveConfig(path, conf); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "php_ini") {
		t.Errorf("an unset php_ini must not be written:\n%s", data)
	}
}

// deploy-init and test-init --tmpfs rewrite .orobox.yaml. They start from LoadConfigFile, not
// viper.Unmarshal, because viper would turn `xdebug.log_level` into a nested map and lowercase
// `Memory_Limit`, and the file written back would fail to load.
func TestLoadConfigFileKeepsPhpIniAcrossARewrite(t *testing.T) {
	dir := loadProject(t, phpIniBase+"php_ini:\n  xdebug.log_level: 0\n  Memory_Limit: 4G\n", nil)

	// What the old write path did, to pin the hazard this guards against.
	var viaViper OroConfig
	if err := viper.Unmarshal(&viaViper); err != nil {
		t.Fatal(err)
	}
	if err := validatePhpIni(viaViper.PhpIni); err == nil {
		t.Fatal("expected the viper round trip to mangle php_ini; if viper changed, this test and LoadConfigFile's comment are stale")
	}

	conf, err := LoadConfigFile()
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}
	conf.Test.UseTmpfs = true
	path := filepath.Join(dir, ".orobox.yaml")
	if err := SaveConfig(path, conf); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("rewritten config no longer parses: %v\n%s", err, data)
	}
	if err := back.Validate(); err != nil {
		t.Fatalf("rewritten config no longer validates: %v\n%s", err, data)
	}
	got, err := back.PhpIniSettings()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Values["xdebug.log_level"]; !ok {
		t.Errorf("xdebug.log_level lost: %#v", got.Values)
	}
	if got.Values["Memory_Limit"] != "4G" {
		t.Errorf("Memory_Limit lost or lowercased: %#v", got.Values)
	}
}

func TestLoadConfigFileWithoutAConfig(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	if _, err := LoadConfigFile(); err == nil || !strings.Contains(err.Error(), ".orobox.yaml") {
		t.Errorf("expected a clear error naming .orobox.yaml, got %v", err)
	}
}
