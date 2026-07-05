package mvp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mysqlstore "xdp/pkg/storage/mysql"
)

func TestCollectConfigAPIManagesSyslogDataSources(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/input-plugins", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("input plugins status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var pluginResponse struct {
		Plugins []struct {
			Code                string         `json:"code"`
			Name                string         `json:"name"`
			Status              string         `json:"status"`
			Version             string         `json:"version"`
			SchemaSummary       map[string]any `json:"schema_summary"`
			RuntimeCapabilities map[string]any `json:"runtime_capabilities"`
		} `json:"plugins"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&pluginResponse); err != nil {
		t.Fatalf("decode input plugins: %v", err)
	}
	seenSyslog := false
	seenKafka := false
	for _, plugin := range pluginResponse.Plugins {
		if plugin.Code == "syslog" {
			seenSyslog = true
			if plugin.Name == "" || plugin.Version != "1.0.0" || plugin.RuntimeCapabilities["runtime_ingest"] != true {
				t.Fatalf("syslog plugin = %#v", plugin)
			}
			required, _ := plugin.SchemaSummary["required"].([]any)
			if len(required) == 0 {
				t.Fatalf("syslog schema_summary missing required fields: %#v", plugin.SchemaSummary)
			}
		}
		if plugin.Code == "kafka" {
			seenKafka = true
			if plugin.Status != "disabled" || plugin.RuntimeCapabilities["runtime_ingest"] != false {
				t.Fatalf("kafka plugin = %#v", plugin)
			}
		}
	}
	if !seenSyslog || !seenKafka {
		t.Fatalf("plugins = %#v, want syslog and kafka", pluginResponse.Plugins)
	}

	port := freeUDPPort(t)
	body := `{"name":"Firewall Syslog P0","plugin_code":"syslog","status":"active","plugin_config":{"collector_port":` + strconv.Itoa(port) + `,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":true,"log_filter_regex":"action=(allow|deny)"}}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save collect datasource status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rawResponse := rec.Body.String()
	for _, forbidden := range []string{"default_index", "parser", "time_field", "raw_topic", "source", "sourcetype"} {
		if strings.Contains(rawResponse, `"`+forbidden+`"`) {
			t.Fatalf("collect datasource response exposes forbidden field %q: %s", forbidden, rawResponse)
		}
	}
	var source struct {
		ID               string         `json:"id"`
		Code             string         `json:"code"`
		Type             string         `json:"type"`
		Status           string         `json:"status"`
		RuntimeStatus    string         `json:"runtime_status"`
		ListenerStatus   string         `json:"listener_status"`
		ListenerEndpoint string         `json:"listener_endpoint"`
		PluginCode       string         `json:"plugin_code"`
		PluginVersion    string         `json:"plugin_version"`
		PluginRuntime    string         `json:"plugin_runtime"`
		InternalRawTopic string         `json:"internal_raw_topic"`
		PipelineID       string         `json:"pipeline_id"`
		PluginConfig     map[string]any `json:"plugin_config"`
	}
	if err := json.NewDecoder(strings.NewReader(rawResponse)).Decode(&source); err != nil {
		t.Fatalf("decode collect datasource: %v", err)
	}
	if source.ID == "" || source.Code == "" || source.Type != "syslog" || source.PluginCode != "syslog" || source.Status != "active" {
		t.Fatalf("saved source = %#v", source)
	}
	if source.PluginVersion != "1.0.0" || source.PluginRuntime != "go_builtin" {
		t.Fatalf("saved source plugin contract = version=%q runtime=%q", source.PluginVersion, source.PluginRuntime)
	}
	if source.InternalRawTopic != "raw.ds_firewall_syslog_p0" {
		t.Fatalf("internal raw topic = %q, want raw.ds_firewall_syslog_p0", source.InternalRawTopic)
	}
	if source.PipelineID == "" || source.PluginConfig["collector_port"] == nil {
		t.Fatalf("saved source missing runtime config: %#v", source)
	}
	for _, removed := range []string{"ip_acl", "keep_unparsed_raw", "max_event_size_m"} {
		if _, ok := source.PluginConfig[removed]; ok {
			t.Fatalf("saved source exposes removed plugin_config field %q: %#v", removed, source.PluginConfig)
		}
	}
	if source.RuntimeStatus != "running" || source.ListenerStatus != "listening" || source.ListenerEndpoint != "udp://0.0.0.0:"+strconv.Itoa(port) {
		t.Fatalf("runtime fields after create = status=%q listener=%q endpoint=%q", source.RuntimeStatus, source.ListenerStatus, source.ListenerEndpoint)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/datasources/"+source.ID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get datasource status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var detail DataSource
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatalf("decode datasource detail: %v", err)
	}
	if detail.RuntimeStatus != "running" || detail.ListenerStatus != "listening" {
		t.Fatalf("detail runtime fields = %#v", detail)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/datasources?plugin_code=syslog&status=active&keyword=Firewall", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list datasources status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		DataSources []struct {
			ID         string `json:"id"`
			PluginCode string `json:"plugin_code"`
			Status     string `json:"status"`
		} `json:"datasources"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode datasource list: %v", err)
	}
	found := false
	for _, item := range list.DataSources {
		if item.ID == source.ID && item.PluginCode == "syslog" && item.Status == "active" {
			found = true
		}
	}
	if !found {
		t.Fatalf("datasources = %#v, want %s", list.DataSources, source.ID)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/datasources/"+source.ID+"/status", strings.NewReader(`{"status":"disabled"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch datasource status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var statusResponse DataSource
	if err := json.NewDecoder(rec.Body).Decode(&statusResponse); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if statusResponse.Status != "disabled" || statusResponse.RuntimeStatus != "stopped" || statusResponse.ListenerStatus != "stopped" {
		t.Fatalf("status response = %#v", statusResponse)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/datasources/"+source.ID+"/runtime", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var runtimeResponse struct {
		ID                  string `json:"id"`
		DesiredStatus       string `json:"desired_status"`
		RuntimeStatus       string `json:"runtime_status"`
		ListenerStatus      string `json:"listener_status"`
		Endpoint            string `json:"endpoint"`
		Protocol            string `json:"protocol"`
		Port                int    `json:"port"`
		AgentID             string `json:"agent_id"`
		PipelineID          string `json:"pipeline_id"`
		ConfigVersion       int64  `json:"config_version"`
		LastLoadedAt        string `json:"last_loaded_at"`
		LastHeartbeatAt     string `json:"last_heartbeat_at"`
		LastTransitionAt    string `json:"last_transition_at"`
		ReceivedEventsTotal int64  `json:"received_events_total"`
		ReceivedBytesTotal  int64  `json:"received_bytes_total"`
		LastReceivedAt      string `json:"last_received_at"`
		LastErrorCode       string `json:"last_error_code"`
		LastError           string `json:"last_error"`
		ParseRuleName       string `json:"parse_rule_name"`
		Sourcetype          string `json:"sourcetype"`
		OutputIndex         string `json:"output_index"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&runtimeResponse); err != nil {
		t.Fatalf("decode runtime response: %v", err)
	}
	if runtimeResponse.ID != source.ID || runtimeResponse.DesiredStatus != "disabled" || runtimeResponse.RuntimeStatus != "stopped" || runtimeResponse.ListenerStatus != "stopped" || runtimeResponse.Endpoint != "udp://0.0.0.0:"+strconv.Itoa(port) || runtimeResponse.Protocol != "udp" || runtimeResponse.Port != port || runtimeResponse.AgentID == "" {
		t.Fatalf("runtime response = %#v", runtimeResponse)
	}
	if runtimeResponse.PipelineID == "" || runtimeResponse.ConfigVersion < 1 {
		t.Fatalf("runtime response missing pipeline/config version: %#v", runtimeResponse)
	}
	if runtimeResponse.LastLoadedAt == "" || runtimeResponse.LastHeartbeatAt == "" || runtimeResponse.LastTransitionAt == "" {
		t.Fatalf("runtime response missing timestamps: %#v", runtimeResponse)
	}
	if runtimeResponse.ReceivedEventsTotal < 0 || runtimeResponse.ReceivedBytesTotal < 0 {
		t.Fatalf("runtime response counters must be non-negative: %#v", runtimeResponse)
	}
	if runtimeResponse.ParseRuleName != "未绑定解析规则" || runtimeResponse.Sourcetype != "未绑定解析规则" || runtimeResponse.OutputIndex != "未指定 index" {
		t.Fatalf("runtime response topology without parse rule = %#v", runtimeResponse)
	}

	parseBody := `{"name":"Firewall Regex","status":"active","parser_plugin":"regex","data_source_name":"Firewall Syslog P0","output_index":"audit","sample_event":"src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048","plugin_config":{"regex_pattern":"src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+action=(?<action>\\S+)\\s+bytes=(?<bytes>\\d+)"},"props_conf":"[source::Firewall Syslog P0]\nEXTRACT-main = src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+action=(?<action>\\S+)\\s+bytes=(?<bytes>\\d+)" }`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/parse-rules", strings.NewReader(parseBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create parse rule = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/datasources/"+source.ID+"/runtime", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime status with parse rule = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&runtimeResponse); err != nil {
		t.Fatalf("decode runtime response with parse rule: %v", err)
	}
	if runtimeResponse.ParseRuleName != "Firewall Regex" || runtimeResponse.Sourcetype != "Firewall Regex" || runtimeResponse.OutputIndex != "audit" {
		t.Fatalf("runtime response topology with parse rule = %#v", runtimeResponse)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/datasources/"+source.ID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete datasource status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/datasources/"+source.ID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
}

func TestRuntimeTopologyFromRulesUsesBoundParseRule(t *testing.T) {
	source := DataSource{ID: "firewall-syslog", Name: "Firewall Syslog", DefaultIndex: "app", InternalRawTopic: "raw.ds_firewall_syslog"}
	rules := []ParseRule{
		{Name: "Other Regex", Status: "active", Stage: "ingest", DataSourceName: "Other Syslog", OutputIndex: "other", Priority: 10},
		{Name: "Firewall Regex", Status: "active", Stage: "ingest", DataSourceName: "Firewall Syslog", OutputIndex: "audit", Priority: 100},
	}

	topology := runtimeTopologyFromRules(source, rules)

	if topology.ParseRuleName != "Firewall Regex" || topology.Sourcetype != "Firewall Regex" || topology.OutputIndex != "audit" {
		t.Fatalf("runtime topology = %#v", topology)
	}
}

func TestRuntimeTopologyFromRulesDoesNotFallbackToAppWithoutParseRule(t *testing.T) {
	source := DataSource{ID: "firewall-syslog", Name: "Firewall Syslog", DefaultIndex: "app", InternalRawTopic: "raw.ds_firewall_syslog"}

	topology := runtimeTopologyFromRules(source, nil)

	if topology.ParseRuleName != "未绑定解析规则" || topology.Sourcetype != "未绑定解析规则" || topology.OutputIndex != "未指定 index" {
		t.Fatalf("runtime topology without parse rule = %#v", topology)
	}
}

func TestCollectConfigRejectsSyslogTCPInP0(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	body := `{"name":"Firewall TCP","plugin_code":"syslog","status":"active","plugin_config":{"collector_port":5515,"transport_protocol":"TCP","encoding":"UTF-8","log_filter_enabled":false}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	raw := rec.Body.String()
	if !strings.Contains(raw, "VALIDATION_ERROR") || !strings.Contains(raw, "UDP") {
		t.Fatalf("expected UDP-only validation error, got %s", raw)
	}
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.ListenPacket("udp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("find udp port: %v", err)
	}
	port := listener.LocalAddr().(*net.UDPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close udp port probe: %v", err)
	}
	return port
}

func TestCollectConfigRejectsDuplicateDataSourceName(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	first := `{"name":"Duplicate Syslog","plugin_code":"syslog","status":"disabled","plugin_config":{"collector_port":5515,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(first))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first save status = %d, body = %s", rec.Code, rec.Body.String())
	}

	second := `{"name":" Duplicate Syslog ","plugin_code":"syslog","status":"disabled","plugin_config":{"collector_port":5516,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(second))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertErrorResponse(t, rec, http.StatusConflict, "DATASOURCE_NAME_EXISTS")
}

func TestCollectConfigAllowsNameReuseAfterDelete(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	first := `{"name":"Reusable Syslog","plugin_code":"syslog","status":"disabled","plugin_config":{"collector_port":5515,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(first))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first save status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var firstSource DataSource
	if err := json.NewDecoder(rec.Body).Decode(&firstSource); err != nil {
		t.Fatalf("decode first datasource: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/datasources/"+firstSource.ID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", rec.Code, rec.Body.String())
	}

	second := `{"name":" Reusable Syslog ","plugin_code":"syslog","status":"disabled","plugin_config":{"collector_port":5516,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(second))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second save status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var secondSource DataSource
	if err := json.NewDecoder(rec.Body).Decode(&secondSource); err != nil {
		t.Fatalf("decode second datasource: %v", err)
	}
	if secondSource.Name != "Reusable Syslog" || secondSource.Status != "disabled" {
		t.Fatalf("reused datasource = %#v, first = %#v", secondSource, firstSource)
	}
	if port, _ := intConfig(secondSource.PluginConfig, "collector_port"); port != 5516 {
		t.Fatalf("reused datasource did not save new config: %#v", secondSource.PluginConfig)
	}
}

func TestCollectConfigPrunesRemovedSyslogFields(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	port := freeUDPPort(t)
	body := `{"name":"Pruned Syslog","plugin_code":"syslog","status":"active","plugin_config":{"collector_port":` + strconv.Itoa(port) + `,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false,"max_event_size_m":99,"keep_unparsed_raw":false,"ip_acl":[{"action":"deny","cidr":"10.0.0.0/8"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save collect datasource status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var source DataSource
	if err := json.NewDecoder(rec.Body).Decode(&source); err != nil {
		t.Fatalf("decode datasource: %v", err)
	}
	for _, removed := range []string{"ip_acl", "keep_unparsed_raw", "max_event_size_m"} {
		if _, ok := source.PluginConfig[removed]; ok {
			t.Fatalf("removed field %q should not be persisted: %#v", removed, source.PluginConfig)
		}
	}
}

func TestCollectConfigPortCheckRejectsOccupiedUDPPort(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	listener, err := net.ListenPacket("udp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("reserve udp port: %v", err)
	}
	defer listener.Close()
	port := listener.LocalAddr().(*net.UDPAddr).Port

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"plugin_code":"syslog","transport_protocol":"UDP","collector_port":` + strconv.Itoa(port) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources/port-check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	raw := rec.Body.String()
	assertErrorResponse(t, rec, http.StatusConflict, "LISTENER_PORT_UNAVAILABLE")
	if !strings.Contains(raw, "端口不可用") {
		t.Fatalf("error body should mention unavailable port, got %s", raw)
	}
}

func TestCollectConfigPortCheckAcceptsAvailableUDPPort(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	listener, err := net.ListenPacket("udp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("find udp port: %v", err)
	}
	port := listener.LocalAddr().(*net.UDPAddr).Port
	listener.Close()

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"plugin_code":"syslog","transport_protocol":"UDP","collector_port":` + strconv.Itoa(port) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources/port-check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("port check status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Available         bool   `json:"available"`
		CollectorPort     int    `json:"collector_port"`
		TransportProtocol string `json:"transport_protocol"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode port check response: %v", err)
	}
	if !response.Available || response.CollectorPort != port || response.TransportProtocol != "UDP" {
		t.Fatalf("port check response = %#v", response)
	}
}

func TestCollectConfigPortCheckUsesConfiguredAgent(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	agentCalled := false
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/port-check" {
			t.Fatalf("unexpected agent request: %s %s", r.Method, r.URL.Path)
		}
		agentCalled = true
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": map[string]any{"code": "LISTENER_PORT_UNAVAILABLE", "message": "端口不可用"},
		})
	}))
	defer agent.Close()
	t.Setenv("XDP_AGENT_BASE_URL", agent.URL)

	listener, err := net.ListenPacket("udp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("find udp port: %v", err)
	}
	port := listener.LocalAddr().(*net.UDPAddr).Port
	listener.Close()

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"plugin_code":"syslog","transport_protocol":"UDP","collector_port":` + strconv.Itoa(port) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources/port-check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !agentCalled {
		t.Fatalf("expected api to call configured agent")
	}
	assertErrorResponse(t, rec, http.StatusConflict, "LISTENER_PORT_UNAVAILABLE")
}

func TestCollectConfigCreateRejectsOccupiedUDPPort(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	listener, err := net.ListenPacket("udp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("reserve udp port: %v", err)
	}
	defer listener.Close()
	port := listener.LocalAddr().(*net.UDPAddr).Port

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"name":"Occupied Syslog","plugin_code":"syslog","status":"active","plugin_config":{"collector_port":` + strconv.Itoa(port) + `,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusConflict, "LISTENER_PORT_UNAVAILABLE")
}

func TestCollectConfigAPIValidatesSyslogRequests(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "status is required",
			body:       `{"name":"Missing Status","plugin_code":"syslog","plugin_config":{"collector_port":5514,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "invalid port",
			body:       `{"name":"Bad Port","plugin_code":"syslog","status":"active","plugin_config":{"collector_port":70000,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "transport protocol is required",
			body:       `{"name":"Missing Protocol","plugin_code":"syslog","status":"active","plugin_config":{"collector_port":5514,"encoding":"UTF-8","log_filter_enabled":false}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "encoding is required",
			body:       `{"name":"Missing Encoding","plugin_code":"syslog","status":"active","plugin_config":{"collector_port":5514,"transport_protocol":"UDP","log_filter_enabled":false}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "log filter flag is required",
			body:       `{"name":"Missing Filter Flag","plugin_code":"syslog","status":"active","plugin_config":{"collector_port":5514,"transport_protocol":"UDP","encoding":"UTF-8"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "enabled filter requires regex",
			body:       `{"name":"Missing Regex","plugin_code":"syslog","status":"active","plugin_config":{"collector_port":5514,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":true}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "invalid filter regex",
			body:       `{"name":"Bad Regex","plugin_code":"syslog","status":"active","plugin_config":{"collector_port":5514,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":true,"log_filter_regex":"("}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "client cannot submit internal route",
			body:       `{"name":"Manual Route","plugin_code":"syslog","status":"active","internal_raw_topic":"raw.manual","plugin_config":{"collector_port":5514,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "client cannot submit legacy raw topic",
			body:       `{"name":"Manual Raw Topic","plugin_code":"syslog","status":"active","raw_topic":"raw.manual","plugin_config":{"collector_port":5514,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "client cannot submit source alias",
			body:       `{"name":"Manual Source","plugin_code":"syslog","status":"active","source":"edge-fw-01","plugin_config":{"collector_port":5514,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "client cannot submit sourcetype alias",
			body:       `{"name":"Manual Sourcetype","plugin_code":"syslog","status":"active","sourcetype":"syslog","plugin_config":{"collector_port":5514,"transport_protocol":"UDP","encoding":"UTF-8","log_filter_enabled":false}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "kafka is not enabled in p0 runtime",
			body:       `{"name":"Kafka Stream","plugin_code":"kafka","plugin_config":{"brokers":"10.0.0.1:9092","topic":"xdp-events","consumer_group":"xdp"}}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "PLUGIN_RUNTIME_DISABLED",
		},
		{
			name:       "unknown input plugin is not supported",
			body:       `{"name":"Unknown Stream","plugin_code":"file","plugin_config":{"path":"/tmp/app.log"}}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "PLUGIN_NOT_SUPPORTED",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/datasources", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertErrorResponse(t, rec, tc.wantStatus, tc.wantCode)
		})
	}
}

func TestLegacyRawTopicMigratesToInternalRawTopic(t *testing.T) {
	item := mysqlstore.DataSource{
		Code:   "legacy-source",
		Type:   "syslog",
		Name:   "Legacy Syslog",
		Status: "active",
		Config: []byte(`{"id":"legacy-source","name":"Legacy Syslog","type":"syslog","status":"active","source":"edge-fw-01","sourcetype":"syslog","raw_topic":"xdp.raw.legacy","pipeline_id":"legacy-pipeline"}`),
	}

	source, err := dataSourceFromStore(item)
	if err != nil {
		t.Fatalf("dataSourceFromStore() error = %v", err)
	}
	if source.RawTopic != "" {
		t.Fatalf("legacy raw_topic leaked after migration: %q", source.RawTopic)
	}
	if source.InternalRawTopic != "xdp.raw.legacy" {
		t.Fatalf("internal_raw_topic = %q, want xdp.raw.legacy", source.InternalRawTopic)
	}
	if source.Source != "" || source.Sourcetype != "" {
		t.Fatalf("legacy source aliases leaked after migration: source=%q sourcetype=%q", source.Source, source.Sourcetype)
	}

	stored := dataSourceToStore(source)
	if strings.Contains(string(stored.Config), `"raw_topic"`) {
		t.Fatalf("stored config still contains raw_topic: %s", string(stored.Config))
	}
	if !strings.Contains(string(stored.Config), `"internal_raw_topic":"xdp.raw.legacy"`) {
		t.Fatalf("stored config missing migrated internal_raw_topic: %s", string(stored.Config))
	}
	if strings.Contains(string(stored.Config), `"source"`) || strings.Contains(string(stored.Config), `"sourcetype"`) {
		t.Fatalf("stored config still contains legacy source aliases: %s", string(stored.Config))
	}
}

func TestListDataSourcesPaginatesResults(t *testing.T) {
	t.Setenv("XDP_MYSQL_DISABLED", "true")
	t.Setenv("XDP_AUTH_ENABLED", "false")
	t.Setenv("XDP_OUTPUT", "")

	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
	handler.mu.Lock()
	handler.dataSources = map[string]DataSource{}
	for i := 1; i <= 25; i++ {
		id := "syslog-" + strconv.Itoa(i)
		handler.dataSources[id] = DataSource{
			ID:         id,
			Code:       id,
			Type:       "syslog",
			Name:       "Syslog " + strconv.Itoa(i),
			Status:     "active",
			PluginCode: "syslog",
			PluginConfig: map[string]any{
				"collector_port":     5500 + i,
				"transport_protocol": "UDP",
				"encoding":           "UTF-8",
			},
		}
	}
	handler.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasources?page=2", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list datasources status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		DataSources []DataSource `json:"datasources"`
		Pagination  struct {
			Page       int `json:"page"`
			PageSize   int `json:"page_size"`
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode datasource pagination: %v", err)
	}
	if len(response.DataSources) != 10 {
		t.Fatalf("datasources len = %d, want 10", len(response.DataSources))
	}
	if response.Pagination.Page != 2 || response.Pagination.PageSize != 10 || response.Pagination.Total != 25 || response.Pagination.TotalPages != 3 {
		t.Fatalf("pagination = %#v, want page=2 page_size=10 total=25 total_pages=3", response.Pagination)
	}
}
