package syslog

import (
	"context"
	"strings"
	"testing"
	"time"

	"xdp/pkg/plugin"
)

func TestMetadataUsesStandardSyslogPluginContract(t *testing.T) {
	meta := New().Metadata()
	if meta.Code != "syslog" {
		t.Fatalf("plugin code = %q, want syslog", meta.Code)
	}
	if meta.Type != plugin.TypeInput {
		t.Fatalf("plugin type = %q, want input", meta.Type)
	}
	if meta.Runtime != "go_builtin" {
		t.Fatalf("runtime = %q, want go_builtin", meta.Runtime)
	}
	required, ok := meta.ConfigSchema["required"].([]string)
	if !ok {
		t.Fatalf("required schema = %#v, want []string", meta.ConfigSchema["required"])
	}
	for _, want := range []string{"collector_port", "transport_protocol", "encoding", "log_filter_enabled"} {
		if !containsString(required, want) {
			t.Fatalf("required schema = %#v, missing %s", required, want)
		}
	}
	if meta.Labels["phase"] != "P0" || meta.Labels["runtime_ingest"] != "true" {
		t.Fatalf("labels = %#v, want P0 runtime_ingest=true", meta.Labels)
	}
}

func TestParseConfigUsesStandardSyslogPluginConfig(t *testing.T) {
	cfg, err := parseConfig(map[string]any{
		"collector_port":     5514,
		"transport_protocol": "UDP",
		"encoding":           "UTF-8",
		"log_filter_enabled": true,
		"log_filter_regex":   "action=(allow|deny)",
		"source_name":        "Firewall Syslog",
		"data_source_id":     "firewall-syslog",
		"plugin_version":     "1.0.0",
		"config_version":     3,
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Addr != ":5514" || cfg.Protocol != "udp" || cfg.Encoding != "UTF-8" {
		t.Fatalf("cfg = %#v, want standard endpoint and encoding", cfg)
	}
	if !cfg.LogFilterEnabled || cfg.LogFilterRegex.String() != "action=(allow|deny)" {
		t.Fatalf("filter cfg = enabled=%v regex=%v", cfg.LogFilterEnabled, cfg.LogFilterRegex)
	}
	if cfg.Name != "Firewall Syslog" || cfg.DataSourceID != "firewall-syslog" || cfg.PluginVersion != "1.0.0" || cfg.ConfigVersion != 3 {
		t.Fatalf("metadata cfg = %#v", cfg)
	}
}

func TestParseConfigRejectsInvalidStandardSyslogConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{name: "missing port", cfg: map[string]any{"transport_protocol": "UDP", "encoding": "UTF-8", "log_filter_enabled": false}, want: "collector_port"},
		{name: "tcp disabled", cfg: map[string]any{"collector_port": 5514, "transport_protocol": "TCP", "encoding": "UTF-8", "log_filter_enabled": false}, want: "udp"},
		{name: "bad regex", cfg: map[string]any{"collector_port": 5514, "transport_protocol": "UDP", "encoding": "UTF-8", "log_filter_enabled": true, "log_filter_regex": "("}, want: "log_filter_regex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig(tc.cfg)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
				t.Fatalf("parseConfig() error = %v, want contains %q", err, tc.want)
			}
		})
	}
}

func TestInputLifecycleHealthReloadAndStop(t *testing.T) {
	input := New()
	if err := input.Init(plugin.BasicInitContext{Ctx: context.Background(), Code: "syslog", Version: "1.0.0"}, map[string]any{
		"collector_port":     5514,
		"transport_protocol": "UDP",
		"encoding":           "UTF-8",
		"log_filter_enabled": false,
		"source_name":        "Firewall Syslog",
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	status := input.Health(context.Background())
	if status.Status != "initialized" || status.ListenerStatus != "stopped" {
		t.Fatalf("health after init = %#v", status)
	}
	if err := input.Reload(context.Background(), map[string]any{
		"collector_port":     5515,
		"transport_protocol": "UDP",
		"encoding":           "UTF-8",
		"log_filter_enabled": false,
	}); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	status = input.Health(context.Background())
	if status.Endpoint != "udp://0.0.0.0:5515" {
		t.Fatalf("health endpoint after reload = %q, want udp://0.0.0.0:5515", status.Endpoint)
	}
	if err := input.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	status = input.Health(context.Background())
	if status.ListenerStatus != "stopped" {
		t.Fatalf("health after stop = %#v", status)
	}
}

func TestParseConfigDoesNotDefaultSourcetype(t *testing.T) {
	cfg, err := parseConfig(map[string]any{"addr": ":5514", "protocol": "udp", "name": "Firewall Syslog"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Sourcetype != "" {
		t.Fatalf("sourcetype = %q, want empty so parser rule name owns event sourcetype", cfg.Sourcetype)
	}
}

func TestParseConfigAllowsExplicitSourcetypeForCompatibility(t *testing.T) {
	cfg, err := parseConfig(map[string]any{"addr": ":5514", "protocol": "udp", "name": "Firewall Syslog", "sourcetype": "legacy"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Sourcetype != "legacy" {
		t.Fatalf("sourcetype = %q, want legacy", cfg.Sourcetype)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

var _ = time.Now
