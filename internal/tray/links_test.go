package tray

import (
	"testing"

	"github.com/algoritma-dev/orobox/internal/config"
)

func TestServiceLinksNilConfigReturnsEmpty(t *testing.T) {
	if got := ServiceLinks(nil); len(got) != 0 {
		t.Errorf("ServiceLinks(nil) = %v, want empty", got)
	}
}

func TestServiceLinksOnlyEnabledServices(t *testing.T) {
	cfg := &config.OroConfig{Services: config.ServicesConfig{Mailpit: true, Adminer: false, RedisInsight: true}}

	got := ServiceLinks(cfg)

	names := map[string]string{}
	for _, l := range got {
		names[l.Name] = l.URL
	}
	if url, ok := names["mailpit"]; !ok || url != "http://localhost:8025" {
		t.Errorf("mailpit link = %q, ok=%v, want http://localhost:8025", url, ok)
	}
	if url, ok := names["redisinsight"]; !ok || url != "http://localhost:8001" {
		t.Errorf("redisinsight link = %q, ok=%v, want http://localhost:8001", url, ok)
	}
	if _, ok := names["adminer"]; ok {
		t.Errorf("adminer link present = %v, want absent (disabled in config)", ok)
	}
	if _, ok := names["rabbitmq"]; ok {
		t.Errorf("rabbitmq link present = %v, want absent (disabled in config)", ok)
	}
}

func TestServiceLinksAllFiveWhenAllEnabled(t *testing.T) {
	cfg := &config.OroConfig{Services: config.ServicesConfig{
		Mailpit: true, Adminer: true, RedisInsight: true, RabbitMQ: true, Kibana: true,
	}}

	got := ServiceLinks(cfg)

	want := map[string]string{
		"mailpit":      "http://localhost:8025",
		"adminer":      "http://localhost:8081",
		"redisinsight": "http://localhost:8001",
		"rabbitmq":     "http://localhost:15672",
		"kibana":       "http://localhost:5601",
	}
	if len(got) != len(want) {
		t.Fatalf("ServiceLinks() = %+v, want %d entries", got, len(want))
	}
	for _, l := range got {
		if want[l.Name] != l.URL {
			t.Errorf("ServiceLinks()[%q] = %q, want %q", l.Name, l.URL, want[l.Name])
		}
	}
}
