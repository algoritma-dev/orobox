package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/viper"
)

// DefaultPorts maps every host port the generated compose stack publishes to the value it has
// when `ports:` does not mention it. The keys are the vocabulary of the `ports:` section of
// .orobox.yaml; the container side of each mapping is fixed in the compose templates.
//
// Do not mutate it: GetPorts returns a copy for callers that need to.
var DefaultPorts = map[string]int{
	"http":          8080,  // web, container 80
	"https":         8443,  // web, container 443
	"db":            5432,  // db
	"db_test":       5433,  // db-test (docker-compose.test.yml)
	"redis":         6379,  // redis
	"redisinsight":  8001,  // redisinsight, container 5540
	"mail_ui":       8025,  // mail
	"mail_smtp":     2025,  // mail, container 1025
	"rabbitmq":      5672,  // rabbitmq
	"rabbitmq_ui":   15672, // rabbitmq
	"elasticsearch": 9200,  // elasticsearch
	"kibana":        5601,  // kibana
	"adminer":       8081,  // adminer, container 8080
	"gotenberg":     3000,  // gotenberg
}

// requiredPorts are the keys that cannot be 0. The application URLs (printed by `orobox up` and
// stored in Oro's config by a db restore) and the websocket frontend port are built from them,
// so an unpublished web port would yield `http://host:0`.
var requiredPorts = map[string]bool{"http": true, "https": true}

// validatePorts rejects a `ports:` section that would silently do nothing or produce an invalid
// compose file: an unknown key (a typo like `dbb` would keep the default and look accepted) and
// a number that is not a TCP port. 0 is valid and means "do not publish this port on the host",
// except for http and https (see requiredPorts).
func validatePorts(p map[string]int) error {
	keys := make([]string, 0, len(p))
	for key := range p {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if _, ok := DefaultPorts[key]; !ok {
			return fmt.Errorf("config error: unknown key %q in 'ports' (valid keys: %s)", key, strings.Join(portKeys(), ", "))
		}
		if value := p[key]; requiredPorts[key] && value == 0 {
			return fmt.Errorf("config error: 'ports.%s' cannot be 0: the web port is the application's entry point "+
				"(the browser and the websocket frontend both use it), so it cannot be left unpublished; use a port between 1 and 65535", key)
		}
		if value := p[key]; value < 0 || value > 65535 {
			return fmt.Errorf("config error: 'ports.%s' is %d, want 0 (not published) or a port between 1 and 65535", key, value)
		}
	}
	return nil
}

// portKeys returns the valid `ports:` keys in a stable order for error messages.
func portKeys() []string {
	keys := make([]string, 0, len(DefaultPorts))
	for key := range DefaultPorts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// GetPorts returns DefaultPorts overlaid with the `ports:` section of the loaded config. The
// result is a fresh map the caller may modify.
//
// A key present in the config wins even when its value is 0, which is why this checks IsSet
// instead of treating a zero value as "unset": 0 is the way to say "do not publish". Reading
// through viper is safe here, unlike php_ini: port keys contain no dots and are already
// lowercase.
func GetPorts() map[string]int {
	ports := make(map[string]int, len(DefaultPorts))
	for key, def := range DefaultPorts {
		if viper.IsSet("ports." + key) {
			ports[key] = viper.GetInt("ports." + key)
			continue
		}
		ports[key] = def
	}
	return ports
}

// GetPort returns the host port configured for key, or 0 when the key is unknown.
func GetPort(key string) int {
	return GetPorts()[key]
}
