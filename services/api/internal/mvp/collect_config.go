package mvp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	mysqlstore "xdp/pkg/storage/mysql"
)

type InputPlugin struct {
	Code                string         `json:"code"`
	Name                string         `json:"name"`
	Description         string         `json:"description"`
	Status              string         `json:"status"`
	Version             string         `json:"version"`
	ConfiguredCount     int            `json:"configured_count"`
	SchemaSummary       map[string]any `json:"schema_summary,omitempty"`
	RuntimeCapabilities map[string]any `json:"runtime_capabilities"`
}

type collectAPIError struct {
	status  int
	code    string
	message string
}

type dataSourcePortCheckRequest struct {
	PluginCode        string `json:"plugin_code"`
	TransportProtocol string `json:"transport_protocol"`
	CollectorPort     int    `json:"collector_port"`
	ListenHost        string `json:"listen_host,omitempty"`
}

type dataSourceConnectivityCheckRequest struct {
	PluginCode    string         `json:"plugin_code"`
	PluginConfig  map[string]any `json:"plugin_config"`
	CollectorPort int            `json:"collector_port,omitempty"`
}

func (h *Handler) listInputPlugins(w http.ResponseWriter, r *http.Request) {
	principal := h.currentPrincipal(r.Context())
	counts := map[string]int{}
	h.mu.RLock()
	for _, source := range h.dataSources {
		if source.Status == "deleted" {
			continue
		}
		code := collectPluginCode(source)
		if code != "" {
			counts[code]++
		}
	}
	h.mu.RUnlock()
	plugins := []InputPlugin{}
	if principal.AllowsPlugin("use", "input", "syslog") {
		plugins = append(plugins, InputPlugin{
			Code:            "syslog",
			Name:            "Syslog",
			Description:     "通过 UDP 监听接收 Syslog 日志，P0 支持保存并热加载到运行时。",
			Status:          "enabled",
			Version:         "1.0.0",
			ConfiguredCount: counts["syslog"],
			SchemaSummary: map[string]any{
				"required": []string{"collector_port", "transport_protocol", "encoding", "log_filter_enabled"},
				"conditional_required": map[string]string{
					"log_filter_regex": "log_filter_enabled=true",
				},
			},
			RuntimeCapabilities: map[string]any{
				"runtime_ingest": true,
				"hot_reload":     true,
				"protocols":      []string{"UDP"},
			},
		})
	}
	for _, plugin := range h.enabledImportedInputPlugins(r.Context(), principal, counts) {
		plugins = append(plugins, plugin)
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": plugins})
}

func (h *Handler) enabledImportedInputPlugins(ctx context.Context, principal AuthenticatedPrincipal, counts map[string]int) []InputPlugin {
	items := []PluginImportResponse{}
	if h.mysql != nil {
		if records, err := h.mysql.ListPluginRecords(ctx); err == nil {
			items = append(items, pluginResponsesFromRecords(records, "input")...)
		}
	}
	h.mu.RLock()
	for _, item := range h.importedPlugins {
		normalized := normalizePluginResponse(item)
		if normalized.PluginType == "input" {
			items = append(items, normalized)
		}
	}
	h.mu.RUnlock()
	items = deduplicatePluginResponses(items)
	out := make([]InputPlugin, 0, len(items))
	for _, item := range items {
		if productVisibleBuiltinPlugin(item.PluginType, item.PluginCode) || !isPluginEnabled(item.Status) || !principal.AllowsPlugin("use", item.PluginType, item.PluginCode) {
			continue
		}
		out = append(out, inputPluginFromImport(item, counts[item.PluginCode]))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

func inputPluginFromImport(item PluginImportResponse, configuredCount int) InputPlugin {
	return InputPlugin{
		Code:            item.PluginCode,
		Name:            firstNonEmpty(item.Name, item.PluginCode),
		Description:     item.Description,
		Status:          item.Status,
		Version:         item.PluginVersion,
		ConfiguredCount: configuredCount,
		SchemaSummary:   schemaSummary(item.ConfigSchema),
		RuntimeCapabilities: map[string]any{
			"runtime_ingest": true,
			"hot_reload":     true,
			"protocols":      []string{strings.ToUpper(item.PluginCode)},
		},
	}
}

func schemaSummary(schema map[string]any) map[string]any {
	summary := map[string]any{}
	if required := schemaStringList(schema["required"]); len(required) > 0 {
		summary["required"] = required
	}
	conditional := map[string]string{}
	for field, rawRule := range schemaProperties(schema) {
		rule, _ := rawRule.(map[string]any)
		if raw, ok := rule["x-required-if"].(map[string]any); ok {
			conditional[field] = fmt.Sprintf("%v=%v", raw["field"], raw["equals"])
		}
	}
	if len(conditional) > 0 {
		summary["conditional_required"] = conditional
	}
	return summary
}

func (h *Handler) checkDataSourcePort(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req dataSourcePortCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid port check request")
		return
	}
	if apiErr := validateDataSourcePortCheckRequest(req); apiErr != nil {
		writeErrorCode(w, apiErr.status, apiErr.code, apiErr.message)
		return
	}
	host := strings.TrimSpace(req.ListenHost)
	if host == "" {
		host = "0.0.0.0"
	}
	if err := h.checkListenerPort(r.Context(), host, req.TransportProtocol, req.CollectorPort); err != nil {
		writeErrorCode(w, http.StatusConflict, "LISTENER_PORT_UNAVAILABLE", "端口不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available":          true,
		"collector_port":     req.CollectorPort,
		"listen_host":        host,
		"transport_protocol": strings.ToUpper(req.TransportProtocol),
	})
}

func (h *Handler) checkDataSourceConnectivity(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req dataSourceConnectivityCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid connectivity check request")
		return
	}
	pluginCode := strings.ToLower(strings.TrimSpace(req.PluginCode))
	if pluginCode == "" {
		writeErrorCode(w, http.StatusBadRequest, "VALIDATION_ERROR", "plugin_code is required")
		return
	}
	config := req.PluginConfig
	if config == nil {
		config = map[string]any{}
	}
	if pluginCode != "kafka" {
		writeErrorCode(w, http.StatusUnprocessableEntity, "CONNECTIVITY_CHECK_NOT_SUPPORTED", "connectivity check only supports Kafka input plugins")
		return
	}
	if apiErr := h.validateImportedInputPluginConfig(pluginCode, config); apiErr != nil {
		writeErrorCode(w, apiErr.status, apiErr.code, apiErr.message)
		return
	}
	brokers := stringListConfig(config, "brokers")
	topic := strings.TrimSpace(stringConfig(config, "topic", ""))
	endpoint := fmt.Sprintf("kafka://%s/%s", strings.Join(brokers, ","), topic)
	if err := probeKafkaBrokers(r.Context(), brokers); err != nil {
		writeErrorCode(w, http.StatusConflict, "KAFKA_CONNECTIVITY_FAILED", "Kafka 连通性失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available":   true,
		"plugin_code": pluginCode,
		"endpoint":    endpoint,
		"message":     "Kafka 连通性正常",
	})
}

func (h *Handler) getDataSource(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	source, ok := h.findDataSource(id)
	if !ok || source.Status == "deleted" {
		writeErrorCode(w, http.StatusNotFound, "NOT_FOUND", "datasource not found")
		return
	}
	writeJSON(w, http.StatusOK, withRuntimeState(source))
}

func (h *Handler) updateDataSource(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	id := strings.TrimSpace(r.PathValue("id"))
	var source DataSource
	if err := json.NewDecoder(r.Body).Decode(&source); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid datasource json")
		return
	}
	if !isCollectDataSourceRequest(source) {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "collect datasource payload is required")
		return
	}
	if existing, ok := h.findDataSource(id); ok && existing.Status != "deleted" {
		existingPlugin := strings.ToLower(strings.TrimSpace(collectPluginCode(existing)))
		requestedPlugin := strings.ToLower(strings.TrimSpace(source.PluginCode))
		if requestedPlugin != "" && existingPlugin != requestedPlugin {
			writeErrorCode(w, http.StatusConflict, "PLUGIN_TYPE_IMMUTABLE", "采集插件类型不可修改")
			return
		}
	}
	h.saveCollectDataSource(w, r, source, id)
}

func (h *Handler) updateDataSourceStatus(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	id := strings.TrimSpace(r.PathValue("id"))
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid status request")
		return
	}
	status := normalizeStatus(req.Status)
	if status != "active" && status != "disabled" {
		writeErrorCode(w, http.StatusBadRequest, "VALIDATION_ERROR", "status must be active or disabled")
		return
	}
	source, ok := h.findDataSource(id)
	if !ok || source.Status == "deleted" {
		writeErrorCode(w, http.StatusNotFound, "NOT_FOUND", "datasource not found")
		return
	}
	source.Status = status
	source.UpdatedAt = time.Now().UTC()
	if err := h.persistDataSource(r, source, false); err != nil {
		writeError(w, http.StatusBadGateway, "save datasource failed")
		return
	}
	h.mu.Lock()
	h.dataSources[source.ID] = source
	h.runtimePipelines = h.buildRuntimePipelinesLocked()
	h.mu.Unlock()
	h.publishRuntimePipelines(r.Context())
	writeJSON(w, http.StatusOK, withRuntimeState(source))
}

func (h *Handler) getDataSourceRuntime(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	source, ok := h.findDataSource(id)
	if !ok || source.Status == "deleted" {
		writeErrorCode(w, http.StatusNotFound, "NOT_FOUND", "datasource not found")
		return
	}
	state := runtimeStateForDataSource(source)
	topology := h.runtimeTopologyForDataSource(source)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                    state.ID,
		"name":                  state.Name,
		"plugin_code":           state.PluginCode,
		"desired_status":        state.DesiredStatus,
		"runtime_status":        state.RuntimeStatus,
		"listener_status":       state.ListenerStatus,
		"endpoint":              state.Endpoint,
		"protocol":              state.Protocol,
		"port":                  state.Port,
		"agent_id":              state.AgentID,
		"pipeline_id":           state.PipelineID,
		"config_version":        state.ConfigVersion,
		"last_loaded_at":        state.LastLoadedAt.Format(time.RFC3339),
		"last_transition_at":    state.LastTransitionAt.Format(time.RFC3339),
		"last_heartbeat_at":     state.LastHeartbeatAt.Format(time.RFC3339),
		"last_received_at":      formatOptionalTime(state.LastReceivedAt),
		"received_events_total": state.ReceivedEvents,
		"received_bytes_total":  state.ReceivedBytes,
		"last_error_code":       state.LastErrorCode,
		"last_error":            state.LastError,
		"parse_rule_name":       topology.ParseRuleName,
		"sourcetype":            topology.Sourcetype,
		"output_index":          topology.OutputIndex,
	})
}

type dataSourceRuntimeTopology struct {
	ParseRuleName string
	Sourcetype    string
	OutputIndex   string
}

func (h *Handler) runtimeTopologyForDataSource(source DataSource) dataSourceRuntimeTopology {
	h.mu.RLock()
	rules := h.parseRulesForSourceLocked(source)
	h.mu.RUnlock()
	if len(rules) == 0 && h.mysql != nil {
		ctx, cancel := contextWithTimeout()
		defer cancel()
		if items, err := h.mysql.ListParseRules(ctx); err == nil {
			rules = make([]ParseRule, 0, len(items))
			for _, item := range items {
				rule, err := parseRuleFromStore(item)
				if err == nil {
					rules = append(rules, rule)
				}
			}
		}
	}
	return runtimeTopologyFromRules(source, rules)
}

func runtimeTopologyFromRules(source DataSource, rules []ParseRule) dataSourceRuntimeTopology {
	matched := make([]ParseRule, 0, len(rules))
	for _, rule := range rules {
		if parseRuleMatchesSource(rule, source) {
			matched = append(matched, rule)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Priority == matched[j].Priority {
			return matched[i].Code < matched[j].Code
		}
		return matched[i].Priority < matched[j].Priority
	})
	if len(matched) == 0 {
		return dataSourceRuntimeTopology{
			ParseRuleName: "未绑定解析规则",
			Sourcetype:    "未绑定解析规则",
			OutputIndex:   "未指定 index",
		}
	}
	rule := matched[0]
	parseRuleName := firstNonEmpty(strings.TrimSpace(rule.Name), strings.TrimSpace(rule.Sourcetype), strings.TrimSpace(rule.Code), "未绑定解析规则")
	return dataSourceRuntimeTopology{
		ParseRuleName: parseRuleName,
		Sourcetype:    firstNonEmpty(strings.TrimSpace(rule.Sourcetype), parseRuleName),
		OutputIndex:   firstNonEmpty(strings.TrimSpace(rule.OutputIndex), "未指定 index"),
	}
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func (h *Handler) deleteDataSource(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	source, ok := h.findDataSource(id)
	if !ok || source.Status == "deleted" {
		writeErrorCode(w, http.StatusNotFound, "NOT_FOUND", "datasource not found")
		return
	}
	source.Status = "deleted"
	source.UpdatedAt = time.Now().UTC()
	if err := h.persistDataSource(r, source, false); err != nil {
		writeError(w, http.StatusBadGateway, "save datasource failed")
		return
	}
	h.mu.Lock()
	delete(h.dataSources, source.ID)
	h.runtimePipelines = h.buildRuntimePipelinesLocked()
	h.mu.Unlock()
	h.publishRuntimePipelines(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "id": id})
}

func (h *Handler) saveCollectDataSource(w http.ResponseWriter, r *http.Request, source DataSource, pathID string) {
	principal := h.currentPrincipal(r.Context())
	if apiErr := h.validateCollectDataSourcePayload(source); apiErr != nil {
		writeErrorCode(w, apiErr.status, apiErr.code, apiErr.message)
		return
	}
	normalized, apiErr := h.normalizeCollectDataSource(source, pathID)
	if apiErr != nil {
		writeErrorCode(w, apiErr.status, apiErr.code, apiErr.message)
		return
	}
	if !h.authorizePluginScope(w, r, principal, "use", "input", collectPluginCode(normalized)) {
		return
	}
	if h.collectDataSourceNameExists(r.Context(), normalized.Name, normalized.ID) {
		writeErrorCode(w, http.StatusConflict, "DATASOURCE_NAME_EXISTS", "设备名称已存在")
		return
	}
	if pathID == "" && normalized.Status == "active" && collectPluginCode(normalized) == "syslog" {
		port, _ := intConfig(normalized.PluginConfig, "collector_port")
		protocol := stringConfig(normalized.PluginConfig, "transport_protocol", "UDP")
		if err := h.checkListenerPort(r.Context(), "0.0.0.0", protocol, port); err != nil {
			writeErrorCode(w, http.StatusConflict, "LISTENER_PORT_UNAVAILABLE", "端口不可用")
			return
		}
	}
	if err := h.persistDataSource(r, normalized, false); err != nil {
		writeError(w, http.StatusBadGateway, "save datasource failed")
		return
	}
	h.mu.Lock()
	h.dataSources[normalized.ID] = normalized
	h.runtimePipelines = h.buildRuntimePipelinesLocked()
	h.mu.Unlock()
	h.publishRuntimePipelines(r.Context())
	writeJSON(w, http.StatusOK, withRuntimeState(normalized))
}

func (h *Handler) normalizeCollectDataSource(source DataSource, pathID string) (DataSource, *collectAPIError) {
	now := time.Now().UTC()
	pluginCode := strings.ToLower(strings.TrimSpace(source.PluginCode))
	if pluginCode == "" {
		return DataSource{}, validationError("plugin_code is required")
	}
	if pluginCode != "syslog" {
		return h.normalizeImportedInputDataSource(source, pathID, pluginCode, now)
	}
	name := strings.TrimSpace(source.Name)
	if name == "" {
		return DataSource{}, validationError("name is required")
	}
	id := strings.TrimSpace(pathID)
	if id == "" {
		id = strings.TrimSpace(source.ID)
	}
	if id == "" {
		id = collectIDFromName(name)
	}
	id = h.uniqueDataSourceID(id, pathID != "")
	code := firstNonEmpty(strings.TrimSpace(source.Code), id)
	config := normalizeSyslogPluginConfig(source.PluginConfig)
	protocol := stringConfig(config, "transport_protocol", "UDP")
	port, _ := intConfig(config, "collector_port")
	status := normalizeStatus(source.Status)
	return DataSource{
		ID:               id,
		Code:             code,
		Type:             pluginCode,
		Name:             name,
		Status:           status,
		PluginCode:       pluginCode,
		PluginVersion:    firstNonEmpty(strings.TrimSpace(source.PluginVersion), "1.0.0"),
		PluginRuntime:    "go_builtin",
		Source:           strings.TrimSpace(source.Source),
		Sourcetype:       strings.TrimSpace(source.Sourcetype),
		PluginConfig:     config,
		InternalRawTopic: "raw.ds_" + strings.ReplaceAll(id, "-", "_"),
		Addr:             fmt.Sprintf(":%d", port),
		Protocol:         strings.ToLower(protocol),
		PipelineID:       "pipe_" + strings.ReplaceAll(id, "-", "_"),
		UpdatedAt:        now,
	}, nil
}

func (h *Handler) normalizeImportedInputDataSource(source DataSource, pathID string, pluginCode string, now time.Time) (DataSource, *collectAPIError) {
	plugin, ok := h.findPlugin("input", pluginCode, "")
	if !ok {
		return DataSource{}, &collectAPIError{status: http.StatusUnprocessableEntity, code: "PLUGIN_NOT_SUPPORTED", message: "unsupported input plugin"}
	}
	if !isPluginEnabled(plugin.Status) {
		return DataSource{}, &collectAPIError{status: http.StatusUnprocessableEntity, code: "PLUGIN_NOT_ENABLED", message: "input plugin is not enabled"}
	}
	name := strings.TrimSpace(source.Name)
	if name == "" {
		return DataSource{}, validationError("name is required")
	}
	id := strings.TrimSpace(pathID)
	if id == "" {
		id = strings.TrimSpace(source.ID)
	}
	if id == "" {
		id = collectIDFromName(name)
	}
	id = h.uniqueDataSourceID(id, pathID != "")
	code := firstNonEmpty(strings.TrimSpace(source.Code), id)
	status := normalizeStatus(source.Status)
	return DataSource{
		ID:               id,
		Code:             code,
		Type:             pluginCode,
		Name:             name,
		Status:           status,
		PluginCode:       pluginCode,
		PluginVersion:    plugin.PluginVersion,
		PluginRuntime:    firstNonEmpty(strings.TrimSpace(plugin.Runtime), "external"),
		PluginConfig:     normalizeImportedPluginConfig(source.PluginConfig),
		InternalRawTopic: "raw.ds_" + strings.ReplaceAll(id, "-", "_"),
		Protocol:         strings.ToLower(firstNonEmpty(stringConfig(source.PluginConfig, "transport_protocol", ""), stringConfig(source.PluginConfig, "security_protocol", ""))),
		PipelineID:       "pipe_" + strings.ReplaceAll(id, "-", "_"),
		UpdatedAt:        now,
	}, nil
}

func (h *Handler) validateCollectDataSourcePayload(source DataSource) *collectAPIError {
	if strings.TrimSpace(source.InternalRawTopic) != "" {
		return validationError("internal_raw_topic is system generated and cannot be submitted")
	}
	if strings.TrimSpace(source.RawTopic) != "" {
		return validationError("raw_topic is deprecated; source routing is system generated")
	}
	if strings.TrimSpace(source.Source) != "" {
		return validationError("source is derived from datasource name and cannot be submitted")
	}
	if strings.TrimSpace(source.Sourcetype) != "" {
		return validationError("sourcetype is derived from parse rule name and cannot be submitted")
	}
	pluginCode := strings.ToLower(strings.TrimSpace(source.PluginCode))
	if pluginCode == "" {
		return validationError("plugin_code is required")
	}
	status := strings.ToLower(strings.TrimSpace(source.Status))
	if status == "" {
		return validationError("status is required")
	}
	if status != "active" && status != "disabled" {
		return validationError("status must be active or disabled")
	}
	config := source.PluginConfig
	if config == nil {
		return validationError("plugin_config is required")
	}
	if pluginCode != "syslog" {
		return h.validateImportedInputPluginConfig(pluginCode, config)
	}
	port, ok := intConfig(config, "collector_port")
	if !ok || port < 1 || port > 65535 {
		return validationError("plugin_config.collector_port must be between 1 and 65535")
	}
	if _, ok := config["log_filter_enabled"]; !ok {
		return validationError("plugin_config.log_filter_enabled is required")
	}
	protocol := strings.ToUpper(strings.TrimSpace(stringConfig(config, "transport_protocol", "")))
	if protocol == "" {
		return validationError("plugin_config.transport_protocol is required")
	}
	if protocol != "UDP" {
		return validationError("plugin_config.transport_protocol must be UDP in P0")
	}
	encoding := strings.ToUpper(strings.TrimSpace(stringConfig(config, "encoding", "")))
	if encoding == "" {
		return validationError("plugin_config.encoding is required")
	}
	if !map[string]bool{"UTF-8": true, "GBK": true, "GB18030": true, "ISO-8859-1": true}[encoding] {
		return validationError("plugin_config.encoding is not supported")
	}
	if boolConfig(config, "log_filter_enabled", false) {
		pattern := strings.TrimSpace(stringConfig(config, "log_filter_regex", ""))
		if pattern == "" {
			return validationError("plugin_config.log_filter_regex is required when log filter is enabled")
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return validationError("plugin_config.log_filter_regex is invalid")
		}
	}
	return nil
}

func validateCollectDataSourcePayload(source DataSource) *collectAPIError {
	if strings.ToLower(strings.TrimSpace(source.PluginCode)) != "syslog" {
		return &collectAPIError{status: http.StatusUnprocessableEntity, code: "PLUGIN_NOT_SUPPORTED", message: "unsupported input plugin"}
	}
	return (&Handler{}).validateCollectDataSourcePayload(source)
}

func (h *Handler) validateImportedInputPluginConfig(pluginCode string, config map[string]any) *collectAPIError {
	plugin, ok := h.findPlugin("input", pluginCode, "")
	if !ok {
		return &collectAPIError{status: http.StatusUnprocessableEntity, code: "PLUGIN_NOT_SUPPORTED", message: "unsupported input plugin"}
	}
	if !isPluginEnabled(plugin.Status) {
		return &collectAPIError{status: http.StatusUnprocessableEntity, code: "PLUGIN_NOT_ENABLED", message: "input plugin is not enabled"}
	}
	if apiErr := validateConfigBySchema(plugin.ConfigSchema, config); apiErr != nil {
		return apiErr
	}
	if strings.EqualFold(pluginCode, "kafka") {
		if boolConfig(config, "log_filter_enabled", false) {
			pattern := strings.TrimSpace(stringConfig(config, "log_filter_regex", ""))
			if pattern == "" {
				return pluginConfigInvalid("plugin_config.log_filter_regex is required when log filter is enabled")
			}
			if _, err := regexp.Compile(pattern); err != nil {
				return pluginConfigInvalid("plugin_config.log_filter_regex is invalid")
			}
		}
	}
	return nil
}

func validateConfigBySchema(schema map[string]any, config map[string]any) *collectAPIError {
	if len(schema) == 0 {
		return nil
	}
	properties := schemaProperties(schema)
	required := schemaStringList(schema["required"])
	for _, field := range required {
		if _, ok := config[field]; !ok {
			return pluginConfigInvalid("plugin_config." + field + " is required")
		}
	}
	if additional, ok := schema["additionalProperties"].(bool); ok && !additional && len(properties) > 0 {
		for field := range config {
			if _, ok := properties[field]; !ok {
				return pluginConfigInvalid("plugin_config." + field + " is not supported")
			}
		}
	}
	for field, rawRule := range properties {
		rule, _ := rawRule.(map[string]any)
		if rule == nil {
			continue
		}
		value, exists := config[field]
		if !exists {
			if requiredByCondition(rule, config) {
				return pluginConfigInvalid("plugin_config." + field + " is required")
			}
			continue
		}
		if apiErr := validateSchemaValue(field, rule, value); apiErr != nil {
			return apiErr
		}
	}
	return nil
}

func schemaProperties(schema map[string]any) map[string]any {
	if raw, ok := schema["properties"].(map[string]any); ok {
		return raw
	}
	return map[string]any{}
}

func schemaStringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func requiredByCondition(rule map[string]any, config map[string]any) bool {
	raw, ok := rule["x-required-if"].(map[string]any)
	if !ok {
		return false
	}
	field := strings.TrimSpace(fmt.Sprint(raw["field"]))
	if field == "" {
		return false
	}
	return fmt.Sprint(config[field]) == fmt.Sprint(raw["equals"])
}

func validateSchemaValue(field string, rule map[string]any, value any) *collectAPIError {
	switch strings.TrimSpace(fmt.Sprint(rule["type"])) {
	case "string":
		text, ok := value.(string)
		if !ok {
			return pluginConfigInvalid("plugin_config." + field + " must be string")
		}
		if minLength, ok := numberFromAny(rule["minLength"]); ok && len(strings.TrimSpace(text)) < minLength {
			return pluginConfigInvalid("plugin_config." + field + " is required")
		}
		if enum := schemaStringList(rule["enum"]); len(enum) > 0 && !containsString(enum, text) {
			return pluginConfigInvalid("plugin_config." + field + " is invalid")
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return pluginConfigInvalid("plugin_config." + field + " must be array")
		}
		if minItems, ok := numberFromAny(rule["minItems"]); ok && len(items) < minItems {
			return pluginConfigInvalid("plugin_config." + field + " is required")
		}
		for _, item := range items {
			if _, ok := item.(string); !ok {
				return pluginConfigInvalid("plugin_config." + field + " items must be string")
			}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return pluginConfigInvalid("plugin_config." + field + " must be boolean")
		}
	}
	return nil
}

func numberFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	default:
		return 0, false
	}
}

func pluginConfigInvalid(message string) *collectAPIError {
	return &collectAPIError{status: http.StatusBadRequest, code: "PLUGIN_CONFIG_INVALID", message: message}
}

func isPluginEnabled(status string) bool {
	return strings.EqualFold(status, "enabled") || strings.EqualFold(status, "active")
}

func normalizeImportedPluginConfig(config map[string]any) map[string]any {
	out := make(map[string]any, len(config))
	for key, value := range config {
		out[key] = value
	}
	return out
}

func normalizeSyslogPluginConfig(config map[string]any) map[string]any {
	port, _ := intConfig(config, "collector_port")
	logFilterEnabled := boolConfig(config, "log_filter_enabled", false)
	out := map[string]any{
		"collector_port":     port,
		"transport_protocol": strings.ToUpper(stringConfig(config, "transport_protocol", "UDP")),
		"encoding":           strings.ToUpper(stringConfig(config, "encoding", "UTF-8")),
		"log_filter_enabled": logFilterEnabled,
		"log_filter_regex":   "",
	}
	if logFilterEnabled {
		out["log_filter_regex"] = strings.TrimSpace(stringConfig(config, "log_filter_regex", ""))
	}
	return out
}

func validateDataSourcePortCheckRequest(req dataSourcePortCheckRequest) *collectAPIError {
	pluginCode := strings.ToLower(strings.TrimSpace(req.PluginCode))
	if pluginCode != "syslog" {
		return validationError("plugin_code must be syslog in P0")
	}
	if req.CollectorPort < 1 || req.CollectorPort > 65535 {
		return validationError("collector_port must be between 1 and 65535")
	}
	if strings.ToUpper(strings.TrimSpace(req.TransportProtocol)) != "UDP" {
		return validationError("transport_protocol must be UDP in P0")
	}
	return nil
}

func probeListenerPort(host string, protocol string, port int) error {
	address := net.JoinHostPort(host, strconv.Itoa(port))
	if strings.EqualFold(protocol, "UDP") {
		conn, err := net.ListenPacket("udp", address)
		if err != nil {
			return err
		}
		return conn.Close()
	}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	return ln.Close()
}

func probeKafkaBrokers(ctx context.Context, brokers []string) error {
	if os.Getenv("XDP_KAFKA_CONNECTIVITY_SKIP_NETWORK") == "true" {
		return nil
	}
	if len(brokers) == 0 {
		return fmt.Errorf("brokers is required")
	}
	var lastErr error
	for _, broker := range brokers {
		address := strings.TrimSpace(broker)
		if address == "" {
			continue
		}
		dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", address)
		cancel()
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("brokers is required")
	}
	return lastErr
}

func (h *Handler) checkListenerPort(ctx context.Context, host string, protocol string, port int) error {
	if err, ok := checkListenerPortViaAgent(ctx, host, protocol, port); ok {
		return err
	}
	return probeListenerPort(host, protocol, port)
}

func checkListenerPortViaAgent(ctx context.Context, host string, protocol string, port int) (error, bool) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("XDP_AGENT_BASE_URL")), "/")
	if baseURL == "" {
		return nil, false
	}
	payload, _ := json.Marshal(map[string]any{
		"plugin_code":        "syslog",
		"transport_protocol": strings.ToUpper(protocol),
		"collector_port":     port,
		"listen_host":        host,
	})
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL+"/api/v1/port-check", bytes.NewReader(payload))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil, true
	}
	data, _ := io.ReadAll(resp.Body)
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &body); err == nil && body.Error.Code == "LISTENER_PORT_UNAVAILABLE" {
		return fmt.Errorf("listener port unavailable"), true
	}
	return fmt.Errorf("agent port check failed with status %d", resp.StatusCode), true
}

func (h *Handler) persistDataSource(r *http.Request, source DataSource, upsertIndex bool) error {
	if h.mysql == nil {
		return nil
	}
	if err := h.mysql.SaveDataSource(r.Context(), dataSourceToStore(source)); err != nil {
		return err
	}
	if err := h.persistDataSourceRuntimeState(r, source); err != nil {
		return err
	}
	if upsertIndex {
		return h.mysql.UpsertIndexConfig(r.Context(), indexToStore(indexFromDataSource(source)))
	}
	return nil
}

func (h *Handler) persistDataSourceRuntimeState(r *http.Request, source DataSource) error {
	if h.mysql == nil {
		return nil
	}
	state := runtimeStateForDataSource(source)
	return h.mysql.SaveDataSourceRuntimeState(r.Context(), mysqlstore.DataSourceRuntimeState{
		DataSourceID:     source.ID,
		DataSourceCode:   firstNonEmpty(source.Code, source.ID),
		AgentID:          state.AgentID,
		PluginCode:       state.PluginCode,
		DesiredStatus:    state.DesiredStatus,
		RuntimeStatus:    state.RuntimeStatus,
		ListenerStatus:   state.ListenerStatus,
		Protocol:         state.Protocol,
		ListenHost:       "0.0.0.0",
		ListenPort:       state.Port,
		Endpoint:         state.Endpoint,
		PipelineID:       source.PipelineID,
		ConfigVersion:    state.ConfigVersion,
		LastLoadedAt:     &state.LastLoadedAt,
		LastTransitionAt: &state.LastTransitionAt,
		LastHeartbeatAt:  &state.LastHeartbeatAt,
		LastErrorCode:    state.LastErrorCode,
		LastError:        state.LastError,
	})
}

func (h *Handler) findDataSource(id string) (DataSource, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	source, ok := h.dataSources[id]
	return source, ok
}

func (h *Handler) collectDataSourceNameExists(ctx context.Context, name string, selfID string) bool {
	normalizedName := strings.TrimSpace(name)
	if normalizedName == "" {
		return false
	}
	if h.mysql != nil {
		if items, err := h.mysql.ListDataSources(ctx); err == nil {
			for _, item := range items {
				if item.Code == selfID {
					continue
				}
				if strings.TrimSpace(item.Name) == normalizedName {
					return true
				}
			}
			return false
		}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, source := range h.dataSources {
		if source.Status == "deleted" || source.ID == selfID {
			continue
		}
		if strings.TrimSpace(source.Name) == normalizedName {
			return true
		}
	}
	return false
}

func (h *Handler) uniqueDataSourceID(candidate string, update bool) string {
	base := collectIDFromName(candidate)
	if update {
		return base
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	id := base
	for i := 2; ; i++ {
		if _, exists := h.dataSources[id]; !exists {
			return id
		}
		id = base + "-" + strconv.Itoa(i)
	}
}

func filterDataSources(items []DataSource, query url.Values) []DataSource {
	pluginCode := strings.ToLower(strings.TrimSpace(query.Get("plugin_code")))
	status := strings.ToLower(strings.TrimSpace(query.Get("status")))
	keyword := strings.ToLower(strings.TrimSpace(query.Get("keyword")))
	out := make([]DataSource, 0, len(items))
	for _, item := range items {
		if item.Status == "deleted" {
			continue
		}
		if pluginCode != "" && collectPluginCode(item) != pluginCode {
			continue
		}
		if status != "" && strings.ToLower(item.Status) != status {
			continue
		}
		if keyword != "" && !strings.Contains(strings.ToLower(item.Name+" "+item.ID+" "+item.Code), keyword) {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func isCollectDataSourceRequest(source DataSource) bool {
	return strings.TrimSpace(source.PluginCode) != "" || source.PluginConfig != nil || strings.TrimSpace(source.InternalRawTopic) != ""
}

func isCollectDataSource(source DataSource) bool {
	return strings.TrimSpace(source.PluginCode) != "" || source.PluginConfig != nil || strings.TrimSpace(source.InternalRawTopic) != ""
}

func collectPluginCode(source DataSource) string {
	if source.PluginCode != "" {
		return strings.ToLower(strings.TrimSpace(source.PluginCode))
	}
	if source.PluginConfig != nil && source.Type != "" {
		return strings.ToLower(strings.TrimSpace(source.Type))
	}
	return ""
}

func collectIDFromName(value string) string {
	id := strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "datasource"
	}
	return out
}

func normalizeStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return "active"
	}
	return status
}

func validationError(message string) *collectAPIError {
	return &collectAPIError{status: http.StatusBadRequest, code: "VALIDATION_ERROR", message: message}
}

func intConfig(config map[string]any, key string) (int, bool) {
	value, ok := config[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		n, err := typed.Int64()
		return int(n), err == nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		return n, err == nil
	default:
		return 0, false
	}
}

func stringConfig(config map[string]any, key string, fallback string) string {
	value, ok := config[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback
		}
		return strings.TrimSpace(typed)
	default:
		return fmt.Sprint(value)
	}
}

func boolConfig(config map[string]any, key string, fallback bool) bool {
	value, ok := config[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true") || strings.TrimSpace(typed) == "1" || strings.EqualFold(strings.TrimSpace(typed), "on")
	default:
		return fallback
	}
}
