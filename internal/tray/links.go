package tray

import "github.com/algoritma-dev/orobox/internal/config"

// ServiceLink is one optional service's UI, opened with xdg-open — never a hardcoded browser.
type ServiceLink struct {
	Name  string // matches config.ServicesConfig's field, lowercased
	Label string
	URL   string
}

// serviceCatalog is every optional service's fixed host port (§2.4: two environments can never
// be up at once, so these never need a per-project port).
var serviceCatalog = []struct {
	name, label, url string
	enabled          func(config.ServicesConfig) bool
}{
	{"mailpit", "Mailpit", "http://localhost:8025", func(s config.ServicesConfig) bool { return s.Mailpit }},
	{"adminer", "Adminer", "http://localhost:8081", func(s config.ServicesConfig) bool { return s.Adminer }},
	{"redisinsight", "RedisInsight", "http://localhost:8001", func(s config.ServicesConfig) bool { return s.RedisInsight }},
	{"rabbitmq", "RabbitMQ", "http://localhost:15672", func(s config.ServicesConfig) bool { return s.RabbitMQ }},
	{"kibana", "Kibana", "http://localhost:5601", func(s config.ServicesConfig) bool { return s.Kibana }},
}

// ServiceLinks returns the enabled optional services' links, in the fixed order above.
func ServiceLinks(cfg *config.OroConfig) []ServiceLink {
	if cfg == nil {
		return nil
	}
	var links []ServiceLink
	for _, s := range serviceCatalog {
		if s.enabled(cfg.Services) {
			links = append(links, ServiceLink{Name: s.name, Label: s.label, URL: s.url})
		}
	}
	return links
}
