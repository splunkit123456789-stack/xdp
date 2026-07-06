package mvp

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/pipeline"
	"xdp/pkg/plugin"
	"xdp/pkg/search/splstats"
	ch "xdp/pkg/storage/clickhouse"
	mysqlstore "xdp/pkg/storage/mysql"
	memoryoutput "xdp/plugins/output/memory"
)

func TestAuthStatusReportsLoginRequirements(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "true")
	t.Setenv("XDP_AUTH_USERNAME", "admin")
	t.Setenv("XDP_AUTH_PASSWORD", "xdp")
	t.Setenv("XDP_API_TOKEN", "test-token")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Enabled       bool     `json:"enabled"`
		LoginRequired bool     `json:"login_required"`
		Authenticated bool     `json:"authenticated"`
		AuthType      string   `json:"auth_type"`
		TokenType     string   `json:"token_type"`
		TokenHeader   string   `json:"token_header"`
		PublicPaths   []string `json:"public_paths"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode auth status: %v", err)
	}
	if !response.Enabled || !response.LoginRequired || response.Authenticated {
		t.Fatalf("auth status flags = %#v", response)
	}
	if response.AuthType != "password_token" || response.TokenType != "Bearer" || response.TokenHeader != "Authorization" {
		t.Fatalf("auth status contract = %#v", response)
	}
	for _, want := range []string{"/", "/healthz", "/readyz", "/api/v1/auth", "/api/v1/login"} {
		if !containsString(response.PublicPaths, want) {
			t.Fatalf("public_paths = %#v, missing %s", response.PublicPaths, want)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated auth status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode authenticated auth status: %v", err)
	}
	if !response.Authenticated {
		t.Fatalf("authenticated = false, response = %#v", response)
	}
}

func TestPluginImportRegistersManifestAndListsPlugin(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := pluginZipBody(t, `{
		"plugin_code": "demo-kafka",
		"plugin_type": "input",
		"version": "1.2.3",
		"name": "Demo Kafka Input",
		"description": "Kafka input plugin imported from Web",
		"runtime": "go_builtin",
		"entrypoint": "runtime/plugin",
		"min_platform_version": "0.3.0",
		"config_schema": {"required": ["brokers", "topic"]},
		"ui_schema": {"groups": [{"title": "Kafka", "fields": ["brokers", "topic"]}]},
		"input_schema": {"mode": "stream"},
		"output_schema": {"fields": ["raw"]},
		"capabilities": {"runtime_ingest": true}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/import?plugin_type=input", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/zip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var imported struct {
		PluginCode    string         `json:"plugin_code"`
		PluginType    string         `json:"plugin_type"`
		PluginVersion string         `json:"plugin_version"`
		Runtime       string         `json:"runtime"`
		Status        string         `json:"status"`
		Checksum      string         `json:"checksum"`
		ConfigSchema  map[string]any `json:"config_schema"`
		UISchema      map[string]any `json:"ui_schema"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&imported); err != nil {
		t.Fatalf("decode imported plugin: %v", err)
	}
	if imported.PluginCode != "demo-kafka" || imported.PluginType != "input" || imported.PluginVersion != "1.2.3" || imported.Runtime != "go_builtin" {
		t.Fatalf("imported plugin identity = %#v", imported)
	}
	if imported.Status != "disabled" || imported.Checksum == "" {
		t.Fatalf("imported plugin status/checksum = %#v", imported)
	}
	if _, ok := imported.ConfigSchema["required"]; !ok {
		t.Fatalf("config_schema missing required: %#v", imported.ConfigSchema)
	}
	if _, ok := imported.UISchema["groups"]; !ok {
		t.Fatalf("ui_schema missing groups: %#v", imported.UISchema)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins?type=input", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Plugins []struct {
			PluginCode    string `json:"plugin_code"`
			PluginType    string `json:"plugin_type"`
			PluginVersion string `json:"plugin_version"`
			Status        string `json:"status"`
			Checksum      string `json:"checksum"`
		} `json:"plugins"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list plugins: %v", err)
	}
	found := false
	for _, item := range listed.Plugins {
		if item.PluginCode == "demo-kafka" && item.PluginType == "input" && item.PluginVersion == "1.2.3" && item.Status == "disabled" && item.Checksum != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("imported plugin not found in list: %#v", listed.Plugins)
	}
}

func TestPluginImportRejectsMismatchedType(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := pluginZipBody(t, `{
		"plugin_code": "demo-parser",
		"plugin_type": "parser",
		"version": "1.0.0",
		"name": "Demo Parser",
		"runtime": "go_builtin",
		"config_schema": {},
		"ui_schema": {}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/import?plugin_type=search_command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/zip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusBadRequest, "PLUGIN_TYPE_MISMATCH")
}

func TestListPluginsOnlyExposesProductVisiblePluginTypes(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Plugins []PluginImportResponse `json:"plugins"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, item := range body.Plugins {
		seen[item.PluginType+"/"+item.PluginCode] = true
		if item.PluginType == "spl_function" {
			t.Fatalf("spl_function should not be exposed in plugin management: %+v", item)
		}
	}
	for _, key := range []string{"input/syslog", "parser/regex", "search_command/stats"} {
		if !seen[key] {
			t.Fatalf("expected visible plugin %s, got %#v", key, seen)
		}
	}
	for _, key := range []string{"input/http-input", "parser/json-parser", "parser/props-conf-parser"} {
		if seen[key] {
			t.Fatalf("internal or not-yet-productized plugin %s should be hidden, got %#v", key, seen)
		}
	}
}

func TestPluginResponsesFromRecordsHidesHistoricalInternalPlugins(t *testing.T) {
	items := []mysqlstore.PluginRecord{
		{PluginType: "input", PluginCode: "syslog", PluginVersion: "1.0.0", Name: "Syslog Input", Runtime: "go_builtin", Status: "active"},
		{PluginType: "input", PluginCode: "http-input", PluginVersion: "1.0.0", Name: "HTTP Input", Runtime: "go", Status: "active"},
		{PluginType: "parser", PluginCode: "regex", PluginVersion: "1.0.0", Name: "Regex Parser", Runtime: "go_builtin", Status: "active"},
		{PluginType: "parser", PluginCode: "json-parser", PluginVersion: "1.0.0", Name: "JSON Parser", Runtime: "go", Status: "active"},
		{PluginType: "parser", PluginCode: "props-conf-parser", PluginVersion: "1.0.0", Name: "Props.conf Parser", Runtime: "go", Status: "active"},
		{PluginType: "search_command", PluginCode: "stats", PluginVersion: "1.0.0", Name: "stats", Runtime: "go_builtin", Status: "active"},
		{PluginType: "input", PluginCode: "demo-kafka", PluginVersion: "1.0.0", Name: "Demo Kafka", Runtime: "go_builtin", Status: "disabled", Checksum: "sha256:demo"},
		{PluginType: "spl_function", PluginCode: "lower", PluginVersion: "1.0.0", Name: "lower", Runtime: "go_builtin", Status: "active", Checksum: "sha256:lower"},
	}

	plugins := pluginResponsesFromRecords(items, "")
	seen := map[string]bool{}
	for _, item := range plugins {
		seen[item.PluginType+"/"+item.PluginCode] = true
	}
	for _, key := range []string{"input/syslog", "parser/regex", "search_command/stats", "input/demo-kafka"} {
		if !seen[key] {
			t.Fatalf("expected visible plugin %s, got %#v", key, seen)
		}
	}
	for _, key := range []string{"input/http-input", "parser/json-parser", "parser/props-conf-parser", "spl_function/lower"} {
		if seen[key] {
			t.Fatalf("historical internal or unsupported plugin %s should be hidden, got %#v", key, seen)
		}
	}
}

func pluginZipBody(t *testing.T, manifest string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatalf("create manifest: %v", err)
	}
	if _, err := w.Write([]byte(manifest)); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestLoginReturnsBearerTokenAndUserInfo(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "true")
	t.Setenv("XDP_AUTH_USERNAME", "admin")
	t.Setenv("XDP_AUTH_PASSWORD", "xdp")
	t.Setenv("XDP_API_TOKEN", "test-token")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(`{"username":"admin","password":"xdp"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Token     string `json:"token"`
		TokenType string `json:"token_type"`
		ExpiresIn int    `json:"expires_in"`
		User      struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if response.Token != "test-token" || response.TokenType != "Bearer" || response.ExpiresIn != 0 {
		t.Fatalf("login token response = %#v", response)
	}
	if response.User.Username != "admin" || response.User.Role != "admin" {
		t.Fatalf("login user response = %#v", response.User)
	}
}

func TestLoginAndAuthFailuresReturnStructuredErrors(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "true")
	t.Setenv("XDP_AUTH_USERNAME", "admin")
	t.Setenv("XDP_AUTH_PASSWORD", "xdp")
	t.Setenv("XDP_API_TOKEN", "test-token")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "INVALID_CREDENTIALS")

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestListIndexesPaginatesResults(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")
	memoryoutput.DefaultStore().Clear()
	t.Cleanup(func() { memoryoutput.DefaultStore().Clear() })

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	handler.mu.Lock()
	handler.indexConfigs = map[string]IndexSummary{}
	for i := 1; i <= 25; i++ {
		name := fmt.Sprintf("idx_%02d", i)
		handler.indexConfigs[indexKey(name)] = IndexSummary{
			IndexName:  name,
			Name:       name,
			TTLDays:    30,
			Storage:    "configured",
			Status:     "active",
			Configured: true,
		}
	}
	handler.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexes?page=2", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list indexes status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Indexes    []IndexSummary `json:"indexes"`
		Pagination struct {
			Page       int `json:"page"`
			PageSize   int `json:"page_size"`
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode index pagination: %v", err)
	}
	if len(response.Indexes) != 10 {
		t.Fatalf("indexes len = %d, want 10", len(response.Indexes))
	}
	if response.Pagination.Page != 2 || response.Pagination.PageSize != 10 || response.Pagination.Total != 25 || response.Pagination.TotalPages != 3 {
		t.Fatalf("pagination = %#v, want page=2 page_size=10 total=25 total_pages=3", response.Pagination)
	}
}

func TestLoginInvalidRequestsReturnInvalidRequest(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "true")
	t.Setenv("XDP_AUTH_USERNAME", "admin")
	t.Setenv("XDP_AUTH_PASSWORD", "xdp")
	t.Setenv("XDP_API_TOKEN", "test-token")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(`{"username":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusBadRequest, "INVALID_REQUEST")

	req = httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(`not-json`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusBadRequest, "INVALID_REQUEST")
}

func TestPublicPathsDoNotRequireToken(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "true")
	t.Setenv("XDP_API_TOKEN", "test-token")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, path := range []string{"/", "/healthz", "/readyz", "/api/v1/auth"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestInvalidBearerTokensAreRejected(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "true")
	t.Setenv("XDP_API_TOKEN", "test-token")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	cases := []struct {
		name   string
		header string
		value  string
	}{
		{name: "empty bearer", header: "Authorization", value: "Bearer "},
		{name: "wrong bearer", header: "Authorization", value: "Bearer wrong-token"},
		{name: "malformed authorization", header: "Authorization", value: "Token test-token"},
		{name: "wrong x api token", header: "X-API-Token", value: "wrong-token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
			req.Header.Set(tc.header, tc.value)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertErrorResponse(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")
		})
	}
}

func TestAuthDisabledAllowsDeveloperModeAccess(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var auth struct {
		Enabled       bool `json:"enabled"`
		LoginRequired bool `json:"login_required"`
		Authenticated bool `json:"authenticated"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&auth); err != nil {
		t.Fatalf("decode auth status: %v", err)
	}
	if auth.Enabled || auth.LoginRequired || !auth.Authenticated {
		t.Fatalf("auth disabled response = %#v", auth)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("plugins status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestSavedSearchFavoritesLifecycle(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "true")
	t.Setenv("XDP_API_TOKEN", "test-token")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := `{"name":"Audit deny by source","spl":"index=audit action=deny | stats count by src","time_range_type":"relative","earliest":"-7d@d","latest":"now"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/search/favorites", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create saved search status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		SPL           string `json:"spl"`
		TimeRangeType string `json:"time_range_type"`
		Earliest      string `json:"earliest"`
		Latest        string `json:"latest"`
		Status        string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created saved search: %v", err)
	}
	if created.ID == "" || created.SPL != "index=audit action=deny | stats count by src" || created.Status != "active" {
		t.Fatalf("created saved search = %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/search/favorites", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list saved searches status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		SavedSearches []struct {
			ID  string `json:"id"`
			SPL string `json:"spl"`
		} `json:"saved_searches"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode saved search list: %v", err)
	}
	if !savedSearchListContains(list.SavedSearches, created.ID) {
		t.Fatalf("saved search list missing %s: %#v", created.ID, list.SavedSearches)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/search/favorites/"+url.PathEscape(created.ID), nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete saved search status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var deleted struct {
		Deleted bool   `json:"deleted"`
		ID      string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&deleted); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if !deleted.Deleted || deleted.ID != created.ID {
		t.Fatalf("delete response = %#v", deleted)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/search/favorites", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list after delete status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode saved search list after delete: %v", err)
	}
	if savedSearchListContains(list.SavedSearches, created.ID) {
		t.Fatalf("deleted saved search still listed: %#v", list.SavedSearches)
	}
}

func TestDefaultSavedSearchesAreListed(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "true")
	t.Setenv("XDP_API_TOKEN", "test-token")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/favorites", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list default saved searches status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		SavedSearches []struct {
			ID  string `json:"id"`
			SPL string `json:"spl"`
		} `json:"saved_searches"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode default saved search list: %v", err)
	}
	if len(list.SavedSearches) != 2 {
		t.Fatalf("default saved searches count = %d, items = %#v", len(list.SavedSearches), list.SavedSearches)
	}
	if !savedSearchListContains(list.SavedSearches, "s-1") || !savedSearchListContains(list.SavedSearches, "s-2") {
		t.Fatalf("default saved searches missing s-1/s-2: %#v", list.SavedSearches)
	}
}

func savedSearchListContains(items []struct {
	ID  string `json:"id"`
	SPL string `json:"spl"`
}, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func TestProtectedAPIAllowsBearerAndXAPIToken(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "true")
	t.Setenv("XDP_AUTH_USERNAME", "admin")
	t.Setenv("XDP_AUTH_PASSWORD", "xdp")
	t.Setenv("XDP_API_TOKEN", "test-token")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	req.Header.Set("X-API-Token", "test-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("x api token status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, wantStatus, rec.Body.String())
	}
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Error.Code != wantCode {
		t.Fatalf("error code = %q, want %q, body = %s", response.Error.Code, wantCode, rec.Body.String())
	}
	if response.Error.Message == "" || response.RequestID == "" {
		t.Fatalf("incomplete error response: %#v", response)
	}
}

func TestSaveDataSourceUpdatesRuntimePipelines(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := `{"id":"syslog-hot-reload","type":"syslog","name":"Syslog Hot Reload","status":"active","addr":":5515","protocol":"udp","default_index":"hotreload","pipeline_id":"syslog-hot-reload-pipeline"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save datasource status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/runtime/pipelines", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime pipelines status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Pipelines []pipeline.Pipeline `json:"pipelines"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode runtime pipelines: %v", err)
	}
	for _, pipe := range response.Pipelines {
		if pipe.Metadata.ID != "syslog-hot-reload-pipeline" {
			continue
		}
		if got := pipe.Spec.Outputs[0].Config["index"]; got != "hotreload" {
			t.Fatalf("runtime pipeline output index = %#v, want hotreload", got)
		}
		if got := pipe.Spec.Source.Config["internal_raw_topic"]; got != "xdp.raw.syslog" {
			t.Fatalf("runtime pipeline internal_raw_topic = %#v, want xdp.raw.syslog", got)
		}
		if _, ok := pipe.Spec.Source.Config["raw_topic"]; ok {
			t.Fatalf("runtime pipeline still exposes raw_topic: %#v", pipe.Spec.Source.Config)
		}
		return
	}
	t.Fatalf("runtime pipelines missing syslog-hot-reload-pipeline: %#v", response.Pipelines)
}

func TestSaveDataSourceRejectsRawTopicPayload(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := `{"id":"syslog-raw-topic","type":"syslog","name":"Syslog Raw Topic","status":"active","addr":":5516","protocol":"udp","default_index":"app","raw_topic":"xdp.raw.syslog","pipeline_id":"syslog-raw-topic-pipeline"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("save datasource with raw_topic status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestSaveDataSourceBuildsParsingStages(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := `{"id":"syslog-plugin-source","type":"syslog","name":"Syslog Plugin Source","status":"active","addr":":5514","protocol":"udp","default_index":"audit","pipeline_id":"pipe_syslog_plugin_source"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save datasource status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/runtime/pipelines", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime pipelines status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Pipelines []pipeline.Pipeline `json:"pipelines"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode runtime pipelines: %v", err)
	}
	for _, pipe := range response.Pipelines {
		if pipe.Metadata.ID != "pipe_syslog_plugin_source" {
			continue
		}
		if pipe.Spec.Source.Plugin != "syslog" {
			t.Fatalf("syslog collection source plugin = %q, want syslog", pipe.Spec.Source.Plugin)
		}
		if len(pipe.Spec.Stages) != 0 {
			t.Fatalf("syslog collection pipeline must not include parse/transform/router stages: %#v", pipe.Spec.Stages)
		}
		return
	}
	t.Fatalf("runtime pipelines missing plugin syslog pipeline: %#v", response.Pipelines)
}

func TestSyslogRuntimePipelineWithoutParseRuleUsesStandardPlugins(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"name":"No Rule Syslog","status":"active","plugin_code":"syslog","plugin_config":{"collector_port":55555,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save datasource status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/runtime/pipelines", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime pipelines status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Pipelines []pipeline.Pipeline `json:"pipelines"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode runtime pipelines: %v", err)
	}
	for _, pipe := range response.Pipelines {
		if pipe.Metadata.ID != "pipe_no_rule_syslog" {
			continue
		}
		if pipe.Spec.Source.Plugin != "syslog" {
			t.Fatalf("syslog runtime source plugin = %q, want syslog", pipe.Spec.Source.Plugin)
		}
		if len(pipe.Spec.Stages) != 0 {
			t.Fatalf("syslog runtime pipeline without parse rules must not include stages: %#v", pipe.Spec.Stages)
		}
		if len(pipe.Spec.Outputs) == 0 || pipe.Spec.Outputs[0].Plugin != "memory-output" {
			t.Fatalf("syslog runtime pipeline output = %#v", pipe.Spec.Outputs)
		}
		return
	}
	t.Fatalf("runtime pipelines missing no-rule syslog pipeline: %#v", response.Pipelines)
}

func TestDefaultDataSourcesDoNotSeedLegacyFirewallSyslog(t *testing.T) {
	sources := defaultDataSources()
	for _, source := range sources {
		if source.Type == "syslog" && (source.Parser != "" || source.RegexPattern != "" || source.DefaultIndex == "firewall") {
			t.Fatalf("default datasource contains legacy syslog parsing config: %#v", source)
		}
	}
}

func TestDefaultIndexConfigsOnlySeedSystemIndexes(t *testing.T) {
	indexes := defaultIndexConfigs()
	if _, ok := indexes["app"]; ok {
		t.Fatalf("default indexes must not seed demo business index app: %#v", indexes)
	}
	if _, ok := indexes["firewall"]; ok {
		t.Fatalf("default indexes must not seed demo business index firewall: %#v", indexes)
	}
	item, ok := indexes[ch.SystemUnparsedIndexName]
	if !ok {
		t.Fatalf("default indexes missing system unparsed index: %#v", indexes)
	}
	if !item.System || item.IndexType != "system" {
		t.Fatalf("system unparsed metadata = %#v", item)
	}
}

func TestSaveAndDeleteIndexConfig(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	saveBody := `{"index_name":"audit","ttl_days":7,"status":"active"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/indexes", strings.NewReader(saveBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save index status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/indexes", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list indexes status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Indexes []IndexSummary `json:"indexes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode indexes: %v", err)
	}
	found := false
	for _, item := range list.Indexes {
		if item.IndexName == "audit" && item.TTLDays == 7 {
			found = true
		}
	}
	if !found {
		t.Fatalf("indexes = %#v, want audit", list.Indexes)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/indexes?index=audit", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete index status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestIndexAPIProtectsSystemUnparsedIndex(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexes", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list indexes status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Indexes []struct {
			IndexName string `json:"index_name"`
			TableName string `json:"table_name"`
			System    bool   `json:"system"`
			IndexType string `json:"index_type"`
		} `json:"indexes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode indexes: %v", err)
	}
	found := false
	for _, item := range list.Indexes {
		if item.IndexName == "_unparsed" {
			found = true
			if !item.System || item.IndexType != "system" {
				t.Fatalf("_unparsed metadata = %#v, want system index", item)
			}
			if item.TableName != "events__unparsed" {
				t.Fatalf("_unparsed table = %q, want events__unparsed", item.TableName)
			}
		}
	}
	if !found {
		t.Fatalf("indexes = %#v, want _unparsed", list.Indexes)
	}

	saveBody := `{"index_name":"_unparsed","ttl_days":7,"status":"active"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/indexes", strings.NewReader(saveBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("save system index status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/indexes?index=_unparsed", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("delete system index status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestSaveIndexConfigValidatesRequiredFields(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	cases := []struct {
		name string
		body string
	}{
		{name: "missing index name", body: `{"ttl_days":7,"status":"active"}`},
		{name: "missing ttl days", body: `{"index_name":"audit_required","status":"active"}`},
		{name: "missing status", body: `{"index_name":"audit_required","ttl_days":7}`},
		{name: "empty status", body: `{"index_name":"audit_required","ttl_days":7,"status":""}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/indexes", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("save index status = %d, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSearchAPIStatsP0ResponseContract(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "searchp0"
	appendSearchEvent(index, "api", "allow", 100)
	appendSearchEvent(index, "api", "allow", 200)
	appendSearchEvent(index, "worker", "deny", 50)

	spl := `index=searchp0 | stats count as total sum(bytes) as total_bytes avg(bytes) as avg_bytes min(bytes) as min_bytes max(bytes) as max_bytes by service action`
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(spl)+"&limit=10", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Mode      string `json:"mode"`
		SPL       string `json:"spl"`
		Index     string `json:"index"`
		ElapsedMS int64  `json:"elapsed_ms"`
		Command   struct {
			PluginCode    string `json:"plugin_code"`
			PluginType    string `json:"plugin_type"`
			PluginVersion string `json:"plugin_version"`
			Runtime       string `json:"runtime"`
			OutputMode    string `json:"output_mode"`
		} `json:"search_command"`
		Stats struct {
			Fields []string         `json:"fields"`
			Rows   []map[string]any `json:"rows"`
		} `json:"stats"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if response.Mode != "stats" || response.SPL != spl || response.Index != index {
		t.Fatalf("response contract = %#v", response)
	}
	if response.Command.PluginCode != "stats" || response.Command.PluginType != "search" || response.Command.PluginVersion != "1.0.0" || response.Command.Runtime != "go_builtin" || response.Command.OutputMode != "stats" {
		t.Fatalf("search command contract = %#v", response.Command)
	}
	for _, want := range []string{"service", "action", "total", "total_bytes", "avg_bytes", "min_bytes", "max_bytes"} {
		if !containsString(response.Stats.Fields, want) {
			t.Fatalf("fields = %#v, missing %s", response.Stats.Fields, want)
		}
	}
	if len(response.Stats.Rows) != 2 {
		t.Fatalf("rows = %d, want 2: %#v", len(response.Stats.Rows), response.Stats.Rows)
	}
	row := response.Stats.Rows[0]
	if row["service"] != "api" || row["action"] != "allow" || row["total"] != float64(2) || row["total_bytes"] != float64(300) || row["avg_bytes"] != float64(150) {
		t.Fatalf("first stats row = %#v", row)
	}
}

func TestSearchAPIStatsByBareSourceUsesSourceName(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "stats_source"
	appendSearchEventWithSource(index, "Firewall Syslog", "deny", 2048)
	appendSearchEventWithSource(index, "Firewall Syslog", "allow", 1024)

	spl := `index=stats_source | stats count as total by source`
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(spl)+"&limit=10", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Mode  string `json:"mode"`
		Stats struct {
			Fields []string         `json:"fields"`
			Rows   []map[string]any `json:"rows"`
		} `json:"stats"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if response.Mode != "stats" || len(response.Stats.Rows) != 1 {
		t.Fatalf("response = %#v", response)
	}
	if !containsString(response.Stats.Fields, "source") || !containsString(response.Stats.Fields, "total") {
		t.Fatalf("fields = %#v", response.Stats.Fields)
	}
	if response.Stats.Rows[0]["source"] != "Firewall Syslog" || response.Stats.Rows[0]["total"] != float64(2) {
		t.Fatalf("stats row = %#v", response.Stats.Rows[0])
	}
}

func TestSearchAPIStatsReturnsStandardErrorCode(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	spl := `index=searchp0 | stats values(raw) by service`
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(spl), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("search status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Error.Code != "SPL_STATS_UNSUPPORTED_FUNCTION" {
		t.Fatalf("error code = %q, want SPL_STATS_UNSUPPORTED_FUNCTION; response = %#v", response.Error.Code, response)
	}
	if !strings.Contains(response.Error.Message, "unsupported stats function") {
		t.Fatalf("error message = %q", response.Error.Message)
	}
}

func TestStatsSearchPluginIsRegistered(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	_, meta, err := handler.reg.Get(plugin.TypeSearch, "stats", "1.0.0")
	if err != nil {
		t.Fatalf("stats search plugin should be registered: %v", err)
	}
	if meta.Code != "stats" || meta.Type != plugin.TypeSearch || meta.Runtime != "go_builtin" {
		t.Fatalf("stats metadata = %#v", meta)
	}
	if meta.OutputSchema["mode"] != "stats" {
		t.Fatalf("stats output schema = %#v", meta.OutputSchema)
	}
}

func TestSearchAPIExecutesStatsThroughSearchPlugin(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	handler.reg = plugin.NewRegistry()
	if err := handler.reg.Register(plugin.Metadata{Code: "stats", Name: "Test Stats", Type: plugin.TypeSearch, Version: "1.0.0", Runtime: "go_builtin", Labels: map[string]string{"output_mode": "stats"}}, func() any {
		return failingStatsSearchPlugin{}
	}); err != nil {
		t.Fatalf("register test stats plugin: %v", err)
	}

	spl := `index=searchp0 | stats count by service`
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(spl), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("search status = %d, want 502 from search plugin, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "stats search plugin failed") {
		t.Fatalf("response body = %s, want stats plugin failure", rec.Body.String())
	}
}

type failingStatsSearchPlugin struct{}

func (failingStatsSearchPlugin) Metadata() plugin.Metadata {
	return plugin.Metadata{Code: "stats", Name: "Test Stats", Type: plugin.TypeSearch, Version: "1.0.0", Runtime: "go_builtin"}
}
func (failingStatsSearchPlugin) Validate(config map[string]any) error { return nil }
func (failingStatsSearchPlugin) Init(ctx plugin.InitContext, config map[string]any) error {
	return nil
}
func (failingStatsSearchPlugin) Execute(ctx context.Context, input plugin.SearchInput, query splstats.Query) (splstats.Result, error) {
	return splstats.Result{}, errors.New("stats plugin sentinel")
}
func (failingStatsSearchPlugin) Close() error { return nil }

func TestSearchAPIEventsP0ResponseContract(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "searchevents"
	appendSearchEvent(index, "api", "deny", 2048)

	spl := `index=searchevents action=deny`
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(spl)+"&limit=10", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Mode      string     `json:"mode"`
		SPL       string     `json:"spl"`
		Index     string     `json:"index"`
		TimeRange *timeRange `json:"time_range"`
		ElapsedMS int64      `json:"elapsed_ms"`
		Events    []struct {
			EventID  string         `json:"event_id"`
			Raw      string         `json:"raw"`
			Fields   map[string]any `json:"fields"`
			Metadata map[string]any `json:"metadata"`
			Display  struct {
				Time       string `json:"time"`
				Event      string `json:"event"`
				Expandable bool   `json:"expandable"`
			} `json:"display"`
			Detail struct {
				Raw       string `json:"raw"`
				FieldRows []struct {
					Category string `json:"category"`
					Name     string `json:"name"`
					Value    any    `json:"value"`
					Type     string `json:"type"`
				} `json:"field_rows"`
			} `json:"detail"`
		} `json:"events"`
		Pagination Pagination `json:"pagination"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if response.Mode != "events" || response.SPL != spl || response.Index != index {
		t.Fatalf("response contract = %#v", response)
	}
	if len(response.Events) != 1 || response.Pagination.Returned != 1 {
		t.Fatalf("events response = %#v", response)
	}
	if response.Pagination.Total != 1 {
		t.Fatalf("pagination.total = %d, want 1", response.Pagination.Total)
	}
	item := response.Events[0]
	if item.EventID == "" || item.Display.Time == "" || item.Display.Event == "" || !item.Display.Expandable {
		t.Fatalf("event display contract = %#v", item)
	}
	if item.Detail.Raw != item.Raw || len(item.Detail.FieldRows) == 0 {
		t.Fatalf("event detail contract = %#v", item.Detail)
	}
	for _, want := range []string{"metadata:index", "metadata:source", "metadata:sourcetype", "metadata:parse_status", "field:service", "field:action", "field:bytes"} {
		if !hasFieldRow(item.Detail.FieldRows, want) {
			t.Fatalf("field_rows missing %s: %#v", want, item.Detail.FieldRows)
		}
	}
	metadata := item.Metadata
	if metadata["parse_status"] != "parsed" || metadata["parse_rule_name"] != "Search Test Rule" || metadata["parse_error"] != "" {
		t.Fatalf("event metadata = %#v", metadata)
	}
}

func TestSearchAPIEventsPaginationUsesStableNewestFirstOrder(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "eventspage"
	appendSearchEventAt(index, "old", "deny", 100, time.Date(2026, 6, 27, 9, 0, 0, 0, time.UTC))
	appendSearchEventAt(index, "middle", "deny", 200, time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC))
	appendSearchEventAt(index, "new", "deny", 300, time.Date(2026, 6, 27, 11, 0, 0, 0, time.UTC))

	type pageResponse struct {
		Events []struct {
			Raw string `json:"raw"`
		} `json:"events"`
		Pagination Pagination `json:"pagination"`
	}
	fetch := func(page string) pageResponse {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(`index=eventspage action=deny`)+"&limit=1&page="+page, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("search page %s status = %d, body = %s", page, rec.Code, rec.Body.String())
		}
		var response pageResponse
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("decode page %s response: %v", page, err)
		}
		return response
	}

	first := fetch("1")
	if len(first.Events) != 1 || !strings.Contains(first.Events[0].Raw, "service=new") {
		t.Fatalf("first page events = %#v", first.Events)
	}
	if first.Pagination.Limit != 1 || first.Pagination.Page != 1 || first.Pagination.Offset != 0 || first.Pagination.Returned != 1 || first.Pagination.Total != 3 || !first.Pagination.HasMore {
		t.Fatalf("first page pagination = %#v", first.Pagination)
	}

	second := fetch("2")
	if len(second.Events) != 1 || !strings.Contains(second.Events[0].Raw, "service=middle") {
		t.Fatalf("second page events = %#v", second.Events)
	}
	if second.Pagination.Limit != 1 || second.Pagination.Page != 2 || second.Pagination.Offset != 1 || second.Pagination.Returned != 1 || second.Pagination.Total != 3 {
		t.Fatalf("second page pagination = %#v", second.Pagination)
	}
}

func TestSearchAPIDefaultPaginationLimitIsTwenty(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "defaultlimit"
	appendSearchEvent(index, "api", "deny", 100)
	appendSearchEvent(index, "worker", "allow", 200)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(`index=defaultlimit`), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Events     []SearchEvent `json:"events"`
		Pagination Pagination    `json:"pagination"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if response.Pagination.Limit != 20 || response.Pagination.Page != 1 || response.Pagination.Offset != 0 {
		t.Fatalf("pagination should default to 20 per page: %#v", response.Pagination)
	}
	if response.Pagination.Returned != len(response.Events) {
		t.Fatalf("returned = %d, events = %d", response.Pagination.Returned, len(response.Events))
	}
	if response.Pagination.Total != 2 {
		t.Fatalf("pagination.total = %d, want 2", response.Pagination.Total)
	}
}

func TestSearchAPIPaginationTotalSupportsDynamicPageNumbers(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "dynamicpages"
	for i := 0; i < 42; i++ {
		appendSearchEventAt(index, fmt.Sprintf("svc-%02d", i), "allow", i, time.Date(2026, 6, 27, 10, i%60, 0, 0, time.UTC))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(`index=dynamicpages`)+"&limit=20&page=3", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Events     []SearchEvent `json:"events"`
		Pagination Pagination    `json:"pagination"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if response.Pagination.Limit != 20 || response.Pagination.Page != 3 || response.Pagination.Offset != 40 {
		t.Fatalf("pagination location = %#v", response.Pagination)
	}
	if response.Pagination.Total != 42 || response.Pagination.Returned != 2 || response.Pagination.HasMore {
		t.Fatalf("pagination total semantics = %#v", response.Pagination)
	}
	if len(response.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(response.Events))
	}
}

func TestNormalizedPageLimitDefaultsAndCapsAtOneThousand(t *testing.T) {
	cases := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "missing", limit: 0, want: 20},
		{name: "negative", limit: -5, want: 20},
		{name: "allowed", limit: 100, want: 100},
		{name: "max", limit: 1000, want: 1000},
		{name: "over max caps", limit: 5000, want: 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizedPageLimit(tc.limit); got != tc.want {
				t.Fatalf("normalizedPageLimit(%d) = %d, want %d", tc.limit, got, tc.want)
			}
		})
	}
}

func TestSearchAPINormalizesSpacesAroundEquals(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "searchspaces"
	appendSearchEvent(index, "api", "deny", 2048)

	queries := []string{
		"index=searchspaces action=deny",
		"index= searchspaces action=deny",
		"index =searchspaces action=deny",
		"index = searchspaces action = deny",
	}
	for _, spl := range queries {
		t.Run(spl, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(spl)+"&limit=10", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("search status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var response struct {
				Mode   string `json:"mode"`
				Index  string `json:"index"`
				Events []struct {
					EventID string `json:"event_id"`
				} `json:"events"`
				Pagination Pagination `json:"pagination"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
				t.Fatalf("decode search response: %v", err)
			}
			if response.Mode != "events" || response.Index != index || response.Pagination.Returned != 1 || len(response.Events) != 1 {
				t.Fatalf("search response = %#v", response)
			}
		})
	}
}

func TestSearchAPIStatsPaginationAppliesAfterAggregation(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "statspage"
	appendSearchEvent(index, "api", "allow", 100)
	appendSearchEvent(index, "api", "allow", 200)
	appendSearchEvent(index, "worker", "deny", 50)
	appendSearchEvent(index, "batch", "allow", 75)

	type statsPageResponse struct {
		Mode  string `json:"mode"`
		Stats struct {
			Fields []string         `json:"fields"`
			Rows   []map[string]any `json:"rows"`
			Limit  int              `json:"limit"`
		} `json:"stats"`
		Pagination *Pagination `json:"pagination"`
	}
	fetch := func(page string) statsPageResponse {
		spl := `index=statspage | stats count as total by service action`
		req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(spl)+"&limit=1&page="+page, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("stats page %s status = %d, body = %s", page, rec.Code, rec.Body.String())
		}
		var response statsPageResponse
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("decode stats page %s response: %v", page, err)
		}
		return response
	}

	first := fetch("1")
	if first.Mode != "stats" || len(first.Stats.Rows) != 1 {
		t.Fatalf("first stats page = %#v", first)
	}
	if first.Stats.Rows[0]["service"] != "api" || first.Stats.Rows[0]["action"] != "allow" || first.Stats.Rows[0]["total"] != float64(2) {
		t.Fatalf("first stats row should aggregate all matching events before paging: %#v", first.Stats.Rows[0])
	}
	if first.Pagination == nil || first.Pagination.Limit != 1 || first.Pagination.Page != 1 || first.Pagination.Returned != 1 || !first.Pagination.HasMore {
		t.Fatalf("first stats pagination = %#v", first.Pagination)
	}

	second := fetch("2")
	if second.Mode != "stats" || len(second.Stats.Rows) != 1 {
		t.Fatalf("second stats page = %#v", second)
	}
	if second.Stats.Rows[0]["service"] == "api" && second.Stats.Rows[0]["action"] == "allow" {
		t.Fatalf("second stats page repeated first row: %#v", second.Stats.Rows[0])
	}
	if second.Pagination == nil || second.Pagination.Limit != 1 || second.Pagination.Page != 2 || second.Pagination.Offset != 1 || second.Pagination.Returned != 1 {
		t.Fatalf("second stats pagination = %#v", second.Pagination)
	}
}

func TestSearchAPIParseStatusFilteringAndFields(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "parsestatus"
	appendSearchEvent(index, "api", "deny", 2048)
	failed := event.New("broken raw", event.Source{Type: "syslog", Name: "Firewall Syslog"}, time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC))
	failed.Metadata["index"] = index
	failed.Metadata["sourcetype"] = "Firewall Regex"
	failed.Metadata["parse_status"] = "parse_failed"
	failed.Metadata["parse_rule_id"] = "pr_firewall_regex"
	failed.Metadata["parse_rule_name"] = "Firewall Regex"
	failed.Metadata["parse_error"] = "PARSE_FAILED: regex did not match"
	memoryoutput.DefaultStore().Append(failed)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(`index=parsestatus parse_status=parse_failed`), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Events []struct {
			Metadata map[string]any `json:"metadata"`
		} `json:"events"`
		Pagination Pagination `json:"pagination"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if len(response.Events) != 1 || response.Events[0].Metadata["parse_status"] != "parse_failed" || response.Events[0].Metadata["parse_error"] == "" {
		t.Fatalf("parse status search response = %#v", response)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/search/fields?q="+url.QueryEscape(`index=parsestatus`), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fields status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var fields FieldsResponse
	if err := json.NewDecoder(rec.Body).Decode(&fields); err != nil {
		t.Fatalf("decode fields response: %v", err)
	}
	for _, want := range []string{"source", "sourcetype", "parse_status", "parse_rule_id", "parse_rule_name", "parse_error"} {
		if !hasFieldSummary(fields.Fields, want) {
			t.Fatalf("fields = %#v, missing %s", fields.Fields, want)
		}
	}
}

func TestSearchAPISupportsEarliestLatestTimeExpressions(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "searchtime"
	appendSearchEventAt(index, "api", "allow", 128, time.Now().UTC())

	spl := `index=searchtime`
	params := url.Values{}
	params.Set("q", spl)
	params.Set("earliest", "@d")
	params.Set("latest", "now")
	params.Set("limit", "10")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?"+params.Encode(), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Mode      string `json:"mode"`
		Index     string `json:"index"`
		TimeRange struct {
			Earliest  string `json:"earliest"`
			Latest    string `json:"latest"`
			StartTime string `json:"start_time"`
			EndTime   string `json:"end_time"`
		} `json:"time_range"`
		Events     []any      `json:"events"`
		Pagination Pagination `json:"pagination"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if response.Mode != "events" || response.Index != index || len(response.Events) != 1 || response.Pagination.Returned != 1 {
		t.Fatalf("search response = %#v", response)
	}
	if response.TimeRange.Earliest != "@d" || response.TimeRange.Latest != "now" || response.TimeRange.StartTime == "" || response.TimeRange.EndTime == "" {
		t.Fatalf("time_range = %#v", response.TimeRange)
	}
	if !strings.Contains(response.TimeRange.StartTime, "+08:00") || !strings.Contains(response.TimeRange.EndTime, "+08:00") {
		t.Fatalf("time_range must be rendered in Asia/Shanghai timezone, got %#v", response.TimeRange)
	}
}

func TestSearchTimelineUsesSPLFiltersAndTimeRange(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "searchtimeline"
	appendSearchEventAt(index, "api", "deny", 128, time.Date(2026, 6, 27, 10, 30, 0, 0, time.UTC))
	appendSearchEventAt(index, "api", "allow", 256, time.Date(2026, 6, 27, 11, 30, 0, 0, time.UTC))

	params := url.Values{}
	params.Set("q", `index=searchtimeline action=deny`)
	params.Set("start_time", "2026-06-27T00:00:00Z")
	params.Set("end_time", "2026-06-28T00:00:00Z")
	params.Set("interval", "hour")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/timeline?"+params.Encode(), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("timeline status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response TimelineResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}
	if response.Interval != "hour" || len(response.Buckets) != 24 {
		t.Fatalf("timeline response = %#v", response)
	}
	total := 0
	nonEmptyStart := ""
	for _, bucket := range response.Buckets {
		total += bucket.Count
		if bucket.Count > 0 {
			nonEmptyStart = bucket.Start
		}
	}
	if total != 1 || nonEmptyStart != "2026-06-27T18:00:00+08:00" || !strings.Contains(response.Buckets[0].Start, "+08:00") {
		t.Fatalf("timeline buckets = %#v", response.Buckets)
	}
}

func TestSearchTimelineWithoutTimeRangeUsesAllMatchingEvents(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "searchtimelineall"
	appendSearchEventAt(index, "api", "deny", 128, time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC))

	params := url.Values{}
	params.Set("q", `index=searchtimelineall action=deny`)
	params.Set("interval", "day")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/timeline?"+params.Encode(), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("timeline status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response TimelineResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}
	if response.Interval != "day" || len(response.Buckets) != 1 || response.Buckets[0].Count != 1 {
		t.Fatalf("timeline response = %#v", response)
	}
}

func TestSearchTimelineCountsAllMatchingEventsBeyondSearchPageLimit(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "timelinelarge"
	for i := 0; i < 1205; i++ {
		appendSearchEventAt(index, "api", "deny", i, time.Date(2026, 6, 27, 10, i%60, 0, 0, time.UTC))
	}

	params := url.Values{}
	params.Set("q", `index=timelinelarge action=deny | stats count by service`)
	params.Set("start_time", "2026-06-27T00:00:00Z")
	params.Set("end_time", "2026-06-28T00:00:00Z")
	params.Set("interval", "hour")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/timeline?"+params.Encode(), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("timeline status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response TimelineResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}
	total := 0
	for _, bucket := range response.Buckets {
		total += bucket.Count
	}
	if total != 1205 {
		t.Fatalf("timeline total = %d, want all matching events 1205; buckets = %#v", total, response.Buckets)
	}
}

func TestSearchTimelineAutoIntervalReturnsContinuousBuckets(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "searchtimelineauto"
	appendSearchEventAt(index, "api", "deny", 128, time.Date(2026, 6, 27, 0, 30, 0, 0, time.UTC))
	appendSearchEventAt(index, "api", "deny", 256, time.Date(2026, 6, 27, 2, 30, 0, 0, time.UTC))

	params := url.Values{}
	params.Set("q", `index=searchtimelineauto action=deny`)
	params.Set("start_time", "2026-06-27T08:00:00+08:00")
	params.Set("end_time", "2026-06-27T11:00:00+08:00")
	params.Set("interval", "auto")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/timeline?"+params.Encode(), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("timeline status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response TimelineResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}
	if response.Interval != "hour" {
		t.Fatalf("auto interval = %q, want hour", response.Interval)
	}
	if len(response.Buckets) != 3 {
		t.Fatalf("bucket count = %d, response = %#v", len(response.Buckets), response)
	}
	counts := []int{response.Buckets[0].Count, response.Buckets[1].Count, response.Buckets[2].Count}
	if !reflect.DeepEqual(counts, []int{1, 0, 1}) {
		t.Fatalf("bucket counts = %#v", counts)
	}
	if response.Buckets[0].Start != "2026-06-27T08:00:00+08:00" || response.Buckets[0].End != "2026-06-27T09:00:00+08:00" {
		t.Fatalf("first bucket = %#v", response.Buckets[0])
	}
	if response.TimeRange == nil || !strings.Contains(response.TimeRange.StartTime, "+08:00") || !strings.Contains(response.TimeRange.EndTime, "+08:00") {
		t.Fatalf("time_range must be rendered in Asia/Shanghai timezone, got %#v", response.TimeRange)
	}
}

func TestSearchTimelineAutoIntervalUsesMonthForLargeRange(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	index := "searchtimelinemonth"
	appendSearchEventAt(index, "api", "deny", 128, time.Date(2026, 1, 15, 2, 0, 0, 0, time.UTC))
	appendSearchEventAt(index, "api", "deny", 256, time.Date(2026, 3, 2, 1, 0, 0, 0, time.UTC))

	params := url.Values{}
	params.Set("q", `index=searchtimelinemonth action=deny`)
	params.Set("start_time", "2026-01-01T00:00:00+08:00")
	params.Set("end_time", "2026-05-01T00:00:00+08:00")
	params.Set("interval", "auto")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/timeline?"+params.Encode(), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("timeline status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response TimelineResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}
	if response.Interval != "month" {
		t.Fatalf("auto interval = %q, want month", response.Interval)
	}
	if len(response.Buckets) != 4 {
		t.Fatalf("bucket count = %d, response = %#v", len(response.Buckets), response)
	}
	counts := []int{response.Buckets[0].Count, response.Buckets[1].Count, response.Buckets[2].Count, response.Buckets[3].Count}
	if !reflect.DeepEqual(counts, []int{1, 0, 1, 0}) {
		t.Fatalf("bucket counts = %#v", counts)
	}
	if response.Buckets[0].Start != "2026-01-01T00:00:00+08:00" || response.Buckets[3].End != "2026-05-01T00:00:00+08:00" {
		t.Fatalf("monthly buckets = %#v", response.Buckets)
	}
}

type timeRange struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

func appendSearchEvent(index string, service string, action string, bytesValue int) {
	now := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	appendSearchEventAt(index, service, action, bytesValue, now)
}

func appendSearchEventWithSource(index string, source string, action string, bytesValue int) {
	now := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	e := event.New("source="+source+" action="+action, event.Source{Type: "syslog", Name: source}, now)
	e.Metadata["index"] = index
	e.Fields["action"] = action
	e.Fields["bytes"] = bytesValue
	memoryoutput.DefaultStore().Append(e)
}

func hasFieldSummary(items []FieldSummary, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func hasFieldRow(items any, want string) bool {
	category, name, ok := strings.Cut(want, ":")
	if !ok {
		return false
	}
	values := reflect.ValueOf(items)
	for i := 0; i < values.Len(); i++ {
		row := values.Index(i)
		if row.FieldByName("Category").String() == category && row.FieldByName("Name").String() == name {
			return true
		}
	}
	return false
}

func appendSearchEventAt(index string, service string, action string, bytesValue int, eventTime time.Time) {
	e := event.New("service="+service+" action="+action, event.Source{Type: "test", Name: "test"}, eventTime)
	e.Metadata["index"] = index
	e.Metadata["sourcetype"] = "Search Test Rule"
	e.Metadata["parse_status"] = "parsed"
	e.Metadata["parse_rule_id"] = "pr_search_test"
	e.Metadata["parse_rule_name"] = "Search Test Rule"
	e.Metadata["parse_error"] = ""
	e.Fields["service"] = service
	e.Fields["action"] = action
	e.Fields["bytes"] = bytesValue
	memoryoutput.DefaultStore().Append(e)
}
