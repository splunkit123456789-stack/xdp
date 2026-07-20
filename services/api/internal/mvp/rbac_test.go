package mvp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	mysqlstore "xdp/pkg/storage/mysql"
	memoryoutput "xdp/plugins/output/memory"
)

func TestGetCurrentUserReturnsEnvPlatformAdminPrincipal(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_AUTH_USERNAME", "admin")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v1/me status=%d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
		Roles []struct {
			RoleCode string `json:"role_code"`
		} `json:"roles"`
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode /api/v1/me: %v", err)
	}
	if body.User.Username != "admin" {
		t.Fatalf("username=%q", body.User.Username)
	}
	if len(body.Roles) == 0 || body.Roles[0].RoleCode != "platform_admin" {
		t.Fatalf("roles=%#v", body.Roles)
	}
	if len(body.Permissions) == 0 {
		t.Fatalf("permissions should not be empty")
	}
}

func TestProtectedAdminUserCannotBeDeleted(t *testing.T) {
	cases := []mysqlstore.AuthUserRecord{
		{ID: "u-admin", Username: "admin", RoleLabel: ""},
		{ID: "u-env-admin", Username: "platform-admin", RoleLabel: "admin"},
	}
	for _, tc := range cases {
		if !isProtectedAdminUser(tc) {
			t.Fatalf("expected protected admin user: %#v", tc)
		}
	}
	if isProtectedAdminUser(mysqlstore.AuthUserRecord{ID: "u-analyst", Username: "analyst", RoleLabel: "user"}) {
		t.Fatalf("regular user should not be protected")
	}
}

func TestGetCurrentUserReturnsIndexScopes(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req = req.WithContext(withPrincipal(req.Context(), AuthenticatedPrincipal{
		UserID:      "u-1",
		Username:    "analyst",
		Permissions: map[string]struct{}{"search:execute": {}},
		IndexScopes: map[string]mysqlstore.EffectiveIndexScope{
			"search": {Restricted: true, Patterns: []string{"audit_*"}},
		},
		PluginScopes: mysqlstore.PluginScopeMap{
			"use": {
				{PluginType: "search_command", PluginCode: "stats"},
			},
			"manage": {
				{PluginType: "parser", PluginCode: "json-parser"},
			},
		},
	}))
	rec := httptest.NewRecorder()
	handler.getCurrentUser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Scopes struct {
			Indexes map[string]struct {
				Restricted bool     `json:"restricted"`
				Patterns   []string `json:"patterns"`
			} `json:"indexes"`
			Plugins map[string][]struct {
				PluginType string `json:"plugin_type"`
				PluginCode string `json:"plugin_code"`
			} `json:"plugins"`
		} `json:"scopes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode /api/v1/me: %v", err)
	}
	scope, ok := body.Scopes.Indexes["search"]
	if !ok || !scope.Restricted || len(scope.Patterns) != 1 || scope.Patterns[0] != "audit_*" {
		t.Fatalf("index scopes=%#v", body.Scopes.Indexes)
	}
	if len(body.Scopes.Plugins["use"]) != 1 || body.Scopes.Plugins["use"][0].PluginCode != "stats" {
		t.Fatalf("plugin use scopes=%#v", body.Scopes.Plugins["use"])
	}
	if len(body.Scopes.Plugins["manage"]) != 1 || body.Scopes.Plugins["manage"][0].PluginCode != "json-parser" {
		t.Fatalf("plugin manage scopes=%#v", body.Scopes.Plugins["manage"])
	}
}

func TestPermissionWrapperReturnsForbiddenWithRequiredPermission(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	wrapped := handler.withPermission("datasource:create", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req = req.WithContext(withPrincipal(req.Context(), AuthenticatedPrincipal{
		UserID:          "u-1",
		Username:        "readonly",
		RoleCodes:       []string{"readonly"},
		PermissionCodes: []string{"search:execute"},
		Permissions:     map[string]struct{}{"search:execute": {}},
	}))
	rec := httptest.NewRecorder()
	wrapped(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code               string `json:"code"`
			RequiredPermission string `json:"required_permission"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode forbidden response: %v", err)
	}
	if body.Error.Code != "FORBIDDEN" || body.Error.RequiredPermission != "datasource:create" {
		t.Fatalf("forbidden body=%#v", body)
	}
	if body.RequestID == "" {
		t.Fatalf("request_id should not be empty")
	}
}

func TestBusinessAPIsRequireRBACPermissions(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	principal := AuthenticatedPrincipal{
		UserID:          "u-1",
		Username:        "readonly",
		RoleCodes:       []string{"readonly"},
		PermissionCodes: []string{"search:execute"},
		Permissions:     map[string]struct{}{"search:execute": {}},
	}

	cases := []struct {
		name     string
		method   string
		path     string
		required string
	}{
		{name: "datasources list", method: http.MethodGet, path: "/api/v1/datasources", required: "datasource:read"},
		{name: "parse rules list", method: http.MethodGet, path: "/api/v1/parse-rules", required: "parse_rule:read"},
		{name: "indexes list", method: http.MethodGet, path: "/api/v1/indexes", required: "index:read"},
		{name: "runtime pipelines list", method: http.MethodGet, path: "/api/v1/runtime/pipelines", required: "parse_rule:read"},
		{name: "writer runtime", method: http.MethodGet, path: "/api/v1/writer/runtime", required: "index:read"},
		{name: "deadletters list", method: http.MethodGet, path: "/api/v1/deadletters", required: "parse_rule:read"},
		{name: "users list", method: http.MethodGet, path: "/api/v1/users", required: "rbac:manage"},
		{name: "users create", method: http.MethodPost, path: "/api/v1/users", required: "rbac:manage"},
		{name: "users update", method: http.MethodPut, path: "/api/v1/users/u-1", required: "rbac:manage"},
		{name: "users roles bind", method: http.MethodPut, path: "/api/v1/users/u-1/roles", required: "rbac:manage"},
		{name: "roles list", method: http.MethodGet, path: "/api/v1/roles", required: "rbac:manage"},
		{name: "roles create", method: http.MethodPost, path: "/api/v1/roles", required: "rbac:manage"},
		{name: "roles update", method: http.MethodPut, path: "/api/v1/roles/role-1", required: "rbac:manage"},
		{name: "permissions list", method: http.MethodGet, path: "/api/v1/permissions", required: "rbac:manage"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req = req.WithContext(withPrincipal(req.Context(), principal))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var body struct {
				Error struct {
					Code               string `json:"code"`
					RequiredPermission string `json:"required_permission"`
				} `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode forbidden response: %v", err)
			}
			if body.Error.Code != "FORBIDDEN" || body.Error.RequiredPermission != tc.required {
				t.Fatalf("body=%#v", body)
			}
		})
	}
}

func TestSearchSupportRoutesRequireSearchExecute(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	principal := AuthenticatedPrincipal{
		UserID:          "u-1",
		Username:        "limited",
		PermissionCodes: []string{"index:read"},
		Permissions:     map[string]struct{}{"index:read": {}},
	}

	for _, path := range []string{
		"/api/v1/search/fields?q=" + url.QueryEscape("index=audit_p2_rbac"),
		"/api/v1/search/timeline?q=" + url.QueryEscape("index=audit_p2_rbac"),
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(withPrincipal(req.Context(), principal))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var body struct {
			Error struct {
				RequiredPermission string `json:"required_permission"`
			} `json:"error"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode forbidden response: %v", err)
		}
		if body.Error.RequiredPermission != "search:execute" {
			t.Fatalf("%s required permission = %q", path, body.Error.RequiredPermission)
		}
	}
}

func TestPluginManagementRouteRejectsMissingManageScope(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins?plugin_type=input", nil)
	req = req.WithContext(withPrincipal(req.Context(), AuthenticatedPrincipal{
		UserID:       "u-1",
		Username:     "analyst",
		Permissions:  map[string]struct{}{"search:execute": {}},
		PluginScopes: mysqlstore.PluginScopeMap{},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code          string `json:"code"`
			RequiredScope string `json:"required_scope"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode forbidden response: %v", err)
	}
	if body.Error.Code != "FORBIDDEN" || body.Error.RequiredScope != "plugin:manage" {
		t.Fatalf("body=%#v", body)
	}
}

func TestPluginCatalogFiltersByUseScope(t *testing.T) {
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
		"config_schema": {"type":"object","properties":{}},
		"ui_schema": {}
	}`)
	enableReq := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/json-parser/enable?plugin_type=parser", nil)
	enableReq = enableReq.WithContext(withPrincipal(enableReq.Context(), AuthenticatedPrincipal{
		UserID:       "u-admin",
		Username:     "admin",
		Permissions:  map[string]struct{}{},
		PluginScopes: mysqlstore.PluginScopeMap{"manage": {{PluginType: "parser", PluginCode: "*"}}},
	}))
	enableRec := httptest.NewRecorder()
	handler.ServeHTTP(enableRec, enableReq)
	if enableRec.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", enableRec.Code, enableRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/catalog?plugin_type=parser&status=enabled", nil)
	req = req.WithContext(withPrincipal(req.Context(), AuthenticatedPrincipal{
		UserID:       "u-1",
		Username:     "analyst",
		Permissions:  map[string]struct{}{},
		PluginScopes: mysqlstore.PluginScopeMap{"use": {{PluginType: "parser", PluginCode: "regex"}}},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Plugins []PluginImportResponse `json:"plugins"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(body.Plugins) != 1 || body.Plugins[0].PluginCode != "regex" {
		t.Fatalf("catalog plugins=%#v", body.Plugins)
	}
}

func TestSearchRouteRequiresSearchExecutePermission(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?index=app", nil)
	req = req.WithContext(withPrincipal(req.Context(), AuthenticatedPrincipal{
		UserID:      "u-1",
		Username:    "readonly",
		Permissions: map[string]struct{}{},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			RequiredPermission string `json:"required_permission"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode forbidden response: %v", err)
	}
	if body.Error.RequiredPermission != "search:execute" {
		t.Fatalf("body=%#v", body)
	}
}

func TestIndexRouteFiltersByScope(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	memoryoutput.DefaultStore().Clear()

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	handler.indexConfigs = map[string]IndexSummary{
		indexKey("audit_p1_manual"): {IndexName: "audit_p1_manual", Name: "audit_p1_manual", Status: "active", Configured: true},
		indexKey("json_p1_manual"):  {IndexName: "json_p1_manual", Name: "json_p1_manual", Status: "active", Configured: true},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexes", nil)
	req = req.WithContext(withPrincipal(req.Context(), AuthenticatedPrincipal{
		UserID:          "u-1",
		Username:        "analyst",
		PermissionCodes: []string{"index:read"},
		Permissions:     map[string]struct{}{"index:read": {}},
		IndexScopes: map[string]mysqlstore.EffectiveIndexScope{
			"read": {Restricted: true, Patterns: []string{"audit_*"}},
		},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Indexes []struct {
			IndexName string `json:"index_name"`
		} `json:"indexes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode indexes: %v", err)
	}
	if len(body.Indexes) != 1 || body.Indexes[0].IndexName != "audit_p1_manual" {
		t.Fatalf("indexes=%#v", body.Indexes)
	}
}

func TestSearchRouteRejectsUnauthorizedIndexScope(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	principal := AuthenticatedPrincipal{
		UserID:          "u-1",
		Username:        "analyst",
		PermissionCodes: []string{"search:execute"},
		Permissions:     map[string]struct{}{"search:execute": {}},
		IndexScopes: map[string]mysqlstore.EffectiveIndexScope{
			"search": {Restricted: true, Patterns: []string{"audit_*"}},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape("index=json_p1_manual action=deny"), nil)
	req = req.WithContext(withPrincipal(req.Context(), principal))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code          string `json:"code"`
			RequiredScope string `json:"required_scope"`
			ResourceName  string `json:"resource_name"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode forbidden response: %v", err)
	}
	if body.Error.Code != "FORBIDDEN" || body.Error.RequiredScope != "index:search" || body.Error.ResourceName != "json_p1_manual" {
		t.Fatalf("body=%#v", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape("action=deny"), nil)
	req = req.WithContext(withPrincipal(req.Context(), principal))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode forbidden response: %v", err)
	}
	if body.Error.RequiredScope != "index:search" {
		t.Fatalf("body=%#v", body)
	}
}

func TestSearchRouteAllowsBuiltinStatsWithoutPluginScope(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	memoryoutput.DefaultStore().Clear()

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape("index=app | stats count"), nil)
	req = req.WithContext(withPrincipal(req.Context(), AuthenticatedPrincipal{
		UserID:          "u-1",
		Username:        "analyst",
		PermissionCodes: []string{"search:execute"},
		Permissions:     map[string]struct{}{"search:execute": {}},
		PluginScopes:    mysqlstore.PluginScopeMap{},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSearchRouteRejectsUnauthorizedExternalSearchCommandPluginScope(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	memoryoutput.DefaultStore().Clear()

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape("index=app | table raw"), nil)
	req = req.WithContext(withPrincipal(req.Context(), AuthenticatedPrincipal{
		UserID:          "u-1",
		Username:        "analyst",
		PermissionCodes: []string{"search:execute"},
		Permissions:     map[string]struct{}{"search:execute": {}},
		PluginScopes:    mysqlstore.PluginScopeMap{},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code          string `json:"code"`
			RequiredScope string `json:"required_scope"`
			PluginType    string `json:"plugin_type"`
			PluginCode    string `json:"plugin_code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode forbidden response: %v", err)
	}
	if body.Error.Code != "FORBIDDEN" || body.Error.RequiredScope != "plugin:use" || body.Error.PluginType != "search_command" || body.Error.PluginCode != "table" {
		t.Fatalf("body=%#v", body)
	}
}

func TestCreateDataSourceRejectsUnauthorizedExternalInputPluginScope(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	importPluginForTest(t, handler, `{
		"plugin_code": "kafka",
		"plugin_type": "input",
		"plugin_version": "1.0.0",
		"name": "Kafka Input",
		"runtime": "go_builtin",
		"entrypoint": "builtin://plugins/input/kafka",
		"config_schema": {"type":"object","required":["brokers","topic","consumer_group","start_offset","security_protocol","encoding","log_filter_enabled"],"properties":{"brokers":{"type":"array","items":{"type":"string"}},"topic":{"type":"string"},"consumer_group":{"type":"string"},"start_offset":{"type":"string"},"security_protocol":{"type":"string"},"encoding":{"type":"string"},"log_filter_enabled":{"type":"boolean"}}},
		"ui_schema": {}
	}`)
	enableReq := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/kafka/enable?plugin_type=input", nil)
	enableRec := httptest.NewRecorder()
	handler.ServeHTTP(enableRec, enableReq)
	if enableRec.Code != http.StatusOK {
		t.Fatalf("enable kafka status=%d body=%s", enableRec.Code, enableRec.Body.String())
	}
	body := bytes.NewBufferString(`{
		"name":"Kafka Scope Test",
		"status":"active",
		"plugin_code":"kafka",
		"plugin_config":{
			"brokers":["127.0.0.1:9092"],
			"topic":"audit-events",
			"consumer_group":"xdp-agent",
			"start_offset":"earliest",
			"security_protocol":"PLAINTEXT",
			"encoding":"UTF-8",
			"log_filter_enabled":false
		}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources", body)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withPrincipal(req.Context(), AuthenticatedPrincipal{
		UserID:          "u-1",
		Username:        "config",
		PermissionCodes: []string{"datasource:create"},
		Permissions:     map[string]struct{}{"datasource:create": {}},
		PluginScopes:    mysqlstore.PluginScopeMap{},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Error struct {
			RequiredScope string `json:"required_scope"`
			PluginType    string `json:"plugin_type"`
			PluginCode    string `json:"plugin_code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.RequiredScope != "plugin:use" || response.Error.PluginType != "input" || response.Error.PluginCode != "kafka" {
		t.Fatalf("response=%#v", response)
	}
}

func TestCreateParseRuleRejectsUnauthorizedExternalParserPluginScope(t *testing.T) {
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
		"config_schema": {"type":"object","required":["source_field","target","flatten_nested","flatten_separator","array_mode","on_invalid_json"],"properties":{"source_field":{"type":"string"},"target":{"type":"string"},"flatten_nested":{"type":"boolean"},"flatten_separator":{"type":"string"},"array_mode":{"type":"string"},"on_invalid_json":{"type":"string"}}},
		"ui_schema": {}
	}`)
	enableReq := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/json-parser/enable?plugin_type=parser", nil)
	enableRec := httptest.NewRecorder()
	handler.ServeHTTP(enableRec, enableReq)
	if enableRec.Code != http.StatusOK {
		t.Fatalf("enable json parser status=%d body=%s", enableRec.Code, enableRec.Body.String())
	}
	body := bytes.NewBufferString(`{
		"name":"JSON Scope Test",
		"status":"active",
		"parser_plugin":"json-parser",
		"data_source_name":"Firewall Syslog",
		"output_index":"audit_p1_manual",
		"sample_event":"{\"service\":\"checkout\"}",
		"plugin_config":{"source_field":"raw","target":"fields","flatten_nested":true,"flatten_separator":".","array_mode":"json_string","on_invalid_json":"continue"},
		"props_conf":"[source::Firewall Syslog]\nINDEXED_EXTRACTIONS = json"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules", body)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withPrincipal(req.Context(), AuthenticatedPrincipal{
		UserID:          "u-1",
		Username:        "config",
		PermissionCodes: []string{"parse_rule:create"},
		Permissions:     map[string]struct{}{"parse_rule:create": {}},
		PluginScopes:    mysqlstore.PluginScopeMap{},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Error struct {
			RequiredScope string `json:"required_scope"`
			PluginType    string `json:"plugin_type"`
			PluginCode    string `json:"plugin_code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.RequiredScope != "plugin:use" || response.Error.PluginType != "parser" || response.Error.PluginCode != "json-parser" {
		t.Fatalf("response=%#v", response)
	}
}

func TestDataSourceStatusRouteRequiresStopPermission(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	handler.mu.Lock()
	handler.dataSources["ds-stop"] = DataSource{
		ID:         "ds-stop",
		Type:       "syslog",
		Name:       "DS Stop",
		Status:     "active",
		PluginCode: "syslog",
		UpdatedAt:  time.Now(),
	}
	handler.mu.Unlock()

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/datasources/ds-stop/status", bytes.NewReader([]byte(`{"status":"disabled"}`)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withPrincipal(req.Context(), AuthenticatedPrincipal{
		UserID:          "u-1",
		Username:        "config-viewer",
		PermissionCodes: []string{"datasource:read"},
		Permissions:     map[string]struct{}{"datasource:read": {}},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			RequiredPermission string `json:"required_permission"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode forbidden response: %v", err)
	}
	if body.Error.RequiredPermission != "datasource:stop" {
		t.Fatalf("body=%#v", body)
	}
}

func TestDataSourceStatusRouteRequiresStartPermission(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	handler.mu.Lock()
	handler.dataSources["ds-start"] = DataSource{
		ID:         "ds-start",
		Type:       "syslog",
		Name:       "DS Start",
		Status:     "disabled",
		PluginCode: "syslog",
		UpdatedAt:  time.Now(),
	}
	handler.mu.Unlock()

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/datasources/ds-start/status", bytes.NewReader([]byte(`{"status":"active"}`)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withPrincipal(req.Context(), AuthenticatedPrincipal{
		UserID:          "u-1",
		Username:        "config-viewer",
		PermissionCodes: []string{"datasource:read"},
		Permissions:     map[string]struct{}{"datasource:read": {}},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			RequiredPermission string `json:"required_permission"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode forbidden response: %v", err)
	}
	if body.Error.RequiredPermission != "datasource:start" {
		t.Fatalf("body=%#v", body)
	}
}

func TestUsersRouteRequiresAuthenticationWhenEnabled(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "true")
	t.Setenv("XDP_AUTH_USERNAME", "admin")
	t.Setenv("XDP_AUTH_PASSWORD", "xdp")
	t.Setenv("XDP_API_TOKEN", "test-token")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
