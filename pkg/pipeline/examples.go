package pipeline

import "xdp/pkg/plugin"

const JSONPipelineID = "mvp-json-pipeline"
const FirewallPipelineID = "firewall-syslog-pipeline"

func MVPJSONPipeline() Pipeline {
	return Pipeline{
		APIVersion: APIVersionV1Alpha1,
		Kind:       KindPipeline,
		Metadata:   Metadata{ID: JSONPipelineID, Name: "MVP JSON Pipeline"},
		Spec: Spec{
			Source: SourceSpec{ID: "raw-http", Type: "input", Plugin: "http-input", Version: "1.0.0"},
			Stages: []StageSpec{{
				ID:      "parse-json",
				Type:    string(plugin.TypeParser),
				Plugin:  "json-parser",
				Version: "1.0.0",
				Config:  map[string]any{"source": "raw", "target": "fields"},
				OnError: &ErrorPolicy{Action: "dead_letter"},
			}},
			Outputs: []OutputSpec{{ID: "memory-search", Plugin: "memory-output", Version: "1.0.0", Config: map[string]any{"index": "app"}}},
		},
	}
}

func FirewallSyslogPipeline() Pipeline {
	return Pipeline{
		APIVersion: APIVersionV1Alpha1,
		Kind:       KindPipeline,
		Metadata:   Metadata{ID: FirewallPipelineID, Name: "Firewall Syslog Pipeline"},
		Spec: Spec{
			Source: SourceSpec{ID: "raw-syslog", Type: "input", Plugin: "syslog-input", Version: "1.0.0"},
			Stages: []StageSpec{
				{ID: "parse-firewall", Type: string(plugin.TypeParser), Plugin: "regex-parser", Version: "1.0.0", Config: map[string]any{"pattern": `src=(?P<src_ip>\S+) dst=(?P<dst_ip>\S+) action=(?P<action>\S+) bytes=(?P<bytes>\d+)`}, OnError: &ErrorPolicy{Action: "dead_letter"}},
				{ID: "convert-types", Type: string(plugin.TypeTransform), Plugin: "type-convert", Version: "1.0.0", Config: map[string]any{"fields": map[string]any{"bytes": "int"}}},
				{ID: "enrich-src", Type: string(plugin.TypeEnrichment), Plugin: "geoip", Version: "1.0.0", Config: map[string]any{"field": "fields.src_ip", "target": "fields.src_geo"}},
				{ID: "route-firewall", Type: string(plugin.TypeRouter), Plugin: "index-router", Version: "1.0.0", Config: map[string]any{"rules": []any{map[string]any{"when": "fields.action == 'deny'", "set": map[string]any{"metadata.index": "firewall"}, "add_tags": []any{"blocked"}}}}},
			},
			Outputs: []OutputSpec{{ID: "memory-search", Plugin: "memory-output", Version: "1.0.0", Config: map[string]any{"index": "firewall"}}},
		},
	}
}
