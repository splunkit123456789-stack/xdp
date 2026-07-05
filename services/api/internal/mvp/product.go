package mvp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/pipeline"
	"xdp/pkg/search/splquery"
	ch "xdp/pkg/storage/clickhouse"
	mysqlstore "xdp/pkg/storage/mysql"
	memoryoutput "xdp/plugins/output/memory"

	"golang.org/x/crypto/bcrypt"
)

type AuthConfig struct {
	Enabled  bool   `json:"enabled"`
	Username string `json:"username,omitempty"`
	Password string `json:"-"`
	Token    string `json:"token,omitempty"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type DataSource struct {
	ID               string            `json:"id"`
	Code             string            `json:"code,omitempty"`
	Type             string            `json:"type"`
	Name             string            `json:"name"`
	Status           string            `json:"status"`
	RuntimeStatus    string            `json:"runtime_status,omitempty"`
	ListenerStatus   string            `json:"listener_status,omitempty"`
	ListenerEndpoint string            `json:"listener_endpoint,omitempty"`
	RuntimeResult    map[string]any    `json:"runtime_result,omitempty"`
	PluginCode       string            `json:"plugin_code,omitempty"`
	PluginVersion    string            `json:"plugin_version,omitempty"`
	Source           string            `json:"source,omitempty"`
	Sourcetype       string            `json:"sourcetype,omitempty"`
	PluginConfig     map[string]any    `json:"plugin_config,omitempty"`
	InternalRawTopic string            `json:"internal_raw_topic,omitempty"`
	ValidationResult map[string]any    `json:"validation_result,omitempty"`
	LastLoadedAt     *time.Time        `json:"last_loaded_at,omitempty"`
	LastError        string            `json:"last_error,omitempty"`
	Addr             string            `json:"addr,omitempty"`
	Path             string            `json:"path,omitempty"`
	Protocol         string            `json:"protocol,omitempty"`
	DefaultIndex     string            `json:"default_index,omitempty"`
	TimeField        string            `json:"time_field,omitempty"`
	Parser           string            `json:"parser,omitempty"`
	RegexPattern     string            `json:"regex_pattern,omitempty"`
	FieldMapping     map[string]string `json:"field_mapping,omitempty"`
	TypeMapping      map[string]string `json:"type_mapping,omitempty"`
	RawTopic         string            `json:"raw_topic,omitempty"`
	PipelineID       string            `json:"pipeline_id"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type IndexSummary struct {
	IndexName       string `json:"index_name"`
	Name            string `json:"name,omitempty"`
	TableName       string `json:"table_name,omitempty"`
	Rows            uint64 `json:"rows"`
	LatestEventTime string `json:"latest_event_time,omitempty"`
	StorageBytes    uint64 `json:"storage_bytes"`
	TTLDays         int    `json:"ttl_days"`
	Storage         string `json:"storage"`
	Status          string `json:"status,omitempty"`
	Configured      bool   `json:"configured"`
	System          bool   `json:"system,omitempty"`
	IndexType       string `json:"index_type,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

type IndexConfigRequest struct {
	IndexName   string `json:"index_name"`
	Name        string `json:"name,omitempty"`
	TTLDays     int    `json:"ttl_days,omitempty"`
	Status      string `json:"status,omitempty"`
	DropStorage bool   `json:"drop_storage,omitempty"`
}

type Pagination struct {
	Limit    int  `json:"limit"`
	Offset   int  `json:"offset"`
	Page     int  `json:"page"`
	Returned int  `json:"returned"`
	HasMore  bool `json:"has_more"`
	Total    int  `json:"total"`
}

type ListPagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type FieldSummary struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Count   int      `json:"count"`
	Samples []string `json:"samples,omitempty"`
}

type FieldsResponse struct {
	Fields []FieldSummary `json:"fields"`
}

type TimelineBucket struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Count int    `json:"count"`
}

type TimelineResponse struct {
	Interval  string           `json:"interval"`
	TimeRange *SearchTimeRange `json:"time_range,omitempty"`
	Buckets   []TimelineBucket `json:"buckets"`
}

type DeadletterRecord struct {
	ID           string       `json:"id"`
	EventID      string       `json:"event_id"`
	Status       string       `json:"status"`
	Stage        string       `json:"stage,omitempty"`
	PluginCode   string       `json:"plugin_code,omitempty"`
	ErrorCode    string       `json:"error_code"`
	ErrorMessage string       `json:"error_message"`
	Retryable    bool         `json:"retryable"`
	RawPreview   string       `json:"raw_preview,omitempty"`
	FirstSeenAt  time.Time    `json:"first_seen_at,omitempty"`
	LastSeenAt   time.Time    `json:"last_seen_at,omitempty"`
	Event        *event.Event `json:"event,omitempty"`
}

type RetryDeadletterRequest struct {
	EventID string `json:"event_id"`
}

func authFromEnv() AuthConfig {
	enabled := strings.EqualFold(os.Getenv("XDP_AUTH_ENABLED"), "true")
	token := env("XDP_API_TOKEN", "xdp-dev-token")
	return AuthConfig{
		Enabled:  enabled,
		Username: env("XDP_AUTH_USERNAME", "admin"),
		Password: env("XDP_AUTH_PASSWORD", "xdp"),
		Token:    token,
	}
}

func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if !h.auth.Enabled || isPublicPath(r.URL.Path) {
		return true
	}
	if h.validToken(r.Context(), tokenFromRequest(r)) {
		return true
	}
	writeErrorCode(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
	return false
}

func isPublicPath(path string) bool {
	switch path {
	case "/", "/healthz", "/readyz", "/api/v1/auth", "/api/v1/login":
		return true
	default:
		return false
	}
}

func tokenFromRequest(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-API-Token")); value != "" {
		return value
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func publicPaths() []string {
	return []string{"/", "/healthz", "/readyz", "/api/v1/auth", "/api/v1/login"}
}

func hashAuthSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func hashPassword(value string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func firstTokenPrefix(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}

func (h *Handler) validToken(ctx context.Context, token string) bool {
	if strings.TrimSpace(token) == "" {
		return false
	}
	if h.mysql != nil {
		ok, err := h.mysql.ValidateAuthToken(ctx, hashAuthSecret(token))
		if err == nil {
			return ok
		}
		h.logger.Warn("validate auth token from mysql failed", "error", err)
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(h.auth.Token)) == 1
}

func (h *Handler) validCredentials(ctx context.Context, username string, password string) bool {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return false
	}
	if h.mysql != nil {
		ok, err := h.mysql.ValidateAuthCredentials(ctx, username, password)
		if err == nil {
			return ok
		}
		h.logger.Warn("validate auth credentials from mysql failed", "error", err)
	}
	return subtle.ConstantTimeCompare([]byte(username), []byte(h.auth.Username)) == 1 &&
		subtle.ConstantTimeCompare([]byte(password), []byte(h.auth.Password)) == 1
}

func (h *Handler) authStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":        h.auth.Enabled,
		"login_required": h.auth.Enabled,
		"authenticated":  !h.auth.Enabled || h.validToken(r.Context(), tokenFromRequest(r)),
		"auth_type":      "password_token",
		"token_type":     "Bearer",
		"token_header":   "Authorization",
		"public_paths":   publicPaths(),
	})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid login request")
		return
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "username and password are required")
		return
	}
	if !h.validCredentials(r.Context(), req.Username, req.Password) {
		writeErrorCode(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      h.auth.Token,
		"token_type": "Bearer",
		"expires_in": 0,
		"user": map[string]any{
			"username": strings.TrimSpace(req.Username),
			"role":     "admin",
		},
	})
}

func defaultDataSources() map[string]DataSource {
	now := time.Now().UTC()
	return map[string]DataSource{
		"http-json": {
			ID:               "http-json",
			Type:             "http",
			Name:             "HTTP JSON",
			Status:           "active",
			Addr:             ":8081",
			Path:             "/ingest",
			DefaultIndex:     "app",
			TimeField:        "@timestamp",
			Parser:           "json",
			InternalRawTopic: "xdp.raw.http",
			PipelineID:       pipeline.JSONPipelineID,
			UpdatedAt:        now,
		},
		"syslog-firewall": {
			ID:               "syslog-firewall",
			Type:             "syslog",
			Name:             "Firewall Syslog",
			Status:           "active",
			Addr:             ":5514",
			Protocol:         "udp",
			DefaultIndex:     "firewall",
			Parser:           "regex",
			RegexPattern:     `src=(?P<src_ip>\S+) dst=(?P<dst_ip>\S+) action=(?P<action>\S+) bytes=(?P<bytes>\d+)`,
			InternalRawTopic: "xdp.raw.syslog",
			PipelineID:       pipeline.FirewallPipelineID,
			UpdatedAt:        now,
		},
	}
}

func defaultIndexConfigs() map[string]IndexSummary {
	items := map[string]IndexSummary{}
	for _, source := range defaultDataSources() {
		index := source.DefaultIndex
		if index == "" {
			index = "app"
		}
		items[indexKey(index)] = IndexSummary{
			IndexName:  index,
			Name:       index,
			TTLDays:    30,
			Storage:    "configured",
			Status:     "active",
			Configured: true,
		}
	}
	items[indexKey(ch.SystemUnparsedIndexName)] = IndexSummary{
		IndexName:  ch.SystemUnparsedIndexName,
		Name:       ch.SystemUnparsedIndexName,
		TableName:  "events_" + ch.SystemUnparsedIndexName,
		TTLDays:    30,
		Storage:    "configured",
		Status:     "active",
		Configured: true,
		System:     true,
		IndexType:  "system",
	}
	return items
}

func defaultSavedSearches() map[string]mysqlstore.SavedSearch {
	now := time.Now()
	items := make(map[string]mysqlstore.SavedSearch)
	for _, item := range defaultSavedSearchList() {
		item.CreatedAt = now
		item.UpdatedAt = now
		items[item.ID] = item
	}
	return items
}

func defaultSavedSearchList() []mysqlstore.SavedSearch {
	return []mysqlstore.SavedSearch{
		{
			ID:            "s-1",
			Name:          "App stats",
			SPL:           "index=app | stats count as total by service",
			TimeRangeType: "近 1 天",
			Visibility:    "private",
			Status:        "active",
		},
		{
			ID:            "s-2",
			Name:          "Firewall deny",
			SPL:           "index=firewall action=deny",
			TimeRangeType: "近 7 天",
			Visibility:    "private",
			Status:        "active",
		},
	}
}

func (h *Handler) seedConfigStore(ctx context.Context) {
	if h.mysql != nil {
		if err := h.mysql.SeedParserPlugins(ctx, parserPluginStoreItems()); err != nil {
			h.logger.Warn("seed parser plugins failed", "error", err)
		}
		if err := h.mysql.SeedSavedSearches(ctx, defaultSavedSearchList()); err != nil {
			h.logger.Warn("seed saved searches failed", "error", err)
		}
	}
	for _, source := range h.dataSources {
		if h.mysql != nil {
			if err := h.mysql.SeedDataSource(ctx, dataSourceToStore(source)); err != nil {
				h.logger.Warn("seed datasource failed", "datasource", source.ID, "error", err)
			}
			if err := h.mysql.UpsertIndexConfig(ctx, indexToStore(indexFromDataSource(source))); err != nil {
				h.logger.Warn("seed index failed", "index", source.DefaultIndex, "error", err)
			}
		}
	}
	h.publishRuntimePipelines(ctx)
}

func (h *Handler) loadConfigStore(ctx context.Context) {
	if h.mysql == nil {
		return
	}
	items, err := h.mysql.ListDataSources(ctx)
	if err != nil {
		h.logger.Warn("load datasources failed", "error", err)
		return
	}
	if len(items) == 0 {
		return
	}
	sources := map[string]DataSource{}
	for _, item := range items {
		source, err := dataSourceFromStore(item)
		if err != nil {
			h.logger.Warn("decode datasource failed", "datasource", item.Code, "error", err)
			continue
		}
		sources[source.ID] = source
	}
	if len(sources) == 0 {
		return
	}
	indexes := map[string]IndexSummary{}
	for key, index := range defaultIndexConfigs() {
		if ch.IsSystemIndexName(index.IndexName) {
			indexes[key] = index
		}
	}
	if items, err := h.mysql.ListIndexConfigs(ctx); err == nil {
		for _, item := range items {
			index := indexFromStore(item)
			indexes[indexKey(index.IndexName)] = index
		}
	} else {
		h.logger.Warn("load indexes failed", "error", err)
	}
	parseRules := map[string]ParseRule{}
	if items, err := h.mysql.ListParseRules(ctx); err == nil {
		for _, item := range items {
			rule, err := parseRuleFromStore(item)
			if err != nil {
				h.logger.Warn("decode parse rule failed", "rule", item.Code, "error", err)
				continue
			}
			parseRules[rule.ID] = rule
		}
	} else {
		h.logger.Warn("load parse rules failed", "error", err)
	}
	h.mu.Lock()
	h.dataSources = sources
	if len(indexes) > 0 {
		h.indexConfigs = indexes
	}
	if len(parseRules) > 0 {
		h.parseRules = parseRules
	}
	h.runtimePipelines = h.buildRuntimePipelinesLocked()
	h.mu.Unlock()
	h.publishRuntimePipelines(ctx)
}

func indexFromDataSource(source DataSource) IndexSummary {
	index := source.DefaultIndex
	if index == "" {
		index = "app"
	}
	return IndexSummary{
		IndexName:  index,
		Name:       index,
		TTLDays:    30,
		Storage:    "configured",
		Status:     "active",
		Configured: true,
	}
}

func indexToStore(index IndexSummary) mysqlstore.IndexConfig {
	return mysqlstore.IndexConfig{
		Code:             index.IndexName,
		Name:             firstNonEmpty(index.Name, index.IndexName),
		Status:           firstNonEmpty(index.Status, "active"),
		HotRetentionDays: index.TTLDays,
	}
}

func indexFromStore(item mysqlstore.IndexConfig) IndexSummary {
	ttl := item.HotRetentionDays
	if ttl <= 0 {
		ttl = 30
	}
	updatedAt := ""
	if !item.UpdatedAt.IsZero() {
		updatedAt = item.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return IndexSummary{
		IndexName:  item.Code,
		Name:       item.Name,
		TTLDays:    ttl,
		Storage:    "configured",
		Status:     item.Status,
		Configured: true,
		UpdatedAt:  updatedAt,
	}
}

func indexKey(index string) string {
	return index
}

func dataSourceToStore(source DataSource) mysqlstore.DataSource {
	source = migrateLegacyRawTopic(source)
	config, _ := json.Marshal(source)
	return mysqlstore.DataSource{
		Code:          source.ID,
		Type:          source.Type,
		Name:          source.Name,
		Status:        source.Status,
		Config:        config,
		ConfigVersion: 1,
	}
}

func dataSourceFromStore(item mysqlstore.DataSource) (DataSource, error) {
	var source DataSource
	if len(item.Config) > 0 {
		if err := json.Unmarshal(item.Config, &source); err != nil {
			return DataSource{}, err
		}
	}
	source.ID = item.Code
	source.Type = item.Type
	source.Name = item.Name
	source.Status = item.Status
	source.UpdatedAt = item.UpdatedAt
	if source.DefaultIndex == "" && !isCollectDataSource(source) {
		source.DefaultIndex = "app"
	}
	return migrateLegacyRawTopic(source), nil
}

func withRuntimeState(source DataSource) DataSource {
	source = migrateLegacyRawTopic(source)
	state := runtimeStateForDataSource(source)
	source.RuntimeStatus = state.RuntimeStatus
	source.ListenerStatus = state.ListenerStatus
	source.ListenerEndpoint = state.Endpoint
	source.RuntimeResult = map[string]any{
		"desired_status":     state.DesiredStatus,
		"runtime_status":     state.RuntimeStatus,
		"listener_status":    state.ListenerStatus,
		"endpoint":           state.Endpoint,
		"agent_id":           state.AgentID,
		"last_transition_at": state.LastTransitionAt.Format(time.RFC3339),
		"last_error":         state.LastError,
	}
	return source
}

type dataSourceRuntimeState struct {
	ID               string
	Name             string
	PluginCode       string
	DesiredStatus    string
	RuntimeStatus    string
	ListenerStatus   string
	Endpoint         string
	Protocol         string
	Port             int
	AgentID          string
	PipelineID       string
	ConfigVersion    int64
	LastLoadedAt     time.Time
	LastTransitionAt time.Time
	LastHeartbeatAt  time.Time
	LastReceivedAt   time.Time
	ReceivedEvents   int64
	ReceivedBytes    int64
	LastErrorCode    string
	LastError        string
}

func runtimeStateForDataSource(source DataSource) dataSourceRuntimeState {
	now := time.Now().UTC()
	pluginCode := collectPluginCode(source)
	if pluginCode == "" {
		pluginCode = source.Type
	}
	status := normalizeStatus(source.Status)
	protocol := strings.ToLower(firstNonEmpty(stringConfig(source.PluginConfig, "transport_protocol", ""), source.Protocol, "udp"))
	port, _ := intConfig(source.PluginConfig, "collector_port")
	if port == 0 {
		port = parseAddrPort(source.Addr)
	}
	endpoint := ""
	if pluginCode == "syslog" && port > 0 {
		endpoint = fmt.Sprintf("%s://0.0.0.0:%d", protocol, port)
	}
	runtimeStatus := "unknown"
	listenerStatus := "unknown"
	if pluginCode == "syslog" {
		if status == "active" {
			runtimeStatus = "running"
			listenerStatus = "listening"
		} else {
			runtimeStatus = "stopped"
			listenerStatus = "stopped"
		}
	}
	return dataSourceRuntimeState{
		ID:               source.ID,
		Name:             source.Name,
		PluginCode:       pluginCode,
		DesiredStatus:    status,
		RuntimeStatus:    runtimeStatus,
		ListenerStatus:   listenerStatus,
		Endpoint:         endpoint,
		Protocol:         protocol,
		Port:             port,
		AgentID:          "local-agent",
		PipelineID:       source.PipelineID,
		ConfigVersion:    1,
		LastLoadedAt:     now,
		LastTransitionAt: now,
		LastHeartbeatAt:  now,
	}
}

func parseAddrPort(addr string) int {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return 0
	}
	if strings.HasPrefix(addr, ":") {
		n, _ := strconv.Atoi(strings.TrimPrefix(addr, ":"))
		return n
	}
	idx := strings.LastIndex(addr, ":")
	if idx >= 0 {
		n, _ := strconv.Atoi(addr[idx+1:])
		return n
	}
	return 0
}

func migrateLegacyRawTopic(source DataSource) DataSource {
	source.InternalRawTopic = strings.TrimSpace(source.InternalRawTopic)
	if source.InternalRawTopic == "" && strings.TrimSpace(source.RawTopic) != "" {
		source.InternalRawTopic = strings.TrimSpace(source.RawTopic)
	}
	if source.InternalRawTopic == "" && strings.TrimSpace(source.Type) != "" {
		source.InternalRawTopic = defaultInternalRawTopic(source)
	}
	source.RawTopic = ""
	if isCollectDataSource(source) {
		source.Source = ""
		source.Sourcetype = ""
	}
	return source
}

func defaultInternalRawTopic(source DataSource) string {
	rawSource := strings.TrimSpace(source.Type)
	if rawSource == "" {
		rawSource = "http"
	}
	return "xdp.raw." + rawSource
}

func (h *Handler) dataSource(id string) DataSource {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if source, ok := h.dataSources[id]; ok {
		if source.DefaultIndex == "" {
			source.DefaultIndex = "app"
		}
		return migrateLegacyRawTopic(source)
	}
	return migrateLegacyRawTopic(defaultDataSources()["http-json"])
}

func (h *Handler) listDataSources(w http.ResponseWriter, r *http.Request) {
	if h.mysql != nil {
		if items, err := h.mysql.ListDataSources(r.Context()); err == nil && len(items) > 0 {
			sources := make([]DataSource, 0, len(items))
			for _, item := range items {
				source, err := dataSourceFromStore(item)
				if err == nil {
					sources = append(sources, withRuntimeState(source))
				}
			}
			sources = filterDataSources(sources, r.URL.Query())
			sortDataSources(sources)
			pageItems, pagination := paginateList(sources, r)
			writeJSON(w, http.StatusOK, map[string]any{"datasources": pageItems, "pagination": pagination})
			return
		}
	}
	h.mu.RLock()
	items := make([]DataSource, 0, len(h.dataSources))
	for _, source := range h.dataSources {
		items = append(items, withRuntimeState(source))
	}
	h.mu.RUnlock()
	items = filterDataSources(items, r.URL.Query())
	sortDataSources(items)
	pageItems, pagination := paginateList(items, r)
	writeJSON(w, http.StatusOK, map[string]any{"datasources": pageItems, "pagination": pagination})
}

func sortDataSources(items []DataSource) {
	sort.Slice(items, func(i, j int) bool {
		left := strings.ToLower(firstNonEmpty(items[i].Name, items[i].Code, items[i].ID))
		right := strings.ToLower(firstNonEmpty(items[j].Name, items[j].Code, items[j].ID))
		if left == right {
			return items[i].ID < items[j].ID
		}
		return left < right
	})
}

func (h *Handler) saveDataSource(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var source DataSource
	if err := json.NewDecoder(r.Body).Decode(&source); err != nil {
		writeError(w, http.StatusBadRequest, "invalid datasource json")
		return
	}
	if isCollectDataSourceRequest(source) {
		h.saveCollectDataSource(w, r, source, "")
		return
	}
	if strings.TrimSpace(source.RawTopic) != "" {
		writeError(w, http.StatusBadRequest, "raw_topic is deprecated")
		return
	}
	if source.ID == "" {
		source.ID = source.Type + "-" + source.DefaultIndex
	}
	normalizedIndex, err := ch.NormalizeIndexName(source.DefaultIndex)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid default_index")
		return
	}
	source.DefaultIndex = normalizedIndex
	if source.Status == "" {
		source.Status = "active"
	}
	if source.Name == "" {
		source.Name = source.ID
	}
	if source.Parser == "" {
		source.Parser = "json"
	}
	source = migrateLegacyRawTopic(source)
	source.UpdatedAt = time.Now().UTC()
	if h.mysql != nil {
		if err := h.mysql.SaveDataSource(r.Context(), dataSourceToStore(source)); err != nil {
			writeError(w, http.StatusBadGateway, "save datasource failed")
			return
		}
		if err := h.mysql.UpsertIndexConfig(r.Context(), indexToStore(indexFromDataSource(source))); err != nil {
			writeError(w, http.StatusBadGateway, "save datasource index failed")
			return
		}
	}
	h.mu.Lock()
	h.dataSources[source.ID] = source
	indexConfig := indexFromDataSource(source)
	h.indexConfigs[indexKey(indexConfig.IndexName)] = indexConfig
	h.runtimePipelines = h.buildRuntimePipelinesLocked()
	h.mu.Unlock()
	if h.mysql != nil {
		h.publishRuntimePipelines(r.Context())
	}
	writeJSON(w, http.StatusOK, source)
}

func (h *Handler) pipelineForSource(id string) pipeline.Pipeline {
	source := h.dataSource(id)
	pipe := pipelineFromDataSource(source)
	if os.Getenv("XDP_OUTPUT") == "clickhouse" {
		pipe.Spec.Outputs = []pipeline.OutputSpec{{ID: "write-clickhouse", Plugin: "clickhouse-output", Version: "1.0.0", Config: map[string]any{"endpoint": env("XDP_CLICKHOUSE_ENDPOINT", "http://127.0.0.1:8123"), "database": env("XDP_CLICKHOUSE_DATABASE", "xdp"), "username": env("XDP_CLICKHOUSE_USERNAME", ""), "password": env("XDP_CLICKHOUSE_PASSWORD", ""), "index": source.DefaultIndex}}}
	}
	applyPipelineOutputIndex(&pipe, h.parseRuleOutputIndex(source))
	return pipe
}

func (h *Handler) buildRuntimePipelines() []pipeline.Pipeline {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.buildRuntimePipelinesLocked()
}

func (h *Handler) buildRuntimePipelinesLocked() []pipeline.Pipeline {
	items := make([]pipeline.Pipeline, 0, len(h.dataSources))
	for _, source := range h.dataSources {
		if source.Status != "" && source.Status != "active" {
			continue
		}
		pipe := pipelineFromDataSource(source)
		pipe.Spec.Stages = h.appendParseRuleStagesLocked(pipe.Spec.Stages, source)
		applyPipelineOutputIndex(&pipe, h.parseRuleOutputIndexLocked(source))
		items = append(items, pipe)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Metadata.ID < items[j].Metadata.ID })
	return items
}

func pipelineFromDataSource(source DataSource) pipeline.Pipeline {
	if source.DefaultIndex == "" {
		source.DefaultIndex = "app"
	}
	if source.PipelineID == "" {
		source.PipelineID = source.ID + "-pipeline"
	}
	switch source.Type {
	case "syslog":
		pipe := pipeline.FirewallSyslogPipeline()
		pipe.Metadata.ID = source.PipelineID
		pipe.Metadata.Name = source.Name + " Pipeline"
		pipe.Spec.Source.ID = source.ID
		pipe.Spec.Source.Config = sourceConfig(source)
		stages := make([]pipeline.StageSpec, 0, len(pipe.Spec.Stages)+1)
		for i, stage := range pipe.Spec.Stages {
			switch stage.ID {
			case "parse-firewall":
				if source.RegexPattern != "" {
					stage.Config["pattern"] = source.RegexPattern
				}
			case "convert-types":
				if len(source.TypeMapping) > 0 {
					stage.Config = map[string]any{"fields": stringMapAny(source.TypeMapping)}
				}
			case "route-firewall":
				stage.Config = map[string]any{"rules": []any{map[string]any{"when": "fields.action == 'deny'", "set": map[string]any{"metadata.index": source.DefaultIndex}, "add_tags": []any{"blocked"}}}}
			}
			pipe.Spec.Stages[i] = stage
			stages = append(stages, stage)
			if stage.ID == "parse-firewall" && len(source.FieldMapping) > 0 {
				stages = append(stages, fieldMappingStage(source.FieldMapping))
			}
		}
		pipe.Spec.Stages = stages
		pipe.Spec.Outputs = []pipeline.OutputSpec{{ID: "set-index", Plugin: "memory-output", Version: "1.0.0", Config: map[string]any{"index": source.DefaultIndex}}}
		return pipe
	default:
		pipe := pipeline.MVPJSONPipeline()
		pipe.Metadata.ID = source.PipelineID
		pipe.Metadata.Name = source.Name + " Pipeline"
		pipe.Spec.Source.ID = source.ID
		pipe.Spec.Source.Config = sourceConfig(source)
		if source.Parser == "regex" {
			pattern := source.RegexPattern
			if pattern == "" {
				pattern = `(?P<message>.*)`
			}
			pipe.Spec.Stages = []pipeline.StageSpec{{ID: "parse-regex", Type: "parser", Plugin: "regex-parser", Version: "1.0.0", Config: map[string]any{"pattern": pattern}, OnError: &pipeline.ErrorPolicy{Action: "dead_letter"}}}
		}
		pipe.Spec.Stages = appendParsingConfigStages(pipe.Spec.Stages, source)
		pipe.Spec.Outputs = []pipeline.OutputSpec{{ID: "set-index", Plugin: "memory-output", Version: "1.0.0", Config: map[string]any{"index": source.DefaultIndex}}}
		return pipe
	}
}

func appendParsingConfigStages(stages []pipeline.StageSpec, source DataSource) []pipeline.StageSpec {
	out := append([]pipeline.StageSpec(nil), stages...)
	if len(source.FieldMapping) > 0 {
		out = append(out, fieldMappingStage(source.FieldMapping))
	}
	if len(source.TypeMapping) > 0 {
		out = append(out, pipeline.StageSpec{ID: "normalize-types", Type: "transform", Plugin: "type-convert", Version: "1.0.0", Config: map[string]any{"fields": stringMapAny(source.TypeMapping)}})
	}
	return out
}

func fieldMappingStage(mapping map[string]string) pipeline.StageSpec {
	return pipeline.StageSpec{ID: "map-fields", Type: "transform", Plugin: "field-mapping", Version: "1.0.0", Config: map[string]any{"mapping": stringMapAny(mapping)}}
}

func stringMapAny(values map[string]string) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		out[key] = value
	}
	return out
}

func sourceConfig(source DataSource) map[string]any {
	source = migrateLegacyRawTopic(source)
	config := map[string]any{}
	if strings.TrimSpace(source.Name) != "" {
		config["source_name"] = strings.TrimSpace(source.Name)
	}
	if source.InternalRawTopic != "" {
		config["internal_raw_topic"] = source.InternalRawTopic
	}
	if source.InternalRawTopic == "" {
		switch source.Type {
		case "syslog":
			config["raw_source"] = "syslog"
		default:
			config["raw_source"] = "http"
		}
	}
	if source.Path != "" {
		config["path"] = source.Path
	}
	if source.Protocol != "" {
		config["protocol"] = source.Protocol
	}
	return config
}

func (h *Handler) publishRuntimePipelines(ctx context.Context) {
	if h.mysql == nil {
		return
	}
	h.mu.RLock()
	pipes := append([]pipeline.Pipeline(nil), h.runtimePipelines...)
	h.mu.RUnlock()
	for _, pipe := range pipes {
		if err := h.mysql.SavePipeline(ctx, pipe); err != nil {
			h.logger.Warn("save runtime pipeline failed", "pipeline_id", pipe.Metadata.ID, "error", err)
		}
	}
}

func (h *Handler) listRuntimePipelines(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	items := append([]pipeline.Pipeline(nil), h.runtimePipelines...)
	h.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"pipelines": items})
}

func (h *Handler) listIndexes(w http.ResponseWriter, r *http.Request) {
	byIndex := h.indexConfigSummaries(r.Context())
	if os.Getenv("XDP_OUTPUT") == "clickhouse" {
		items, err := h.clickhouse.ListIndexes(r.Context())
		if err == nil {
			for _, item := range items {
				current := byIndex[item.IndexName]
				ttl := current.TTLDays
				if ttl <= 0 {
					ttl = item.TTLDays
				}
				if ttl <= 0 {
					ttl = 30
				}
				current.IndexName = item.IndexName
				if current.Name == "" {
					current.Name = item.IndexName
				}
				current.TableName = item.TableName
				current.Rows = item.Rows
				current.LatestEventTime = item.LatestEventTime
				current.StorageBytes = item.StorageBytes
				current.TTLDays = ttl
				current.Storage = "clickhouse"
				if current.Status == "" {
					current.Status = "active"
				}
				byIndex[item.IndexName] = current
			}
			indexes := sortedIndexSummaries(byIndex)
			pageItems, pagination := paginateList(indexes, r)
			writeJSON(w, http.StatusOK, map[string]any{"indexes": pageItems, "pagination": pagination})
			return
		}
		h.logger.Warn("list clickhouse indexes failed, falling back to memory", "error", err)
	}
	memoryItems := memoryoutput.DefaultStore().Indexes()
	for _, item := range memoryItems {
		latest := ""
		if !item.LatestEventTime.IsZero() {
			latest = formatSearchTime(item.LatestEventTime)
		}
		current := byIndex[item.IndexName]
		ttl := current.TTLDays
		if ttl <= 0 {
			ttl = item.TTLDays
		}
		if ttl <= 0 {
			ttl = 30
		}
		current.IndexName = item.IndexName
		if current.Name == "" {
			current.Name = item.IndexName
		}
		current.Rows = uint64(item.Rows)
		current.LatestEventTime = latest
		current.StorageBytes = uint64(item.StorageBytes)
		current.TTLDays = ttl
		current.Storage = "memory"
		if current.Status == "" {
			current.Status = "active"
		}
		byIndex[item.IndexName] = current
	}
	indexes := sortedIndexSummaries(byIndex)
	pageItems, pagination := paginateList(indexes, r)
	writeJSON(w, http.StatusOK, map[string]any{"indexes": pageItems, "pagination": pagination})
}

func (h *Handler) saveIndex(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req IndexConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid index json")
		return
	}
	if strings.TrimSpace(req.IndexName) == "" {
		writeErrorCode(w, http.StatusBadRequest, "VALIDATION_ERROR", "index_name is required")
		return
	}
	indexName, err := ch.NormalizeIndexName(req.IndexName)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid index_name")
		return
	}
	if ch.IsSystemIndexName(indexName) {
		writeErrorCode(w, http.StatusBadRequest, "VALIDATION_ERROR", "system index is reserved")
		return
	}
	ttl := req.TTLDays
	if ttl <= 0 {
		writeErrorCode(w, http.StatusBadRequest, "VALIDATION_ERROR", "ttl_days is required")
		return
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		writeErrorCode(w, http.StatusBadRequest, "VALIDATION_ERROR", "status is required")
		return
	}
	if status != "active" && status != "disabled" {
		writeErrorCode(w, http.StatusBadRequest, "VALIDATION_ERROR", "status must be active or disabled")
		return
	}
	index := IndexSummary{
		IndexName:  indexName,
		Name:       firstNonEmpty(req.Name, indexName),
		TTLDays:    ttl,
		Storage:    "configured",
		Status:     status,
		Configured: true,
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if h.mysql != nil {
		if err := h.mysql.UpsertIndexConfig(r.Context(), indexToStore(index)); err != nil {
			writeError(w, http.StatusBadGateway, "save index failed")
			return
		}
	}
	if os.Getenv("XDP_OUTPUT") == "clickhouse" {
		if err := h.clickhouse.EnsureIndexTable(r.Context(), indexName); err != nil {
			writeError(w, http.StatusBadGateway, "create clickhouse index table failed")
			return
		}
		index.Storage = "clickhouse"
		index.TableName = "events_" + indexName
	}
	h.mu.Lock()
	h.indexConfigs[indexKey(indexName)] = index
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, index)
}

func (h *Handler) deleteIndex(w http.ResponseWriter, r *http.Request) {
	rawIndex := strings.TrimSpace(r.URL.Query().Get("index"))
	if rawIndex == "" {
		writeError(w, http.StatusBadRequest, "index is required")
		return
	}
	indexName, err := ch.NormalizeIndexName(rawIndex)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid index")
		return
	}
	if ch.IsSystemIndexName(indexName) {
		writeErrorCode(w, http.StatusForbidden, "SYSTEM_INDEX_PROTECTED", "system index cannot be deleted")
		return
	}
	if h.mysql != nil {
		if err := h.mysql.DeleteIndexConfig(r.Context(), indexName); err != nil {
			writeError(w, http.StatusBadGateway, "delete index config failed")
			return
		}
	}
	if strings.EqualFold(r.URL.Query().Get("drop_storage"), "true") && os.Getenv("XDP_OUTPUT") == "clickhouse" {
		if err := h.clickhouse.DropIndexTable(r.Context(), indexName); err != nil {
			writeError(w, http.StatusBadGateway, "drop clickhouse index table failed")
			return
		}
	}
	h.mu.Lock()
	delete(h.indexConfigs, indexKey(indexName))
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "index_name": indexName})
}

func (h *Handler) indexConfigSummaries(ctx context.Context) map[string]IndexSummary {
	byIndex := map[string]IndexSummary{}
	if h.mysql != nil {
		if items, err := h.mysql.ListIndexConfigs(ctx); err == nil {
			for _, item := range items {
				index := indexFromStore(item)
				byIndex[index.IndexName] = index
			}
			return byIndex
		} else {
			h.logger.Warn("list configured indexes failed", "error", err)
		}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, index := range h.indexConfigs {
		byIndex[index.IndexName] = index
	}
	return byIndex
}

func sortedIndexSummaries(byIndex map[string]IndexSummary) []IndexSummary {
	out := make([]IndexSummary, 0, len(byIndex))
	for _, item := range byIndex {
		if item.TTLDays <= 0 {
			item.TTLDays = 30
		}
		if item.Status == "" {
			item.Status = "active"
		}
		if ch.IsSystemIndexName(item.IndexName) {
			item.System = true
			item.IndexType = "system"
		} else if item.IndexType == "" {
			item.IndexType = "business"
		}
		if item.TableName == "" {
			item.TableName = "events_" + item.IndexName
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IndexName < out[j].IndexName })
	return out
}

func (h *Handler) searchQueryFromRequest(w http.ResponseWriter, r *http.Request, defaultLimit int) (SearchQuery, bool) {
	limit := ch.ParseLimit(r.URL.Query().Get("limit"), defaultLimit)
	page := ch.ParseLimit(r.URL.Query().Get("page"), 1)
	offset := parseNonNegative(r.URL.Query().Get("offset"), (page-1)*limit)
	startTime, endTime, earliest, latest, err := searchTimeBoundsFromRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return SearchQuery{}, false
	}
	query := SearchQuery{Index: r.URL.Query().Get("index"), Keyword: r.URL.Query().Get("keyword"), Field: r.URL.Query().Get("field"), Value: r.URL.Query().Get("value"), StartTime: startTime, EndTime: endTime, Limit: limit, Offset: offset, Q: r.URL.Query().Get("q"), Earliest: earliest, Latest: latest}
	if strings.TrimSpace(query.Q) != "" {
		parsed, err := splquery.Parse(query.Q)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid search query")
			return SearchQuery{}, false
		}
		query.ApplyFilters(parsed.Filters)
	}
	if err := query.NormalizeIndex(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid index")
		return SearchQuery{}, false
	}
	return query, true
}

func (h *Handler) findEvents(ctx context.Context, query SearchQuery) ([]*event.Event, Pagination, error) {
	limit := normalizedPageLimit(query.Limit)
	query.Limit = limit
	pagination := paginationFromQuery(query, limit)
	var events []*event.Event
	var err error
	if os.Getenv("XDP_OUTPUT") == "clickhouse" {
		events, err = h.clickhouse.Search(ctx, ch.SearchQuery{Index: query.Index, Keyword: query.Keyword, Field: query.Field, Value: query.Value, StartTime: query.StartTime, EndTime: query.EndTime, Limit: query.Limit, Offset: query.Offset, HotFields: h.hotFieldsForIndex(query.Index)})
		if err != nil {
			h.logger.Warn("clickhouse search failed, falling back to memory", "error", err)
		} else {
			total, countErr := h.clickhouse.Count(ctx, ch.SearchQuery{Index: query.Index, Keyword: query.Keyword, Field: query.Field, Value: query.Value, StartTime: query.StartTime, EndTime: query.EndTime, HotFields: h.hotFieldsForIndex(query.Index)})
			if countErr != nil {
				h.logger.Warn("clickhouse count failed, falling back to returned count", "error", countErr)
			} else {
				pagination.Total = total
			}
		}
	}
	if events == nil {
		memoryQuery := memoryoutput.SearchQuery{Index: query.Index, Keyword: query.Keyword, Field: query.Field, Value: query.Value, StartTime: query.StartTime, EndTime: query.EndTime, Limit: query.Limit, Offset: query.Offset}
		events = memoryoutput.DefaultStore().Search(memoryQuery)
		pagination.Total = memoryoutput.DefaultStore().Count(memoryQuery)
	}
	pagination.Returned = len(events)
	if pagination.Total < pagination.Returned+pagination.Offset {
		pagination.Total = pagination.Returned + pagination.Offset
	}
	pagination.HasMore = pagination.Offset+pagination.Returned < pagination.Total
	return events, pagination, nil
}

func normalizedPageLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func paginateList[T any](items []T, r *http.Request) ([]T, ListPagination) {
	total := len(items)
	pageSize := parseListPositiveInt(r.URL.Query().Get("page_size"), 10)
	if pageSize > 1000 {
		pageSize = 1000
	}
	page := parseListPositiveInt(r.URL.Query().Get("page"), 1)
	totalPages := 1
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	pagination := ListPagination{Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages}
	offset := (page - 1) * pageSize
	if offset >= total {
		return []T{}, pagination
	}
	end := offset + pageSize
	if end > total {
		end = total
	}
	return items[offset:end], pagination
}

func parseListPositiveInt(value string, defaultValue int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}

func paginationFromQuery(query SearchQuery, limit int) Pagination {
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	page := offset/limit + 1
	return Pagination{Limit: limit, Offset: offset, Page: page}
}

func paginateStatsRows(rows []map[string]any, query SearchQuery) ([]map[string]any, Pagination) {
	limit := normalizedPageLimit(query.Limit)
	pagination := paginationFromQuery(query, limit)
	pagination.Total = len(rows)
	if pagination.Offset >= len(rows) {
		rows = []map[string]any{}
	} else {
		end := pagination.Offset + limit
		if end > len(rows) {
			end = len(rows)
		}
		rows = rows[pagination.Offset:end]
	}
	if pagination.Offset+len(rows) < pagination.Total {
		pagination.HasMore = true
	}
	pagination.Returned = len(rows)
	return rows, pagination
}

func (h *Handler) searchFields(w http.ResponseWriter, r *http.Request) {
	query, ok := h.searchQueryFromRequest(w, r, 500)
	if !ok {
		return
	}
	query.Offset = 0
	events, _, err := h.findEvents(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusBadGateway, "field discovery failed")
		return
	}
	fields := map[string]*FieldSummary{}
	for _, e := range events {
		for key, value := range e.Fields {
			addFieldSummary(fields, key, value)
		}
		for key, value := range eventMetadataFields(e) {
			addFieldSummary(fields, key, value)
		}
	}
	items := make([]FieldSummary, 0, len(fields))
	for _, item := range fields {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Name < items[j].Name
		}
		return items[i].Count > items[j].Count
	})
	writeJSON(w, http.StatusOK, FieldsResponse{Fields: items})
}

func addFieldSummary(fields map[string]*FieldSummary, key string, value any) {
	if strings.TrimSpace(key) == "" {
		return
	}
	item := fields[key]
	if item == nil {
		item = &FieldSummary{Name: key, Type: valueType(value)}
		fields[key] = item
	}
	item.Count++
	sample := stringifySample(value)
	if sample != "" && len(item.Samples) < 3 && !containsString(item.Samples, sample) {
		item.Samples = append(item.Samples, sample)
	}
}

func eventMetadataFields(e *event.Event) map[string]any {
	fields := map[string]any{
		"source": e.Source.Name,
	}
	for _, key := range []string{"sourcetype", "parse_status", "parse_rule_id", "parse_rule_name", "parse_error", "parsed_at"} {
		if value, ok := e.Metadata[key]; ok {
			fields[key] = value
		}
	}
	return fields
}

func (h *Handler) searchTimeline(w http.ResponseWriter, r *http.Request) {
	query, ok := h.searchQueryFromRequest(w, r, 1000)
	if !ok {
		return
	}
	query.Offset = 0
	requestedInterval := firstNonEmpty(r.URL.Query().Get("interval"), "auto")
	rawStart, rawEnd, hasRange := timelineRawRange(query, nil)
	interval := normalizeTimelineInterval(requestedInterval, rawStart, rawEnd, hasRange)
	counts, err := h.timelineCounts(r.Context(), query, interval)
	if err != nil {
		writeError(w, http.StatusBadGateway, "timeline query failed")
		return
	}
	if !hasRange {
		rawStart, rawEnd, hasRange = timelineRangeFromCounts(counts)
		interval = normalizeTimelineInterval(requestedInterval, rawStart, rawEnd, hasRange)
		if normalized := strings.ToLower(strings.TrimSpace(requestedInterval)); normalized == "" || normalized == "auto" {
			counts, err = h.timelineCounts(r.Context(), query, interval)
			if err != nil {
				writeError(w, http.StatusBadGateway, "timeline query failed")
				return
			}
			rawStart, rawEnd, hasRange = timelineRangeFromCounts(counts)
		}
	}
	rangeStart, rangeEnd, hasBuckets := timelineBucketRange(rawStart, rawEnd, hasRange, interval)
	var buckets []TimelineBucket
	if hasBuckets {
		for cursor := rangeStart; cursor.Before(rangeEnd); cursor = addTimelineInterval(cursor, interval) {
			end := addTimelineInterval(cursor, interval)
			buckets = append(buckets, TimelineBucket{Start: formatSearchTime(cursor), End: formatSearchTime(end), Count: counts[cursor]})
		}
	}
	writeJSON(w, http.StatusOK, TimelineResponse{Interval: interval, TimeRange: timelineResponseRange(query, rawStart, rawEnd, hasRange), Buckets: buckets})
}

func (h *Handler) timelineCounts(ctx context.Context, query SearchQuery, interval string) (map[time.Time]int, error) {
	if os.Getenv("XDP_OUTPUT") == "clickhouse" {
		buckets, err := h.clickhouse.Timeline(ctx, ch.TimelineQuery{Index: query.Index, Keyword: query.Keyword, Field: query.Field, Value: query.Value, StartTime: query.StartTime, EndTime: query.EndTime, Interval: interval, HotFields: h.hotFieldsForIndex(query.Index)})
		if err != nil {
			h.logger.Warn("clickhouse timeline failed, falling back to memory", "error", err)
		} else {
			return timelineCountsFromClickHouse(buckets), nil
		}
	}
	buckets := memoryoutput.DefaultStore().Timeline(memoryoutput.TimelineQuery{Index: query.Index, Keyword: query.Keyword, Field: query.Field, Value: query.Value, StartTime: query.StartTime, EndTime: query.EndTime, Interval: interval, Location: searchLocation})
	counts := map[time.Time]int{}
	for _, bucket := range buckets {
		counts[bucket.Start.In(searchLocation)] = bucket.Count
	}
	return counts, nil
}

func timelineCountsFromClickHouse(buckets []ch.TimelineBucket) map[time.Time]int {
	counts := map[time.Time]int{}
	for _, bucket := range buckets {
		counts[bucket.Start.In(searchLocation)] = bucket.Count
	}
	return counts
}

func timelineRangeFromCounts(counts map[time.Time]int) (time.Time, time.Time, bool) {
	var start time.Time
	var end time.Time
	for bucket, count := range counts {
		if count <= 0 {
			continue
		}
		local := bucket.In(searchLocation)
		if start.IsZero() || local.Before(start) {
			start = local
		}
		if end.IsZero() || local.After(end) {
			end = local
		}
	}
	if start.IsZero() || end.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

func timelineRawRange(query SearchQuery, events []*event.Event) (time.Time, time.Time, bool) {
	start := query.StartTime
	end := query.EndTime
	if start.IsZero() || end.IsZero() {
		for _, e := range events {
			eventTime := e.EventTime.In(searchLocation)
			if start.IsZero() || eventTime.Before(start.In(searchLocation)) {
				start = eventTime
			}
			if end.IsZero() || eventTime.After(end.In(searchLocation)) {
				end = eventTime
			}
		}
	}
	if start.IsZero() || end.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	return start.In(searchLocation), end.In(searchLocation), true
}

func normalizeTimelineInterval(value string, start time.Time, end time.Time, hasRange bool) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "minute", "m", "1m":
		return "minute"
	case "hour", "h", "1h":
		return "hour"
	case "day", "d", "1d":
		return "day"
	case "month", "mon", "1mon", "1month":
		return "month"
	}
	if !hasRange {
		return "hour"
	}
	span := end.Sub(start)
	if span <= 2*time.Hour {
		return "minute"
	}
	if span <= 48*time.Hour {
		return "hour"
	}
	if span <= 90*24*time.Hour {
		return "day"
	}
	return "month"
}

func timelineBucketRange(start time.Time, end time.Time, hasRange bool, interval string) (time.Time, time.Time, bool) {
	if !hasRange {
		return time.Time{}, time.Time{}, false
	}
	rangeStart := timelineBucketStart(start, interval)
	rangeEnd := timelineRangeEnd(end, interval)
	if !rangeEnd.After(rangeStart) {
		rangeEnd = addTimelineInterval(rangeStart, interval)
	}
	return rangeStart, rangeEnd, true
}

func timelineResponseRange(query SearchQuery, start time.Time, end time.Time, hasRange bool) *SearchTimeRange {
	if out := searchTimeRange(query); out != nil {
		return out
	}
	if !hasRange {
		return nil
	}
	startText := formatSearchTime(start)
	endText := formatSearchTime(end)
	return &SearchTimeRange{StartTime: startText, EndTime: endText, Start: startText, End: endText}
}

func timelineBucketStart(value time.Time, interval string) time.Time {
	local := value.In(searchLocation)
	year, month, day := local.Date()
	switch interval {
	case "minute":
		return time.Date(year, month, day, local.Hour(), local.Minute(), 0, 0, searchLocation)
	case "day":
		return time.Date(year, month, day, 0, 0, 0, 0, searchLocation)
	case "month":
		return time.Date(year, month, 1, 0, 0, 0, 0, searchLocation)
	default:
		return time.Date(year, month, day, local.Hour(), 0, 0, 0, searchLocation)
	}
}

func timelineRangeEnd(value time.Time, interval string) time.Time {
	start := timelineBucketStart(value, interval)
	if start.Equal(value.In(searchLocation)) {
		return start
	}
	return addTimelineInterval(start, interval)
}

func addTimelineInterval(value time.Time, interval string) time.Time {
	switch interval {
	case "minute":
		return value.Add(time.Minute)
	case "day":
		return value.AddDate(0, 0, 1)
	case "month":
		return value.AddDate(0, 1, 0)
	default:
		return value.Add(time.Hour)
	}
}

func (h *Handler) retryDeadletter(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req RetryDeadletterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid retry request")
		return
	}
	events := h.deadletterEvents(r.Context())
	var target *event.Event
	for _, e := range events {
		if e.EventID == req.EventID {
			target = e
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "deadletter event not found")
		return
	}
	copyEvent := *target
	copyEvent.Errors = nil
	result, err := h.runtime.Execute(r.Context(), h.pipelineForSource("http-json"), &copyEvent)
	if err != nil {
		deadletterStore.Append(result.Event)
		writeJSON(w, http.StatusAccepted, IngestResponse{Status: result.Status, Event: result.Event})
		return
	}
	writeJSON(w, http.StatusOK, IngestResponse{Status: "indexed", Event: result.Event})
}

func (h *Handler) deadletterEvents(ctx context.Context) []*event.Event {
	if h.mysql != nil {
		if events, err := h.mysql.ListDeadletters(ctx); err == nil {
			return events
		}
	}
	return deadletterStore.Search(memoryoutput.SearchQuery{Limit: 1000})
}

func deadletterRecordsFromEvents(events []*event.Event) []DeadletterRecord {
	records := make([]DeadletterRecord, 0, len(events))
	for _, e := range events {
		record := DeadletterRecord{ID: e.EventID, EventID: e.EventID, Status: "pending", ErrorCode: "UNKNOWN", RawPreview: previewText(e.Raw, 512), FirstSeenAt: e.IngestTime, LastSeenAt: e.IngestTime, Event: e}
		if len(e.Errors) > 0 {
			last := e.Errors[len(e.Errors)-1]
			record.Stage = last.Stage
			record.PluginCode = last.PluginID
			record.ErrorCode = last.ErrorCode
			record.ErrorMessage = last.Message
			record.Retryable = last.Retryable
			record.LastSeenAt = last.Time
		}
		records = append(records, record)
	}
	return records
}

func parseNonNegative(value string, fallback int) int {
	if strings.TrimSpace(value) == "" {
		if fallback < 0 {
			return 0
		}
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func intervalDuration(value string) time.Duration {
	switch strings.ToLower(value) {
	case "minute", "m", "1m":
		return time.Minute
	case "day", "d", "1d":
		return 24 * time.Hour
	default:
		return time.Hour
	}
}

func valueType(value any) string {
	switch value.(type) {
	case bool:
		return "bool"
	case float64, float32, int, int64, int32, uint64, uint32:
		return "number"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return "string"
	}
}

func stringifySample(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func previewText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
