package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// phpIniDirectivePattern is the alphabet of a directive name. Names are written verbatim into
// an ini file, so anything outside it (`;` starts a comment, `=` ends the name, `"` and
// brackets have meaning of their own) could change what the line says.
var phpIniDirectivePattern = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)

// PhpIni is the resolved `php_ini` key. Exactly one of the two fields is set, or neither when
// the project does not customize PHP settings.
type PhpIni struct {
	// File is the project-relative path of an ini file the project already owns, when php_ini
	// is a string. That file is mounted as-is instead of a generated one.
	File string
	// Values maps a php.ini directive to a scalar, when php_ini is a map. Keys are kept exactly
	// as written: php.ini directive names are case-sensitive and contain dots.
	Values map[string]any
}

// PhpIniSettings validates and resolves the php_ini key of a parsed config. It is the form the
// Dagger pipeline uses, which holds an OroConfig rather than a loaded viper.
//
// It is only correct on a config produced by ParseConfig or LoadConfigFile. A config filled by
// viper.Unmarshal has already had its keys split on "." and lowercased, so `xdebug.log_level`
// is a nested map by then and the directive names are lost for good.
func (c *OroConfig) PhpIniSettings() (PhpIni, error) {
	if err := validatePhpIni(c.PhpIni); err != nil {
		return PhpIni{}, err
	}
	switch raw := c.PhpIni.(type) {
	case string:
		return PhpIni{File: normalizeDockerfilePath(raw)}, nil
	case map[string]any:
		values := make(map[string]any, len(raw))
		for key, value := range raw {
			values[key] = value
		}
		return PhpIni{Values: values}, nil
	}
	return PhpIni{}, nil
}

// GetPhpIni returns the php_ini settings of the loaded .orobox.yaml, or the zero value when
// there is no config file or no such key.
//
// It re-parses the file instead of calling viper.Get("php_ini"): viper splits keys on "." and
// lowercases them, so `xdebug.log_level` would come back as a nested `xdebug: {log_level: …}`
// and `Memory_Limit` as `memory_limit`. A directive name has to reach php.ini untouched.
//
// For the file form it also checks the file exists, as ValidateFiles does.
func GetPhpIni() (PhpIni, error) {
	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		return PhpIni{}, nil
	}
	data, err := os.ReadFile(configFile)
	if err != nil {
		return PhpIni{}, fmt.Errorf("reading %s: %w", configFile, err)
	}
	conf, err := ParseConfig(data)
	if err != nil {
		return PhpIni{}, err
	}
	ini, err := conf.PhpIniSettings()
	if err != nil {
		return PhpIni{}, err
	}
	// ValidateFiles already refused a missing file while the config loaded; this stays as a
	// fallback for callers that reach the settings without going through the root command.
	if err := checkPhpIniFile(filepath.Dir(configFile), ini); err != nil {
		return PhpIni{}, err
	}
	return ini, nil
}

// ValidateFiles checks what Validate cannot because it holds no directory to resolve paths
// against: that the files the config points at exist. baseDir is the directory holding
// .orobox.yaml. Call it right after Validate.
//
// For php_ini the file form must name an existing regular file. A missing bind source would
// make Docker create an empty directory in its place, and PHP then silently ignores the "ini
// file", so the mistake has to be loud and early.
func (c *OroConfig) ValidateFiles(baseDir string) error {
	ini, err := c.PhpIniSettings()
	if err != nil {
		return err
	}
	return checkPhpIniFile(baseDir, ini)
}

// checkPhpIniFile verifies the file form of php_ini; the map form and an unset key have no file.
func checkPhpIniFile(baseDir string, ini PhpIni) error {
	if ini.File == "" {
		return nil
	}
	abs := filepath.Join(baseDir, filepath.FromSlash(ini.File))
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("config error: 'php_ini' file %s does not exist (resolved to %s)", strconv.Quote(ini.File), abs)
	}
	if info.IsDir() {
		return fmt.Errorf("config error: 'php_ini' must be a file, but %s is a directory", strconv.Quote(ini.File))
	}
	return nil
}

// validatePhpIni checks the shape of the php_ini key: absent, a flat map of scalars, or a
// project-relative path. Existence of the file is GetPhpIni's job, for the reason given there.
func validatePhpIni(raw any) error {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(v) == "" {
			return errors.New("config error: 'php_ini' must not be an empty string; give a path or a map of directives")
		}
		return validateProjectPathKey("php_ini", v)
	case map[string]any:
		for key, value := range v {
			if !phpIniDirectivePattern.MatchString(key) {
				return fmt.Errorf("config error: 'php_ini' directive name %s must be non-empty and contain only letters, digits, '.', '_' and '-'", strconv.Quote(key))
			}
			// Caught here rather than at render time so every consumer (dev stack and deploy
			// pipeline) fails the same way and early; a quoted value cannot span lines in php.ini.
			if str, ok := value.(string); ok && strings.ContainsAny(str, "\r\n") {
				return fmt.Errorf("config error: 'php_ini.%s' must not contain a line break; php.ini values are single-line", key)
			}
			// A value with `$` is written single-quoted, the only php.ini form that does not
			// expand ${VAR}; single quotes have no escape, so a `'` in such a value cannot be
			// written at all.
			if str, ok := value.(string); ok && strings.Contains(str, "$") && strings.Contains(str, "'") {
				return fmt.Errorf("config error: 'php_ini.%s' must not contain both '$' and a single quote; php.ini can only keep a '$' literal inside single quotes, which cannot hold a single quote themselves", key)
			}
			if !isPhpIniScalar(value) {
				return fmt.Errorf("config error: 'php_ini.%s' must be a string, number or boolean; php.ini has no nesting, so write a dotted key such as xdebug.mode instead of a nested map", key)
			}
		}
		return nil
	}
	return fmt.Errorf("config error: 'php_ini' must be a map of directives or the path of an ini file, got %T", raw)
}

// isPhpIniScalar reports whether a value has a php.ini spelling.
func isPhpIniScalar(value any) bool {
	switch value.(type) {
	case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	}
	return false
}
