package mvp

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"xdp/pkg/plugin"
	"xdp/pkg/search/splquery"
	mysqlstore "xdp/pkg/storage/mysql"
)

func TestPluginManagementAPIRequiresTypeAndPaginatesCurrentType(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 1; i <= 12; i++ {
		importPluginForTest(t, handler, fmt.Sprintf(`{
			"plugin_code": "demo-input-%02d",
			"plugin_type": "input",
			"plugin_version": "1.0.0",
			"name": "Demo Input %02d",
			"runtime": "go_builtin",
			"config_schema": {"type":"object","properties":{}},
			"ui_schema": {}
		}`, i, i))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins?page=1&page_size=10", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusBadRequest, "PLUGIN_TYPE_REQUIRED")

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins?plugin_type=input&page=2&page_size=10", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Plugins    []PluginImportResponse `json:"plugins"`
		Pagination struct {
			Page       int `json:"page"`
			PageSize   int `json:"page_size"`
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		} `json:"pagination"`
		TypeCounts map[string]int `json:"type_counts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode plugin page: %v", err)
	}
	if body.Pagination.Page != 2 || body.Pagination.PageSize != 10 || body.Pagination.Total != 13 || body.Pagination.TotalPages != 2 {
		t.Fatalf("pagination = %#v", body.Pagination)
	}
	if len(body.Plugins) != 3 {
		t.Fatalf("page plugins length = %d, plugins = %#v", len(body.Plugins), body.Plugins)
	}
	if body.TypeCounts["input"] != 13 || body.TypeCounts["parser"] != 1 || body.TypeCounts["search_command"] != 1 {
		t.Fatalf("type_counts = %#v", body.TypeCounts)
	}
}

func TestPluginCatalogReturnsAllEnabledCurrentPluginsWithoutPagination(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 1; i <= 12; i++ {
		importPluginForTest(t, handler, fmt.Sprintf(`{
			"plugin_code": "catalog-input-%02d",
			"plugin_type": "input",
			"plugin_version": "1.0.0",
			"name": "Catalog Input %02d",
			"runtime": "go_builtin",
			"config_schema": {"type":"object","properties":{}},
			"ui_schema": {}
		}`, i, i))
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/plugins/catalog-input-%02d/enable?plugin_type=input", i), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("enable catalog plugin %d status = %d, body = %s", i, rec.Code, rec.Body.String())
		}
	}
	importPluginForTest(t, handler, `{
		"plugin_code": "catalog-disabled",
		"plugin_type": "input",
		"plugin_version": "1.0.0",
		"name": "Catalog Disabled",
		"runtime": "go_builtin",
		"config_schema": {"type":"object","properties":{}},
		"ui_schema": {}
	}`)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/catalog?plugin_type=input&status=enabled", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Plugins []PluginImportResponse `json:"plugins"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode plugin catalog: %v", err)
	}
	if len(body.Plugins) != 13 {
		t.Fatalf("catalog plugins length = %d, plugins = %#v", len(body.Plugins), body.Plugins)
	}
	var foundLast bool
	for _, item := range body.Plugins {
		if item.PluginCode == "catalog-disabled" {
			t.Fatalf("disabled plugin leaked into enabled catalog: %#v", item)
		}
		if item.PluginCode == "catalog-input-12" {
			foundLast = true
		}
	}
	if !foundLast {
		t.Fatalf("catalog missing plugin beyond management first page: %#v", body.Plugins)
	}
}

func TestPluginCatalogExposesEnabledImportedJSONParser(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
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
		"ui_schema": {
			"order": ["array_mode", "on_invalid_json"],
			"hidden": ["source_field", "target", "flatten_nested", "flatten_separator"]
		}
	}`)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/catalog?plugin_type=parser&status=enabled", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disabled catalog status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"plugin_code":"json-parser"`) {
		t.Fatalf("disabled json parser leaked into catalog: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/json-parser/enable?plugin_type=parser", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable json parser status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/catalog?plugin_type=parser&status=enabled", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enabled catalog status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Plugins []PluginImportResponse `json:"plugins"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode parser catalog: %v", err)
	}
	var found *PluginImportResponse
	for i := range body.Plugins {
		if body.Plugins[i].PluginCode == "json-parser" {
			found = &body.Plugins[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("enabled json parser missing from catalog: %#v", body.Plugins)
	}
	if found.PluginType != "parser" || found.PluginVersion != "1.0.0" || found.Status != "enabled" {
		t.Fatalf("json parser catalog item = %#v", found)
	}
}

func TestPluginManagementAPIExposesCurrentDetailSchemaAndStateChanges(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	importPluginForTest(t, handler, `{
		"plugin_code": "demo-kafka",
		"plugin_type": "input",
		"plugin_version": "1.0.0",
		"name": "Demo Kafka",
		"runtime": "go_builtin",
		"config_schema": {"type":"object","properties":{"brokers":{"type":"string"}},"required":["brokers"]},
		"ui_schema": {"order":["brokers"]}
	}`)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/demo-kafka?plugin_type=input", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		PluginCode    string `json:"plugin_code"`
		PluginType    string `json:"plugin_type"`
		PluginVersion string `json:"plugin_version"`
		Status        string `json:"status"`
		References    struct {
			Count int `json:"count"`
		} `json:"references"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.PluginCode != "demo-kafka" || detail.PluginType != "input" || detail.PluginVersion != "1.0.0" || detail.Status != "disabled" || detail.References.Count != 0 {
		t.Fatalf("detail = %#v", detail)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/demo-kafka/schema?plugin_type=input", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("schema status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var schema struct {
		ConfigSchema map[string]any `json:"config_schema"`
		UISchema     map[string]any `json:"ui_schema"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	if schema.ConfigSchema["type"] != "object" || len(schema.UISchema["order"].([]any)) != 1 {
		t.Fatalf("schema = %#v", schema)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/demo-kafka/enable?plugin_type=input", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var enabled PluginImportResponse
	if err := json.NewDecoder(rec.Body).Decode(&enabled); err != nil {
		t.Fatalf("decode enabled: %v", err)
	}
	if enabled.Status != "enabled" {
		t.Fatalf("enabled status = %q", enabled.Status)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/demo-kafka/disable?plugin_type=input", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var disabled PluginImportResponse
	if err := json.NewDecoder(rec.Body).Decode(&disabled); err != nil {
		t.Fatalf("decode disabled: %v", err)
	}
	if disabled.Status != "disabled" {
		t.Fatalf("disabled status = %q", disabled.Status)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/plugins/demo-kafka?plugin_type=input", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/demo-kafka?plugin_type=input", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusNotFound, "PLUGIN_NOT_FOUND")
}

func TestPluginExecutionAuditsAPIExposesRecentSearchCommandRuns(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	importPluginForTest(t, handler, `{
		"plugin_code": "table",
		"plugin_type": "search_command",
		"plugin_version": "1.0.0",
		"name": "Table Command",
		"runtime": "declarative_search_command",
		"config_schema": {"type":"object","properties":{}},
		"ui_schema": {},
		"runtime_config": {"operation":"table"}
	}`)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/table/execution-audits?plugin_type=search_command&limit=20", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("execution audit status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		PluginCode string           `json:"plugin_code"`
		PluginType string           `json:"plugin_type"`
		Audits     []map[string]any `json:"audits"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode execution audits: %v", err)
	}
	if body.PluginCode != "table" || body.PluginType != "search_command" || len(body.Audits) != 0 {
		t.Fatalf("execution audits body = %#v", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/table/execution-audits?plugin_type=input", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusBadRequest, "PLUGIN_TYPE_UNSUPPORTED")

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/stats/execution-audits?plugin_type=search_command", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusConflict, "BUILTIN_PLUGIN_PROTECTED")
}

func TestPluginManagementAPIProtectsBuiltInAndInUsePlugins(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, tc := range []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{name: "enable built-in input", method: http.MethodPost, path: "/api/v1/plugins/syslog/enable?plugin_type=input", wantStatus: http.StatusConflict, wantCode: "BUILTIN_PLUGIN_PROTECTED"},
		{name: "disable built-in parser", method: http.MethodPost, path: "/api/v1/plugins/regex/disable?plugin_type=parser", wantStatus: http.StatusConflict, wantCode: "BUILTIN_PLUGIN_PROTECTED"},
		{name: "delete built-in search command", method: http.MethodDelete, path: "/api/v1/plugins/stats?plugin_type=search_command", wantStatus: http.StatusConflict, wantCode: "BUILTIN_PLUGIN_PROTECTED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertErrorResponse(t, rec, tc.wantStatus, tc.wantCode)
		})
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/plugins/syslog?plugin_type=input", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusConflict, "BUILTIN_PLUGIN_PROTECTED")

	handler.(*Handler).dataSources["syslog-default"] = DataSource{ID: "syslog-default", Name: "Syslog Default", PluginCode: "syslog", Status: "active"}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/syslog/disable?plugin_type=input", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusConflict, "BUILTIN_PLUGIN_PROTECTED")

	importPluginForTest(t, handler, `{
		"plugin_code": "demo-kafka",
		"plugin_type": "input",
		"plugin_version": "1.0.0",
		"name": "Demo Kafka",
		"runtime": "go_builtin",
		"config_schema": {"type":"object","properties":{"brokers":{"type":"string"}}},
		"ui_schema": {}
	}`)
	handler.(*Handler).dataSources["ds-1"] = DataSource{ID: "ds-1", Name: "Kafka Source", PluginCode: "demo-kafka", Status: "active"}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/demo-kafka/enable?plugin_type=input", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/demo-kafka/disable?plugin_type=input", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusConflict, "PLUGIN_IN_USE")
}

func TestParserPluginReferenceProtectionUsesPluginCode(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	importPluginForTest(t, handler, `{
		"plugin_code": "json-parser",
		"plugin_type": "parser",
		"plugin_version": "1.0.0",
		"name": "JSON Parser",
		"runtime": "go_builtin",
		"entrypoint": "builtin://plugins/parser/json",
		"config_schema": {"type":"object","properties":{"source_field":{"type":"string"}}},
		"ui_schema": {}
	}`)
	handler.parseRules["rule-v1"] = ParseRule{
		ID:                  "rule-v1",
		Name:                "JSON Rule V1",
		Status:              "active",
		ParserPlugin:        "json-parser",
		ParserPluginVersion: "1.0.0",
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/plugins/json-parser?plugin_type=parser", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusConflict, "PLUGIN_IN_USE")
}

func TestPluginManagementAPIExposesExternalDetailsForAllProductPluginTypes(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	importPluginForTest(t, handler, `{
		"plugin_code": "demo-kafka",
		"plugin_type": "input",
		"plugin_version": "1.0.0",
		"name": "Demo Kafka",
		"runtime": "go_builtin",
		"config_schema": {"type":"object","properties":{"brokers":{"type":"string"}}},
		"ui_schema": {"order":["brokers"]}
	}`)
	importPluginForTest(t, handler, `{
		"plugin_code": "vendor-fw",
		"plugin_type": "parser",
		"plugin_version": "2.0.0",
		"name": "Vendor Firewall Parser",
		"runtime": "go_plugin",
		"config_schema": {"type":"object","properties":{"regex_pattern":{"type":"string"}}},
		"ui_schema": {"order":["regex_pattern"]}
	}`)
	importPluginForTest(t, handler, `{
		"plugin_code": "table",
		"plugin_type": "search_command",
		"plugin_version": "1.0.0",
		"name": "Table Command",
		"runtime": "go_plugin",
		"config_schema": {"type":"object","properties":{"fields":{"type":"array"}}},
		"ui_schema": {"order":["fields"]}
	}`)
	h := handler.(*Handler)
	h.dataSources["ds-kafka"] = DataSource{ID: "ds-kafka", Name: "Kafka Source", PluginCode: "demo-kafka", Status: "active"}
	h.parseRules["rule-fw"] = ParseRule{ID: "rule-fw", Name: "Vendor FW", ParserPlugin: "vendor-fw", Status: "active"}
	h.savedSearches["table-search"] = mysqlstore.SavedSearch{ID: "table-search", Name: "Table search", SPL: "index=app | table host service", Status: "active"}

	for _, tc := range []struct {
		code       string
		pluginType string
		version    string
		schemaKey  string
		refs       int
		refType    string
		refName    string
	}{
		{code: "demo-kafka", pluginType: "input", version: "1.0.0", schemaKey: "brokers", refs: 1, refType: "datasource", refName: "Kafka Source"},
		{code: "vendor-fw", pluginType: "parser", version: "2.0.0", schemaKey: "regex_pattern", refs: 1, refType: "parse_rule", refName: "Vendor FW"},
		{code: "table", pluginType: "search_command", version: "1.0.0", schemaKey: "fields", refs: 1, refType: "saved_search", refName: "Table search"},
	} {
		t.Run(tc.pluginType, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/"+tc.code+"?plugin_type="+tc.pluginType, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("detail status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var detail struct {
				PluginCode    string         `json:"plugin_code"`
				PluginType    string         `json:"plugin_type"`
				PluginVersion string         `json:"plugin_version"`
				BuiltIn       bool           `json:"built_in"`
				ConfigSchema  map[string]any `json:"config_schema"`
				UISchema      map[string]any `json:"ui_schema"`
				References    struct {
					Count int `json:"count"`
					Items []struct {
						Type   string `json:"type"`
						ID     string `json:"id"`
						Name   string `json:"name"`
						Status string `json:"status"`
					} `json:"items"`
				} `json:"references"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
				t.Fatalf("decode detail: %v", err)
			}
			if detail.PluginCode != tc.code || detail.PluginType != tc.pluginType || detail.PluginVersion != tc.version || detail.BuiltIn {
				t.Fatalf("detail identity = %#v", detail)
			}
			properties, _ := detail.ConfigSchema["properties"].(map[string]any)
			if _, ok := properties[tc.schemaKey]; !ok {
				t.Fatalf("config_schema missing %s: %#v", tc.schemaKey, detail.ConfigSchema)
			}
			if len(detail.UISchema) == 0 {
				t.Fatalf("ui_schema should be present: %#v", detail)
			}
			if detail.References.Count != tc.refs {
				t.Fatalf("references.count = %d, want %d", detail.References.Count, tc.refs)
			}
			if len(detail.References.Items) != tc.refs {
				t.Fatalf("references.items length = %d, want %d: %#v", len(detail.References.Items), tc.refs, detail.References.Items)
			}
			if detail.References.Items[0].Type != tc.refType || detail.References.Items[0].Name != tc.refName || detail.References.Items[0].Status == "" {
				t.Fatalf("references.items[0] = %#v, want type=%s name=%s with status", detail.References.Items[0], tc.refType, tc.refName)
			}

		})
	}
}

func TestPluginManagementAPIProtectsSearchCommandPluginsUsedBySavedSearches(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	importPluginForTest(t, handler, `{
		"plugin_code": "table",
		"plugin_type": "search_command",
		"plugin_version": "1.0.0",
		"name": "Table Command",
		"runtime": "go_builtin",
		"config_schema": {"type":"object","properties":{}},
		"ui_schema": {}
	}`)
	h := handler.(*Handler)
	h.savedSearches["table-search"] = mysqlstore.SavedSearch{
		ID:     "table-search",
		Name:   "Table search",
		SPL:    "index=app | table host service",
		Status: "active",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/table?plugin_type=search_command", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		References struct {
			Count int `json:"count"`
			Items []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"items"`
		} `json:"references"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.References.Count != 1 {
		t.Fatalf("references.count = %d, want 1", detail.References.Count)
	}
	if len(detail.References.Items) != 1 || detail.References.Items[0].Type != "saved_search" || detail.References.Items[0].Name != "Table search" {
		t.Fatalf("references.items = %#v", detail.References.Items)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/table/enable?plugin_type=search_command", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/table/disable?plugin_type=search_command", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusConflict, "PLUGIN_IN_USE")

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/plugins/table?plugin_type=search_command", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusConflict, "PLUGIN_IN_USE")
}

func TestPluginManagementAPIDeduplicatesBuiltInPluginDetail(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := handler.(*Handler)
	h.importedPlugins[pluginKey("input", "syslog", "1.0.0")] = PluginImportResponse{
		PluginCode:    "syslog",
		PluginType:    "input",
		PluginVersion: "1.0.0",
		Name:          "Syslog duplicate",
		Runtime:       "go_builtin",
		Status:        "active",
		Checksum:      "legacy-duplicate",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/syslog?plugin_type=input", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body PluginImportResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if body.PluginCode != "syslog" || body.PluginVersion != "1.0.0" || body.Status != "enabled" {
		t.Fatalf("builtin detail = %#v", body)
	}
}

func TestPluginManagementAPIDeduplicatesDisplayEquivalentPluginDetail(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := handler.(*Handler)
	h.importedPlugins[pluginKey("input", "syslog", "")] = PluginImportResponse{
		PluginCode:    " syslog ",
		PluginType:    "collect",
		PluginVersion: "",
		Name:          "Syslog legacy empty version",
		Runtime:       "go_builtin",
		Status:        "active",
		Checksum:      "legacy-empty-version",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/syslog?plugin_type=input", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body PluginImportResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if body.PluginCode != "syslog" || body.PluginType != "input" || body.PluginVersion != "1.0.0" {
		t.Fatalf("normalized builtin detail = %#v", body)
	}
}

func TestPluginImportRejectsInvalidPackagesWithSpecificErrorCodes(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	cases := []struct {
		name       string
		body       []byte
		wantStatus int
		wantCode   string
	}{
		{name: "empty", body: nil, wantStatus: http.StatusBadRequest, wantCode: "PLUGIN_PACKAGE_EMPTY"},
		{name: "not zip", body: []byte("not zip"), wantStatus: http.StatusBadRequest, wantCode: "PLUGIN_PACKAGE_INVALID"},
		{name: "missing manifest", body: pluginZipWithoutManifestForTest(t), wantStatus: http.StatusBadRequest, wantCode: "PLUGIN_MANIFEST_MISSING"},
		{name: "bad manifest json", body: pluginZipBody(t, `{`), wantStatus: http.StatusBadRequest, wantCode: "PLUGIN_MANIFEST_INVALID"},
		{name: "unsupported type", body: pluginZipBody(t, `{"plugin_code":"lower","plugin_type":"spl_function","plugin_version":"1.0.0"}`), wantStatus: http.StatusBadRequest, wantCode: "PLUGIN_TYPE_UNSUPPORTED"},
		{name: "manifest in subdir is ignored", body: pluginZipBodyAtPath(t, "nested/manifest.json", `{"plugin_code":"nested","plugin_type":"input","plugin_version":"1.0.0"}`), wantStatus: http.StatusBadRequest, wantCode: "PLUGIN_MANIFEST_MISSING"},
		{name: "invalid plugin code", body: pluginZipBody(t, `{"plugin_code":"Bad Code","plugin_type":"input","plugin_version":"1.0.0","runtime":"go_builtin","config_schema":{"type":"object","properties":{}},"ui_schema":{}}`), wantStatus: http.StatusBadRequest, wantCode: "PLUGIN_CODE_INVALID"},
		{name: "builtin code protected", body: pluginZipBody(t, `{"plugin_code":"syslog","plugin_type":"input","plugin_version":"2.0.0","runtime":"go_builtin","config_schema":{"type":"object","properties":{}},"ui_schema":{}}`), wantStatus: http.StatusConflict, wantCode: "BUILTIN_PLUGIN_PROTECTED"},
		{name: "unsupported runtime", body: pluginZipBody(t, `{"plugin_code":"bad-runtime","plugin_type":"input","plugin_version":"1.0.0","runtime":"python","config_schema":{"type":"object","properties":{}},"ui_schema":{}}`), wantStatus: http.StatusBadRequest, wantCode: "PLUGIN_RUNTIME_UNSUPPORTED"},
		{name: "incompatible platform version", body: pluginZipBody(t, `{"plugin_code":"future-plugin","plugin_type":"input","plugin_version":"1.0.0","runtime":"go_builtin","min_platform_version":"999.0.0","config_schema":{"type":"object","properties":{}},"ui_schema":{}}`), wantStatus: http.StatusBadRequest, wantCode: "PLUGIN_PLATFORM_INCOMPATIBLE"},
		{name: "bad schema", body: pluginZipBody(t, `{"plugin_code":"bad-schema","plugin_type":"input","plugin_version":"1.0.0","config_schema":{"type":"object","properties":{"host":{"type":"string"}}},"ui_schema":{"order":["missing"]}}`), wantStatus: http.StatusBadRequest, wantCode: "PLUGIN_SCHEMA_INVALID"},
		{name: "required references unknown field", body: pluginZipBody(t, `{"plugin_code":"bad-required","plugin_type":"input","plugin_version":"1.0.0","runtime":"go_builtin","config_schema":{"type":"object","required":["missing"],"properties":{"host":{"type":"string"}}},"ui_schema":{"order":["host"]}}`), wantStatus: http.StatusBadRequest, wantCode: "PLUGIN_SCHEMA_INVALID"},
		{name: "sensitive field not marked", body: pluginZipBody(t, `{"plugin_code":"bad-secret","plugin_type":"input","plugin_version":"1.0.0","runtime":"go_builtin","config_schema":{"type":"object","properties":{"api_token":{"type":"string"}}},"ui_schema":{"order":["api_token"]}}`), wantStatus: http.StatusBadRequest, wantCode: "PLUGIN_SCHEMA_INVALID"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/import?plugin_type=input", bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/zip")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertErrorResponse(t, rec, tc.wantStatus, tc.wantCode)
		})
	}
}

func TestPluginImportRequiresOverwriteForSameOrLowerVersionAndReplacesWhenConfirmed(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	manifest := `{
		"plugin_code": "dup-kafka",
		"plugin_type": "input",
		"plugin_version": "1.0.0",
		"name": "Duplicate Kafka",
		"runtime": "go_builtin",
		"config_schema": {"type":"object","properties":{"brokers":{"type":"array","items":{"type":"string"}}}},
		"ui_schema": {"order":["brokers"]}
	}`
	importPluginForTest(t, handler, manifest)

	for _, version := range []string{"1.0.0", "0.9.0"} {
		body := strings.Replace(manifest, `"plugin_version": "1.0.0"`, fmt.Sprintf(`"plugin_version": %q`, version), 1)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/import?plugin_type=input", bytes.NewReader(pluginZipBody(t, body)))
		req.Header.Set("Content-Type", "application/zip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assertErrorResponse(t, rec, http.StatusConflict, "PLUGIN_ALREADY_EXISTS")

		req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/import?overwrite=true", bytes.NewReader(pluginZipBody(t, body)))
		req.Header.Set("Content-Type", "application/zip")
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("overwrite version %s status = %d, body = %s", version, rec.Code, rec.Body.String())
		}
		var overwritten PluginImportResponse
		if err := json.NewDecoder(rec.Body).Decode(&overwritten); err != nil {
			t.Fatalf("decode overwritten plugin: %v", err)
		}
		if overwritten.PluginVersion != version {
			t.Fatalf("overwritten version = %s, want %s", overwritten.PluginVersion, version)
		}
	}

	upgradedManifest := strings.Replace(manifest, `"plugin_version": "1.0.0"`, `"plugin_version": "1.1.0"`, 1)
	upgradedManifest = strings.Replace(upgradedManifest, `"name": "Duplicate Kafka"`, `"name": "Upgraded Kafka"`, 1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/import?plugin_type=input", bytes.NewReader(pluginZipBody(t, upgradedManifest)))
	req.Header.Set("Content-Type", "application/zip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upgrade status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/dup-kafka?plugin_type=input", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var current PluginImportResponse
	if err := json.NewDecoder(rec.Body).Decode(&current); err != nil {
		t.Fatalf("decode current plugin: %v", err)
	}
	if current.PluginVersion != "1.1.0" || current.Name != "Upgraded Kafka" {
		t.Fatalf("current plugin = %#v", current)
	}
	if got := len(handler.(*Handler).pluginVersions("input", "dup-kafka")); got != 1 {
		t.Fatalf("stored current plugin count = %d, want 1", got)
	}
}

func TestPluginImportAcceptsLegacyManifestAliasesButReturnsStandardFields(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/import?plugin_type=input", bytes.NewReader(pluginZipBody(t, `{
		"code": "legacy-kafka",
		"type": "input",
		"version": "1.2.3",
		"display_name": "Legacy Kafka",
		"runtime": "go_builtin",
		"config_schema": {"type":"object","properties":{"brokers":{"type":"array","items":{"type":"string"}}}},
		"ui_schema": {"order":["brokers"]}
	}`)))
	req.Header.Set("Content-Type", "application/zip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var imported PluginImportResponse
	if err := json.NewDecoder(rec.Body).Decode(&imported); err != nil {
		t.Fatalf("decode imported: %v", err)
	}
	if imported.PluginCode != "legacy-kafka" || imported.PluginType != "input" || imported.PluginVersion != "1.2.3" || imported.Name != "Legacy Kafka" {
		t.Fatalf("imported = %#v", imported)
	}
}

func TestKafkaInputPluginSampleManifestIsImportable(t *testing.T) {
	data, err := os.ReadFile("../../../../docs/plugins/kafka-input-sample/manifest.json")
	if err != nil {
		t.Fatalf("read kafka sample manifest: %v", err)
	}
	manifest, err := parsePluginManifest(pluginZipBody(t, string(data)))
	if err != nil {
		t.Fatalf("parse kafka sample manifest: %v", err)
	}
	if manifest.PluginCode != "kafka" || normalizePluginType(manifest.PluginType) != "input" || manifest.version() != "1.0.0" {
		t.Fatalf("kafka sample manifest identity = %#v", manifest)
	}
	if _, ok := manifest.ConfigSchema["properties"].(map[string]any)["brokers"]; !ok {
		t.Fatalf("kafka sample config_schema missing brokers: %#v", manifest.ConfigSchema)
	}
}

func TestJSONParserSampleManifestIsImportable(t *testing.T) {
	data, err := os.ReadFile("../../../../docs/plugins/json-parser-sample/manifest.json")
	if err != nil {
		t.Fatalf("read json parser sample manifest: %v", err)
	}
	manifest, err := parsePluginManifest(pluginZipBody(t, string(data)))
	if err != nil {
		t.Fatalf("parse json parser sample manifest: %v", err)
	}
	if manifest.PluginCode != "json-parser" || normalizePluginType(manifest.PluginType) != "parser" || manifest.version() != "1.0.0" {
		t.Fatalf("json parser sample manifest identity = %#v", manifest)
	}
	properties, _ := manifest.ConfigSchema["properties"].(map[string]any)
	for _, field := range []string{"source_field", "target", "flatten_nested", "flatten_separator", "array_mode", "on_invalid_json"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("json parser sample config_schema missing %s: %#v", field, manifest.ConfigSchema)
		}
	}
}

func TestSearchCommandSampleManifestsCarryExecutableRuntime(t *testing.T) {
	cases := map[string]string{
		"table-search-command-sample": "bin/table_command.py",
		"sort-search-command-sample":  "bin/sort_command.py",
		"head-search-command-sample":  "bin/head_command.py",
		"dedup-search-command-sample": "bin/dedup_command.py",
	}
	for dir, entrypoint := range cases {
		t.Run(dir, func(t *testing.T) {
			data, err := os.ReadFile("../../../../docs/plugins/" + dir + "/manifest.json")
			if err != nil {
				t.Fatalf("read search command sample manifest: %v", err)
			}
			manifest, err := parsePluginManifest(pluginZipBody(t, string(data)))
			if err != nil {
				t.Fatalf("parse search command sample manifest: %v", err)
			}
			if normalizePluginType(manifest.PluginType) != "search_command" {
				t.Fatalf("plugin_type = %q, want search_command", manifest.PluginType)
			}
			if manifest.Runtime != "executable_search_command" {
				t.Fatalf("runtime = %q, want executable_search_command", manifest.Runtime)
			}
			if manifest.Entrypoint != entrypoint {
				t.Fatalf("entrypoint = %q, want %q", manifest.Entrypoint, entrypoint)
			}
			if got := fmt.Sprint(manifest.RuntimeConfig["interpreter"]); got != "python3" {
				t.Fatalf("runtime_config.interpreter = %q, want python3", got)
			}
			packageBody := pluginZipFromDir(t, "../../../../docs/plugins/"+dir)
			if err := validatePluginPackageAssets(packageBody, manifest); err != nil {
				t.Fatalf("validate executable package assets: %v", err)
			}
		})
	}
}

func TestExecutableSearchCommandPluginPreparesRuntimeOnEnableAndExecutesPreparedScript(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_PLUGIN_RUNTIME_DIR", t.TempDir())

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := pluginZipFromDir(t, "../../../../docs/plugins/table-search-command-sample")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/import?plugin_type=search_command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/zip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import table status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var imported PluginImportResponse
	if err := json.NewDecoder(rec.Body).Decode(&imported); err != nil {
		t.Fatalf("decode imported plugin: %v", err)
	}
	runtimeDir, err := executablePluginRuntimeDir(imported)
	if err != nil {
		t.Fatalf("runtime dir: %v", err)
	}
	if _, err := os.Stat(runtimeDir); !os.IsNotExist(err) {
		t.Fatalf("runtime dir should not be prepared before enable, stat err = %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/table/enable?plugin_type=search_command", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable table status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var enabled PluginImportResponse
	if err := json.NewDecoder(rec.Body).Decode(&enabled); err != nil {
		t.Fatalf("decode enabled plugin: %v", err)
	}
	runtimeDir, err = executablePluginRuntimeDir(enabled)
	if err != nil {
		t.Fatalf("enabled runtime dir: %v", err)
	}
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Fatalf("runtime dir not prepared on enable: %v", err)
	}
	if _, err := os.Stat(runtimeDir + "/bin/table_command.py"); err != nil {
		t.Fatalf("entrypoint not prepared on enable: %v", err)
	}

	withoutPackage := enabled
	withoutPackage.PackageBytes = nil
	result, err := executeExecutableSearchCommand(context.Background(), plugin.SearchCommandResult{
		Rows: []map[string]any{{"src": "10.0.1.8", "action": "deny", "bytes": "2048"}},
		Fields: []string{
			"src", "action", "bytes",
		},
		OutputMode: "rows",
	}, withoutPackage, splquery.Command{Name: "table", Args: []string{"src", "action"}, Raw: "table src action"})
	if err != nil {
		t.Fatalf("execute prepared script without package bytes: %v", err)
	}
	if len(result.Rows) != 1 || len(result.Fields) != 2 || result.Fields[0] != "src" || result.Fields[1] != "action" {
		t.Fatalf("table result = %#v", result)
	}
	if _, ok := result.Rows[0]["bytes"]; ok {
		t.Fatalf("table command should project configured fields only: %#v", result.Rows[0])
	}
}

func TestExecutableSearchCommandPluginSecurityLimits(t *testing.T) {
	t.Setenv("XDP_PLUGIN_RUNTIME_DIR", t.TempDir())

	t.Run("interpreter whitelist", func(t *testing.T) {
		item := executableSearchCommandPluginForTest(t, "bad-interpreter", `import json; print(json.dumps({"rows":[],"fields":[],"output_mode":"rows"}))`)
		item.RuntimeConfig["interpreter"] = "bash"
		if err := prepareExecutableSearchCommandPlugin(item); err != nil {
			t.Fatalf("prepare plugin: %v", err)
		}
		_, err := executeExecutableSearchCommand(context.Background(), plugin.SearchCommandResult{}, item, splquery.Command{Name: "bad-interpreter"})
		if err == nil || !strings.Contains(err.Error(), "only python3 is allowed") {
			t.Fatalf("error = %v, want interpreter whitelist error", err)
		}
	})

	t.Run("input row limit", func(t *testing.T) {
		item := executableSearchCommandPluginForTest(t, "row-limit", `import json; print(json.dumps({"rows":[],"fields":[],"output_mode":"rows"}))`)
		item.RuntimeConfig["max_input_rows"] = 1
		if err := prepareExecutableSearchCommandPlugin(item); err != nil {
			t.Fatalf("prepare plugin: %v", err)
		}
		_, err := executeExecutableSearchCommand(context.Background(), plugin.SearchCommandResult{
			Rows: []map[string]any{{"a": 1}, {"a": 2}},
		}, item, splquery.Command{Name: "row-limit"})
		if err == nil || !strings.Contains(err.Error(), "input rows exceed limit") {
			t.Fatalf("error = %v, want input rows limit error", err)
		}
	})

	t.Run("output byte limit", func(t *testing.T) {
		item := executableSearchCommandPluginForTest(t, "output-limit", `print("x" * 2048)`)
		item.RuntimeConfig["max_output_bytes"] = 1024
		if err := prepareExecutableSearchCommandPlugin(item); err != nil {
			t.Fatalf("prepare plugin: %v", err)
		}
		_, err := executeExecutableSearchCommand(context.Background(), plugin.SearchCommandResult{}, item, splquery.Command{Name: "output-limit"})
		if err == nil || !strings.Contains(err.Error(), "output exceeds limit") {
			t.Fatalf("error = %v, want output limit error", err)
		}
	})

	t.Run("outside runtime file access", func(t *testing.T) {
		item := executableSearchCommandPluginForTest(t, "outside-file", `open("/etc/passwd").read()
import json
print(json.dumps({"rows":[],"fields":[],"output_mode":"rows"}))`)
		if err := prepareExecutableSearchCommandPlugin(item); err != nil {
			t.Fatalf("prepare plugin: %v", err)
		}
		_, err := executeExecutableSearchCommand(context.Background(), plugin.SearchCommandResult{}, item, splquery.Command{Name: "outside-file"})
		if err == nil || !strings.Contains(err.Error(), "outside plugin runtime dir") {
			t.Fatalf("error = %v, want outside runtime access error", err)
		}
	})
}

func importPluginForTest(t *testing.T, handler http.Handler, manifest string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/import?plugin_type=input", bytes.NewReader(pluginZipBody(t, manifest)))
	if strings.Contains(manifest, `"plugin_type": "parser"`) {
		req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/import?plugin_type=parser", bytes.NewReader(pluginZipBody(t, manifest)))
	}
	if strings.Contains(manifest, `"plugin_type": "search_command"`) {
		req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/import?plugin_type=search_command", bytes.NewReader(pluginZipBody(t, manifest)))
	}
	req.Header.Set("Content-Type", "application/zip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func pluginZipWithoutManifestForTest(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("README.md")
	if err != nil {
		t.Fatalf("create readme: %v", err)
	}
	if _, err := w.Write([]byte("demo")); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func executableSearchCommandPluginForTest(t *testing.T, code string, script string) PluginImportResponse {
	t.Helper()
	manifest := fmt.Sprintf(`{
		"plugin_code": %q,
		"plugin_type": "search_command",
		"plugin_version": "1.0.0",
		"name": %q,
		"runtime": "executable_search_command",
		"entrypoint": "bin/command.py",
		"config_schema": {"type":"object","properties":{}},
		"ui_schema": {},
		"runtime_config": {"interpreter":"python3","timeout_ms":5000}
	}`, code, code)
	body := pluginZipWithFilesForTest(t, map[string]string{
		"manifest.json":  manifest,
		"bin/command.py": script,
		"README.md":      "# " + code,
	})
	parsed, err := parsePluginManifest(body)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if err := validatePluginPackageAssets(body, parsed); err != nil {
		t.Fatalf("validate assets: %v", err)
	}
	item := parsed.toImportResponse(pluginChecksum(body))
	item.PackageBytes = body
	return item
}

func pluginZipWithFilesForTest(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func pluginZipBodyAtPath(t *testing.T, path string, manifest string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if _, err := w.Write([]byte(manifest)); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}
