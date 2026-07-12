package mvp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/pipeline"
	"xdp/pkg/plugin"
	xdpruntime "xdp/pkg/runtime"
	clickhouse "xdp/pkg/storage/clickhouse"
	geoip "xdp/plugins/enrichment/geoip"
	memoryoutput "xdp/plugins/output/memory"
	propsconfparser "xdp/plugins/parser/propsconf"
	regexparser "xdp/plugins/parser/regex"
	typeconvert "xdp/plugins/transform/typeconvert"
)

func TestParseConfigAPIManagesRulesAndPreview(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	dataSource := createTestSyslogDataSource(t, handler, "Firewall Syslog")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/parser-plugins", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("parser plugins status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var plugins struct {
		Plugins []struct {
			PluginCode          string         `json:"plugin_code"`
			PluginType          string         `json:"plugin_type"`
			DisplayName         string         `json:"display_name"`
			Runtime             string         `json:"runtime"`
			Status              string         `json:"status"`
			Phase               string         `json:"phase"`
			SchemaSummary       map[string]any `json:"schema_summary"`
			ConfiguredCount     int            `json:"configured_count"`
			RuntimeCapabilities map[string]any `json:"runtime_capabilities"`
		} `json:"plugins"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&plugins); err != nil {
		t.Fatalf("decode plugins: %v", err)
	}
	if len(plugins.Plugins) != 1 {
		t.Fatalf("parser plugins = %#v, want regex only", plugins.Plugins)
	}
	regexPlugin := parserPluginByCode(plugins.Plugins, "regex")
	if regexPlugin == nil {
		t.Fatalf("regex parser plugin missing: %#v", plugins.Plugins)
	}
	if regexPlugin.Runtime != "go_builtin" || regexPlugin.Status != "active" {
		t.Fatalf("regex plugin runtime/status = %#v, want go_builtin active", regexPlugin)
	}
	if !stringSliceContains(anyStringSlice(regexPlugin.SchemaSummary["required"]), "sample_event") ||
		!stringSliceContains(anyStringSlice(regexPlugin.SchemaSummary["required"]), "regex_pattern") ||
		!stringSliceContains(anyStringSlice(regexPlugin.SchemaSummary["required"]), "props_conf") {
		t.Fatalf("regex plugin schema_summary = %#v, want required sample_event regex_pattern props_conf", regexPlugin.SchemaSummary)
	}
	for _, capability := range []string{"preview", "ingest_parse", "hot_reload", "props_conf_generate"} {
		if regexPlugin.RuntimeCapabilities[capability] != true {
			t.Fatalf("regex plugin capability %s = %#v, want true", capability, regexPlugin.RuntimeCapabilities[capability])
		}
	}
	for _, code := range []string{"json", "kv", "delimited"} {
		if parserPluginByCode(plugins.Plugins, code) != nil {
			t.Fatalf("%s parser plugin should not be exposed before plugin integration: %#v", code, plugins.Plugins)
		}
	}

	body := `{"name":"Firewall Regex","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"raw.ds_firewall_syslog","output_index":"audit","sample_event":"src=1.1.1.1 dst=8.8.8.8 action=deny bytes=1024","plugin_config":{"regex_pattern":"src=(?<src_ip>\\S+) dst=(?<dst_ip>\\S+) action=(?<action>\\S+) bytes=(?<bytes>\\d+)"},"hot_fields":[{"name":"src_ip","type":"string","searchable":true,"aggregatable":true,"aliases":["src"]},{"name":"bytes","type":"uint64","searchable":false,"aggregatable":true}],"props_conf":"[source::firewall]\nEXTRACT-firewall = src=(?<src_ip>\\S+) dst=(?<dst_ip>\\S+) action=(?<action>\\S+) bytes=(?<bytes>\\d+)"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create parse rule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	createdBody := rec.Body.Bytes()
	var created ParseRule
	if err := json.Unmarshal(createdBody, &created); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}
	if created.ID == "" || created.ParserPlugin != "regex" || created.Stage != "ingest" {
		t.Fatalf("created rule = %#v", created)
	}
	if created.InputRoute != "raw.ds_firewall_syslog" || !strings.Contains(created.PropsConf, "EXTRACT-firewall") {
		t.Fatalf("created rule route/props = %#v", created)
	}
	if len(created.HotFields) != 2 || created.HotFields[0].Name != "src_ip" || !created.HotFields[0].Searchable || created.HotFields[1].Type != "uint64" {
		t.Fatalf("created rule hot_fields = %#v", created.HotFields)
	}
	if created.PluginConfig["source_field"] != "raw" || created.PluginConfig["target"] != "fields" || created.PluginConfig["on_no_match"] != "continue" {
		t.Fatalf("created regex plugin_config = %#v, want standard defaults", created.PluginConfig)
	}
	var createdRaw map[string]any
	if err := json.Unmarshal(createdBody, &createdRaw); err != nil {
		t.Fatalf("decode created raw rule: %v", err)
	}
	if createdRaw["output_index"] != "audit" {
		t.Fatalf("created rule output_index = %#v, body = %s", createdRaw["output_index"], string(createdBody))
	}
	if strings.Contains(string(createdBody), "time_field") {
		t.Fatalf("parse rule response must not expose time_field: %s", string(createdBody))
	}
	if strings.Contains(string(createdBody), `"sourcetype":"syslog"`) {
		t.Fatalf("parse rule response must not echo legacy request sourcetype: %s", string(createdBody))
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/runtime/pipelines", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime pipelines status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var runtime struct {
		Pipelines []pipeline.Pipeline `json:"pipelines"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&runtime); err != nil {
		t.Fatalf("decode runtime pipelines: %v", err)
	}
	if !hasParseRuleStage(runtime.Pipelines, created.Code, "regex") {
		t.Fatalf("runtime pipelines missing parse rule stage for %s: %#v", created.Code, runtime.Pipelines)
	}
	if !pipelineSourceConfigEquals(runtime.Pipelines, dataSource.PipelineID, "source_name", "Firewall Syslog") {
		t.Fatalf("runtime pipelines missing source_name from datasource name: %#v", runtime.Pipelines)
	}
	if !parseRuleStageConfigEquals(runtime.Pipelines, created.Code, "sourcetype", "Firewall Regex") {
		t.Fatalf("runtime pipelines missing sourcetype from parse rule name: %#v", runtime.Pipelines)
	}
	if !parseRuleStageHasHotField(runtime.Pipelines, created.Code, "src_ip") {
		t.Fatalf("runtime pipelines missing hot_fields for %s: %#v", created.Code, runtime.Pipelines)
	}
	if got := pipelineOutputIndex(runtime.Pipelines, dataSource.PipelineID); got != "${metadata.index}" {
		t.Fatalf("runtime pipeline output index = %q, want ${metadata.index}", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/parse-rules?parser_plugin=regex&status=active", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list parse rules status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		ParseRules []ParseRule `json:"parse_rules"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode rule list: %v", err)
	}
	if len(list.ParseRules) != 1 || list.ParseRules[0].ID != created.ID {
		t.Fatalf("parse rule list = %#v, want created rule", list.ParseRules)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/parse-rules/"+created.ID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get parse rule status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules/"+created.ID+"/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("test parse rule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var preview struct {
		Success bool `json:"success"`
		Fields  []struct {
			Field string `json:"field"`
			Value string `json:"value"`
			Type  string `json:"type"`
		} `json:"fields"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !preview.Success || !hasPreviewField(preview.Fields, "src_ip", "1.1.1.1") || !hasPreviewField(preview.Fields, "bytes", "1024") {
		t.Fatalf("preview = %#v", preview)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/parse-rules/"+created.ID+"/status", strings.NewReader(`{"status":"disabled"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable parse rule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var disabled ParseRule
	if err := json.NewDecoder(rec.Body).Decode(&disabled); err != nil {
		t.Fatalf("decode disabled rule: %v", err)
	}
	if disabled.Status != "disabled" {
		t.Fatalf("disabled rule status = %q", disabled.Status)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/runtime/pipelines", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime pipelines after disable status = %d, body = %s", rec.Code, rec.Body.String())
	}
	runtime.Pipelines = nil
	if err := json.NewDecoder(rec.Body).Decode(&runtime); err != nil {
		t.Fatalf("decode runtime pipelines after disable: %v", err)
	}
	if hasParseRuleStage(runtime.Pipelines, created.Code, "regex") {
		t.Fatalf("disabled parse rule stage still present: %#v", runtime.Pipelines)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/parse-rules/"+created.ID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete parse rule status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/parse-rules/"+created.ID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusNotFound, "PARSE_RULE_NOT_FOUND")
}

func TestParseConfigAcceptsEnabledImportedJSONParser(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	createTestSyslogDataSource(t, handler, "JSON Syslog")
	importPluginForTest(t, handler, `{
		"plugin_code": "json-parser",
		"plugin_type": "parser",
		"plugin_version": "1.0.0",
		"name": "JSON Parser",
		"runtime": "go_builtin",
		"entrypoint": "builtin://plugins/parser/json",
		"config_schema": {
			"type": "object",
			"required": ["source_field", "target", "array_mode", "on_invalid_json"],
			"properties": {
				"source_field": {"type":"string","enum":["raw"],"default":"raw"},
				"target": {"type":"string","enum":["fields"],"default":"fields"},
				"flatten_nested": {"type":"boolean","default":true},
				"flatten_separator": {"type":"string","default":"."},
				"array_mode": {"type":"string","enum":["json_string","expand_index"],"default":"json_string"},
				"on_invalid_json": {"type":"string","enum":["continue","fail"],"default":"continue"}
			}
		},
		"ui_schema": {"order":["array_mode","on_invalid_json"]}
	}`)

	body := `{"name":"JSON Rule","status":"active","parser_plugin":"json-parser","parser_plugin_version":"1.0.0","data_source_name":"JSON Syslog","input_route":"raw.ds_json_syslog","output_index":"json_app","priority":10,"stage":"ingest","sample_event":"{\"service\":\"checkout\",\"latency\":128,\"user\":{\"id\":\"u-1\"}}","plugin_config":{"source_field":"raw","target":"fields","flatten_nested":true,"flatten_separator":".","array_mode":"json_string","on_invalid_json":"continue"},"props_conf":"[source::json-rule]\nINDEXED_EXTRACTIONS = json\nKV_MODE = none"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "PARSER_PLUGIN_NOT_ENABLED")

	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/json-parser/enable?plugin_type=parser", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable json parser status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules/preview/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview json rule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var preview struct {
		Success bool           `json:"success"`
		Fields  []PreviewField `json:"fields"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&preview); err != nil {
		t.Fatalf("decode json preview: %v", err)
	}
	if !preview.Success || !hasPreviewField(preview.Fields, "service", "checkout") || !hasPreviewField(preview.Fields, "user.id", "u-1") {
		t.Fatalf("json preview = %#v", preview)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create json rule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created ParseRule
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode json rule: %v", err)
	}
	if created.ParserPlugin != "json-parser" || created.ParserPluginVersion != "1.0.0" {
		t.Fatalf("created json rule = %#v", created)
	}
	if !hasParseRuleStage(handler.buildRuntimePipelines(), created.Code, "json-parser") {
		t.Fatalf("runtime pipelines missing json-parser stage: %#v", handler.buildRuntimePipelines())
	}
}

func TestParseConfigRejectsInvalidJSONParserConfig(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	createTestSyslogDataSource(t, handler, "JSON Syslog")
	importPluginForTest(t, handler, `{
		"plugin_code": "json-parser",
		"plugin_type": "parser",
		"plugin_version": "1.0.0",
		"name": "JSON Parser",
		"runtime": "go_builtin",
		"entrypoint": "builtin://plugins/parser/json",
		"config_schema": {
			"type": "object",
			"required": ["source_field", "target", "array_mode", "on_invalid_json"],
			"properties": {
				"source_field": {"type":"string","enum":["raw"],"default":"raw"},
				"target": {"type":"string","enum":["fields"],"default":"fields"},
				"array_mode": {"type":"string","enum":["json_string","expand_index"],"default":"json_string"},
				"on_invalid_json": {"type":"string","enum":["continue","fail"],"default":"continue"}
			}
		},
		"ui_schema": {"order":["array_mode","on_invalid_json"]}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/json-parser/enable?plugin_type=parser", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable json parser status = %d, body = %s", rec.Code, rec.Body.String())
	}

	body := `{"name":"JSON Rule","status":"active","parser_plugin":"json-parser","parser_plugin_version":"1.0.0","data_source_name":"JSON Syslog","input_route":"raw.ds_json_syslog","output_index":"json_app","priority":10,"stage":"ingest","sample_event":"{\"service\":\"checkout\"}","plugin_config":{"source_field":"raw","target":"fields","array_mode":"json","on_invalid_json":"parse_failed"},"props_conf":"[source::json-rule]\nINDEXED_EXTRACTIONS = json\nKV_MODE = none"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	responseBody := rec.Body.String()
	assertErrorResponse(t, rec, http.StatusBadRequest, "VALIDATION_ERROR")
	if !strings.Contains(responseBody, "plugin_config.array_mode") {
		t.Fatalf("error body = %s, want array_mode validation message", responseBody)
	}
}

func TestParseConfigUsesLatestPluginVersionForExistingAndNewRules(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	createTestSyslogDataSource(t, handler, "JSON Versioned Syslog")

	importJSONParserVersion := func(version string) {
		t.Helper()
		importPluginForTest(t, handler, fmt.Sprintf(`{
			"plugin_code": "json-parser",
			"plugin_type": "parser",
			"plugin_version": %q,
			"name": "JSON Parser",
			"runtime": "go_builtin",
			"entrypoint": "builtin://plugins/parser/json",
			"config_schema": {
				"type": "object",
				"required": ["source_field", "target", "array_mode", "on_invalid_json"],
				"properties": {
					"source_field": {"type":"string","default":"raw"},
					"target": {"type":"string","default":"fields"},
					"array_mode": {"type":"string","default":"json_string"},
					"on_invalid_json": {"type":"string","default":"continue"}
				}
			},
			"ui_schema": {}
		}`, version))
		if version == "1.0.0" {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/json-parser/enable?plugin_type=parser", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("enable json parser %s status = %d, body = %s", version, rec.Code, rec.Body.String())
			}
		}
	}

	createRule := func(name string) ParseRule {
		t.Helper()
		body := fmt.Sprintf(`{"name":%q,"status":"active","parser_plugin":"json-parser","data_source_name":"JSON Versioned Syslog","output_index":"json_app","sample_event":"{\"service\":\"checkout\"}","plugin_config":{"source_field":"raw","target":"fields","flatten_nested":true,"flatten_separator":".","array_mode":"json_string","on_invalid_json":"continue"},"props_conf":"[source::json]\nINDEXED_EXTRACTIONS = json\nKV_MODE = none"}`, name)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create rule %s status = %d, body = %s", name, rec.Code, rec.Body.String())
		}
		var rule ParseRule
		if err := json.NewDecoder(rec.Body).Decode(&rule); err != nil {
			t.Fatalf("decode rule %s: %v", name, err)
		}
		return rule
	}

	importJSONParserVersion("1.0.0")
	ruleV1 := createRule("JSON Rule V1")
	if ruleV1.ParserPluginVersion != "1.0.0" {
		t.Fatalf("rule v1 version = %q, want 1.0.0", ruleV1.ParserPluginVersion)
	}

	importJSONParserVersion("1.1.0")
	ruleV1AfterUpgrade, ok := handler.parseRules[ruleV1.ID]
	if !ok {
		t.Fatalf("existing rule missing after plugin upgrade")
	}
	stageV1AfterUpgrade := handler.parseRuleStage(ruleV1AfterUpgrade)
	if stageV1AfterUpgrade.Config["plugin_package_version"] != "1.1.0" {
		t.Fatalf("existing rule plugin_package_version = %#v, want latest 1.1.0", stageV1AfterUpgrade.Config["plugin_package_version"])
	}
	ruleV11 := createRule("JSON Rule V1.1")
	if ruleV11.ParserPluginVersion != "1.1.0" {
		t.Fatalf("new rule version = %q, want current enabled 1.1.0", ruleV11.ParserPluginVersion)
	}
	stageV11 := parseRuleStage(ruleV11)
	if stageV11.Version != "1.0.0" {
		t.Fatalf("json builtin runtime version = %q, want registered executor 1.0.0", stageV11.Version)
	}
	if stageV11.Config["plugin_package_version"] != "1.1.0" {
		t.Fatalf("plugin_package_version = %#v, want 1.1.0", stageV11.Config["plugin_package_version"])
	}

	storedV1, ok := handler.findParseRule(ruleV1.ID)
	if !ok {
		t.Fatalf("existing rule %s not found", ruleV1.ID)
	}
	if storedV1.ParserPluginVersion != "1.0.0" {
		t.Fatalf("existing rule version = %q, want unchanged 1.0.0", storedV1.ParserPluginVersion)
	}
}

func TestParseConfigAPIValidatesRequests(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "data source name is required",
			body:       `{"name":"Missing Source","status":"active","parser_plugin":"regex","input_route":"internal_raw_topic","output_index":"audit","sample_event":"src=1.1.1.1","plugin_config":{"regex_pattern":"src=(?<src_ip>\\S+)"},"props_conf":"[source::missing]\nEXTRACT-missing = src=(?<src_ip>\\S+)"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "output index is required",
			body:       `{"name":"Missing Index","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"internal_raw_topic","sample_event":"src=1.1.1.1","plugin_config":{"regex_pattern":"src=(?<src_ip>\\S+)"},"props_conf":"[source::missing-index]\nEXTRACT-missing-index = src=(?<src_ip>\\S+)"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "regex sample event is required",
			body:       `{"name":"Missing Sample","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"internal_raw_topic","output_index":"audit","plugin_config":{"regex_pattern":"src=(?<src_ip>\\S+)"},"props_conf":"[source::missing-sample]\nEXTRACT-missing-sample = src=(?<src_ip>\\S+)"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "props conf is required",
			body:       `{"name":"Missing Props","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"internal_raw_topic","output_index":"audit","sample_event":"src=1.1.1.1","plugin_config":{"regex_pattern":"src=(?<src_ip>\\S+)"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "invalid regex",
			body:       `{"name":"Bad Regex","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"internal_raw_topic","output_index":"audit","sample_event":"src=1.1.1.1","plugin_config":{"regex_pattern":"("},"props_conf":"[source::bad]\nEXTRACT-bad = ("}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "time field is rejected",
			body:       `{"name":"Bad Time Field","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"internal_raw_topic","output_index":"audit","time_field":"@timestamp","sample_event":"src=1.1.1.1","plugin_config":{"regex_pattern":"src=(?<src_ip>\\S+)"},"props_conf":"[source::bad-time]\nEXTRACT-bad-time = src=(?<src_ip>\\S+)"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "invalid output index is rejected",
			body:       `{"name":"Bad Index","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"internal_raw_topic","output_index":"Events-Firewall","sample_event":"src=1.1.1.1","plugin_config":{"regex_pattern":"src=(?<src_ip>\\S+)"},"props_conf":"[source::bad-index]\nEXTRACT-bad-index = src=(?<src_ip>\\S+)"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "system output index is rejected",
			body:       `{"name":"System Index","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"internal_raw_topic","output_index":"_unparsed","sample_event":"src=1.1.1.1","plugin_config":{"regex_pattern":"src=(?<src_ip>\\S+)"},"props_conf":"[source::system-index]\nEXTRACT-system-index = src=(?<src_ip>\\S+)"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "unsupported parser",
			body:       `{"name":"Unsupported","parser_plugin":"xml","input_route":"internal_raw_topic","plugin_config":{},"props_conf":"[source::xml]"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "PARSER_PLUGIN_NOT_SUPPORTED",
		},
		{
			name:       "delimited parser is not supported before plugin integration",
			body:       `{"name":"CSV","status":"active","parser_plugin":"delimited","data_source_name":"Firewall Syslog","input_route":"internal_raw_topic","output_index":"audit","plugin_config":{"field_delimiter":",","field_names":[]},"props_conf":"[sourcetype::csv]\nINDEXED_EXTRACTIONS = csv"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "PARSER_PLUGIN_NOT_SUPPORTED",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertErrorResponse(t, rec, tc.wantStatus, tc.wantCode)
		})
	}
}

func TestListParseRulesPaginatesResults(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	handler.mu.Lock()
	handler.parseRules = map[string]ParseRule{}
	for i := 1; i <= 25; i++ {
		id := fmt.Sprintf("rule-%02d", i)
		handler.parseRules[id] = ParseRule{
			ID:           id,
			Code:         id,
			Name:         fmt.Sprintf("Rule %02d", i),
			Status:       "active",
			ParserPlugin: "regex",
			OutputIndex:  "audit",
			Priority:     i,
			Stage:        "ingest",
		}
	}
	handler.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/parse-rules?page=3", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list parse rules status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		ParseRules []ParseRule `json:"parse_rules"`
		Pagination struct {
			Page       int `json:"page"`
			PageSize   int `json:"page_size"`
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode parse rule pagination: %v", err)
	}
	if len(response.ParseRules) != 5 {
		t.Fatalf("parse_rules len = %d, want 5", len(response.ParseRules))
	}
	if response.Pagination.Page != 3 || response.Pagination.PageSize != 10 || response.Pagination.Total != 25 || response.Pagination.TotalPages != 3 {
		t.Fatalf("pagination = %#v, want page=3 page_size=10 total=25 total_pages=3", response.Pagination)
	}
}

func TestParseConfigAPIDerivesInternalHotFieldsFromPreviewWhenMissing(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	createTestSyslogDataSource(t, handler, "Firewall Syslog")

	body := `{"name":"Firewall Regex","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"raw.ds_firewall_syslog","output_index":"audit","sample_event":"src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048","plugin_config":{"regex_pattern":"src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+action=(?<action>\\S+)\\s+bytes=(?<bytes>\\d+)"},"props_conf":"[source::firewall]\nEXTRACT-firewall = src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+action=(?<action>\\S+)\\s+bytes=(?<bytes>\\d+)"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create parse rule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created ParseRule
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}
	if !hasHotField(created.HotFields, "src_ip", "string", true, true) {
		t.Fatalf("created rule missing derived src_ip hot field: %#v", created.HotFields)
	}
	if !hasHotField(created.HotFields, "dst_ip", "string", true, true) {
		t.Fatalf("created rule missing derived dst_ip hot field: %#v", created.HotFields)
	}
	if !hasHotField(created.HotFields, "action", "low_cardinality_string", true, true) {
		t.Fatalf("created rule missing derived action hot field: %#v", created.HotFields)
	}
	if !hasHotField(created.HotFields, "bytes", "uint64", false, true) {
		t.Fatalf("created rule missing derived bytes hot field: %#v", created.HotFields)
	}
	if len(created.PreviewResult) == 0 || !hasPreviewField(created.PreviewResult, "src_ip", "10.0.1.8") {
		t.Fatalf("created rule preview_result = %#v", created.PreviewResult)
	}
}

func TestParseConfigDerivesValidHotFieldsWhenPreviewContainsNestedJSONFields(t *testing.T) {
	fields := []PreviewField{
		{Field: "level", Value: "warn", Type: "string"},
		{Field: "service", Value: "checkout", Type: "string"},
		{Field: "latency", Value: "128", Type: "number"},
		{Field: "user.id", Value: "u-1", Type: "string"},
		{Field: "user.geo.country", Value: "CN", Type: "string"},
	}

	hotFields := deriveInternalHotFieldsFromPreview(fields)

	if !hasHotField(hotFields, "level", "string", true, true) {
		t.Fatalf("derived hot fields missing level: %#v", hotFields)
	}
	if !hasHotField(hotFields, "service", "string", true, true) {
		t.Fatalf("derived hot fields missing service: %#v", hotFields)
	}
	if !hasHotField(hotFields, "latency", "uint64", false, true) {
		t.Fatalf("derived hot fields missing latency: %#v", hotFields)
	}
	if hasHotField(hotFields, "user.id", "string", true, true) || hasHotField(hotFields, "user.geo.country", "string", true, true) {
		t.Fatalf("nested JSON field names must not become ClickHouse columns: %#v", hotFields)
	}
}

func TestRuntimePipelineGroupsParseRulesBySourcePriority(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	dataSource := createTestSyslogDataSource(t, handler, "Firewall Syslog")

	ruleBodies := []string{
		`{"name":"Traffic Regex","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"raw.ds_firewall_syslog","output_index":"audit_alt","priority":20,"sample_event":"traffic src=10.0.1.8 dst=172.16.0.4 bytes=2048","plugin_config":{"regex_pattern":"^traffic\\s+src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+bytes=(?<bytes>\\d+)"},"props_conf":"[source::traffic]\nEXTRACT-traffic = ^traffic\\s+src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+bytes=(?<bytes>\\d+)"}`,
		`{"name":"Deny Regex","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"raw.ds_firewall_syslog","output_index":"audit_p0","priority":10,"sample_event":"deny src=10.0.1.8 action=deny","plugin_config":{"regex_pattern":"^deny\\s+src=(?<src_ip>\\S+)\\s+action=(?<action>\\S+)"},"props_conf":"[source::deny]\nEXTRACT-deny = ^deny\\s+src=(?<src_ip>\\S+)\\s+action=(?<action>\\S+)"}`,
	}
	for _, body := range ruleBodies {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create parse rule status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/pipelines", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime pipelines status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var runtime struct {
		Pipelines []pipeline.Pipeline `json:"pipelines"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&runtime); err != nil {
		t.Fatalf("decode runtime pipelines: %v", err)
	}
	group := parseRuleGroupStage(runtime.Pipelines, dataSource.PipelineID)
	if group == nil {
		t.Fatalf("runtime pipeline missing parse rule group: %#v", runtime.Pipelines)
	}
	if group.Type != "parser_group" {
		t.Fatalf("group type = %q, want parser_group", group.Type)
	}
	if len(group.Stages) != 2 {
		t.Fatalf("group stages = %#v, want 2 rules", group.Stages)
	}
	if group.Config["fallback_output_index"] != "_unparsed" {
		t.Fatalf("group fallback index = %#v, want _unparsed", group.Config["fallback_output_index"])
	}
	if group.Stages[0].Config["sourcetype"] != "Deny Regex" || group.Stages[0].Config["output_index"] != "audit_p0" {
		t.Fatalf("first grouped rule = %#v, want Deny Regex priority 10", group.Stages[0])
	}
	if group.Stages[1].Config["sourcetype"] != "Traffic Regex" || group.Stages[1].Config["output_index"] != "audit_alt" {
		t.Fatalf("second grouped rule = %#v, want Traffic Regex priority 20", group.Stages[1])
	}
	if got := pipelineOutputIndex(runtime.Pipelines, dataSource.PipelineID); got != "${metadata.index}" {
		t.Fatalf("runtime pipeline output index = %q, want ${metadata.index}", got)
	}
}

func TestRuntimePipelineParsesSecondRuleWhenFirstRuleMisses(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	dataSource := createTestSyslogDataSource(t, handler, "Firewall Syslog")

	for _, body := range []string{
		`{"name":"Deny Regex","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"raw.ds_firewall_syslog","output_index":"audit_p0","priority":10,"sample_event":"deny src=10.0.1.8 action=deny","plugin_config":{"regex_pattern":"^deny\\s+src=(?<src_ip>\\S+)\\s+action=(?<action>\\S+)"},"props_conf":"[source::deny]\nEXTRACT-deny = ^deny\\s+src=(?<src_ip>\\S+)\\s+action=(?<action>\\S+)"}`,
		`{"name":"Traffic Regex","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog","input_route":"raw.ds_firewall_syslog","output_index":"audit_alt","priority":20,"sample_event":"traffic src=10.0.1.8 dst=172.16.0.4 bytes=2048","plugin_config":{"regex_pattern":"^traffic\\s+src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+bytes=(?<bytes>\\d+)"},"props_conf":"[source::traffic]\nEXTRACT-traffic = ^traffic\\s+src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+bytes=(?<bytes>\\d+)"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create parse rule status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}

	runtimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/pipelines", nil)
	runtimeRec := httptest.NewRecorder()
	handler.ServeHTTP(runtimeRec, runtimeReq)
	if runtimeRec.Code != http.StatusOK {
		t.Fatalf("runtime pipelines status = %d, body = %s", runtimeRec.Code, runtimeRec.Body.String())
	}
	var runtime struct {
		Pipelines []pipeline.Pipeline `json:"pipelines"`
	}
	if err := json.NewDecoder(runtimeRec.Body).Decode(&runtime); err != nil {
		t.Fatalf("decode runtime pipelines: %v", err)
	}
	pipe := runtimePipelineByID(runtime.Pipelines, dataSource.PipelineID)
	if pipe == nil {
		t.Fatalf("missing syslog collection pipeline %s", dataSource.PipelineID)
	}
	reg := plugin.NewRegistry()
	mustRegisterPlugin(t, propsconfparser.Register(reg))
	mustRegisterPlugin(t, regexparser.Register(reg))
	mustRegisterPlugin(t, typeconvert.Register(reg))
	mustRegisterPlugin(t, geoip.Register(reg))
	mustRegisterPlugin(t, memoryoutput.Register(reg))
	ev := event.New("traffic src=10.0.1.8 dst=172.16.0.4 bytes=2048", event.Source{Type: "syslog"}, time.Now().UTC())
	result, err := xdpruntime.NewExecutor(reg).Execute(context.Background(), *pipe, ev)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Event.Metadata["parse_status"] != "parsed" {
		t.Fatalf("parse_status = %#v, want parsed", result.Event.Metadata["parse_status"])
	}
	if result.Event.Metadata["parse_rule_name"] != "Traffic Regex" {
		t.Fatalf("parse_rule_name = %#v, want Traffic Regex", result.Event.Metadata["parse_rule_name"])
	}
	if result.Event.Metadata["index"] != "audit_alt" {
		t.Fatalf("metadata.index = %#v, want audit_alt", result.Event.Metadata["index"])
	}
	if result.Event.Fields["src_ip"] != "10.0.1.8" || result.Event.Fields["bytes"] != "2048" {
		t.Fatalf("fields = %#v, want traffic fields", result.Event.Fields)
	}
	if len(result.Event.Errors) != 0 {
		t.Fatalf("errors = %#v, want none for first rule miss", result.Event.Errors)
	}
}

func TestParseConfigAPIRequiresAuthentication(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "true")
	t.Setenv("XDP_API_TOKEN", "test-token")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, path := range []string{"/api/v1/parser-plugins", "/api/v1/parse-rules"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assertErrorResponse(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")
	}
}

func runtimePipelineByID(items []pipeline.Pipeline, pipelineID string) *pipeline.Pipeline {
	for i := range items {
		if items[i].Metadata.ID == pipelineID {
			return &items[i]
		}
	}
	return nil
}

func createTestSyslogDataSource(t *testing.T, handler http.Handler, name string) DataSource {
	t.Helper()
	t.Setenv("XDP_AGENT_BASE_URL", "")
	var rec *httptest.ResponseRecorder
	for attempt := 0; attempt < 5; attempt++ {
		port := freeUDPPort(t)
		body := fmt.Sprintf(`{"name":%q,"plugin_code":"syslog","status":"active","plugin_config":{"collector_port":%d,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`, name, port)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			break
		}
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "LISTENER_PORT_UNAVAILABLE") {
			t.Fatalf("create test syslog datasource status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}
	if rec == nil || rec.Code != http.StatusOK {
		t.Fatalf("create test syslog datasource status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var source DataSource
	if err := json.NewDecoder(rec.Body).Decode(&source); err != nil {
		t.Fatalf("decode test syslog datasource: %v", err)
	}
	if source.PipelineID == "" {
		t.Fatalf("test syslog datasource missing pipeline_id: %#v", source)
	}
	return source
}

func parserPluginByCode[T any](plugins []T, code string) *T {
	for i := range plugins {
		data, _ := json.Marshal(plugins[i])
		var raw struct {
			PluginCode string `json:"plugin_code"`
		}
		if err := json.Unmarshal(data, &raw); err == nil && raw.PluginCode == code {
			return &plugins[i]
		}
	}
	return nil
}

func anyStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return []string{}
	}
}

func stringSliceContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func parseRuleGroupStage(items []pipeline.Pipeline, pipelineID string) *pipeline.StageSpec {
	for _, item := range items {
		if item.Metadata.ID != pipelineID {
			continue
		}
		for i := range item.Spec.Stages {
			if item.Spec.Stages[i].ID == "parse-rule-group" {
				return &item.Spec.Stages[i]
			}
		}
	}
	return nil
}

func mustRegisterPlugin(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func parseRuleStageHasHotField(pipes []pipeline.Pipeline, code string, field string) bool {
	for _, pipe := range pipes {
		for _, stage := range parseRuleStages(pipe.Spec.Stages) {
			if stage.ID != "parse-rule-"+code {
				continue
			}
			items, ok := stage.Config["hot_fields"].([]any)
			if !ok {
				return false
			}
			for _, raw := range items {
				item, ok := raw.(map[string]any)
				if ok && item["name"] == field {
					return true
				}
			}
		}
	}
	return false
}

func hasParserPlugin[T any](items []T, code string) bool {
	for _, item := range items {
		raw, _ := json.Marshal(item)
		var parsed struct {
			PluginCode  string `json:"plugin_code"`
			PluginType  string `json:"plugin_type"`
			DisplayName string `json:"display_name"`
		}
		_ = json.Unmarshal(raw, &parsed)
		if parsed.PluginCode == code && parsed.PluginType == "parser" && parsed.DisplayName != "" {
			return true
		}
	}
	return false
}

func hasPreviewField[T interface {
	~struct {
		Field string `json:"field"`
		Value string `json:"value"`
		Type  string `json:"type"`
	}
}](items []T, field string, value string) bool {
	for _, item := range items {
		raw, _ := json.Marshal(item)
		var parsed struct {
			Field string `json:"field"`
			Value string `json:"value"`
		}
		_ = json.Unmarshal(raw, &parsed)
		if parsed.Field == field && parsed.Value == value {
			return true
		}
	}
	return false
}

func hasHotField(items []clickhouse.HotField, name string, fieldType string, searchable bool, aggregatable bool) bool {
	for _, item := range items {
		if item.Name == name && item.Type == fieldType && item.Searchable == searchable && item.Aggregatable == aggregatable {
			return true
		}
	}
	return false
}

func hasParseRuleStage(items []pipeline.Pipeline, code string, plugin string) bool {
	stageID := "parse-rule-" + code
	for _, item := range items {
		for _, stage := range parseRuleStages(item.Spec.Stages) {
			if stage.ID == stageID && stage.Plugin == plugin && stage.Type == "parser" {
				return true
			}
		}
	}
	return false
}

func parseRuleStageConfigEquals(items []pipeline.Pipeline, code string, key string, value string) bool {
	stageID := "parse-rule-" + code
	for _, item := range items {
		for _, stage := range parseRuleStages(item.Spec.Stages) {
			if stage.ID == stageID && stage.Config != nil && stage.Config[key] == value {
				return true
			}
		}
	}
	return false
}

func parseRuleStages(stages []pipeline.StageSpec) []pipeline.StageSpec {
	out := make([]pipeline.StageSpec, 0, len(stages))
	for _, stage := range stages {
		if stage.Type == "parser_group" {
			out = append(out, stage.Stages...)
			continue
		}
		out = append(out, stage)
	}
	return out
}

func pipelineSourceConfigEquals(items []pipeline.Pipeline, id string, key string, value string) bool {
	for _, item := range items {
		if item.Metadata.ID == id && item.Spec.Source.Config != nil && item.Spec.Source.Config[key] == value {
			return true
		}
	}
	return false
}

func pipelineOutputIndex(items []pipeline.Pipeline, id string) string {
	for _, item := range items {
		if item.Metadata.ID != id {
			continue
		}
		for _, output := range item.Spec.Outputs {
			if output.Config == nil {
				continue
			}
			if value, ok := output.Config["index"].(string); ok {
				return value
			}
		}
	}
	return ""
}
