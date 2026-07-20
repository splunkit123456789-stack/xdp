package mvp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/eventtime"
	"xdp/pkg/pipeline"
	"xdp/pkg/plugin"
	xdpruntime "xdp/pkg/runtime"
	"xdp/pkg/search/splquery"
	"xdp/pkg/search/splstats"
	ch "xdp/pkg/storage/clickhouse"
	mysqlstore "xdp/pkg/storage/mysql"
	geoip "xdp/plugins/enrichment/geoip"
	kafkainput "xdp/plugins/input/kafka"
	sysloginput "xdp/plugins/input/syslog"
	clickhouseoutput "xdp/plugins/output/clickhouse"
	kafkaoutput "xdp/plugins/output/kafka"
	memoryoutput "xdp/plugins/output/memory"
	s3output "xdp/plugins/output/s3"
	jsonparser "xdp/plugins/parser/json"
	propsconfparser "xdp/plugins/parser/propsconf"
	regexparser "xdp/plugins/parser/regex"
	indexrouter "xdp/plugins/router/indexrouter"
	statssearch "xdp/plugins/search/stats"
	fieldmapping "xdp/plugins/transform/fieldmapping"
	typeconvert "xdp/plugins/transform/typeconvert"
)

const searchTimezone = "Asia/Shanghai"

var searchLocation = mustLoadSearchLocation(searchTimezone)

type Handler struct {
	logger           *slog.Logger
	mux              *http.ServeMux
	reg              *plugin.Registry
	runtime          *xdpruntime.Executor
	pipeline         pipeline.Pipeline
	clickhouse       *ch.Client
	mysql            *mysqlstore.Client
	metrics          *Metrics
	auth             AuthConfig
	mu               sync.RWMutex
	dataSources      map[string]DataSource
	indexConfigs     map[string]IndexSummary
	parseRules       map[string]ParseRule
	savedSearches    map[string]mysqlstore.SavedSearch
	importedPlugins  map[string]PluginImportResponse
	runtimePipelines []pipeline.Pipeline
}

var requestSeq atomic.Uint64

func NewHandler(logger *slog.Logger) http.Handler {
	reg := plugin.NewRegistry()
	must(kafkainput.Register(reg))
	must(sysloginput.Register(reg))
	must(propsconfparser.Register(reg))
	must(jsonparser.Register(reg))
	must(regexparser.Register(reg))
	must(fieldmapping.Register(reg))
	must(typeconvert.Register(reg))
	must(indexrouter.Register(reg))
	must(geoip.Register(reg))
	must(kafkaoutput.Register(reg))
	must(memoryoutput.Register(reg))
	must(clickhouseoutput.Register(reg))
	must(s3output.Register(reg))
	must(statssearch.Register(reg))

	pipe := pipeline.SyslogCollectionPipeline()
	if os.Getenv("XDP_OUTPUT") == "clickhouse" {
		pipe.Spec.Outputs = []pipeline.OutputSpec{{ID: "write-clickhouse", Plugin: "clickhouse-output", Version: "1.0.0", Config: map[string]any{"endpoint": env("XDP_CLICKHOUSE_ENDPOINT", "http://127.0.0.1:8123"), "database": env("XDP_CLICKHOUSE_DATABASE", "xdp"), "username": env("XDP_CLICKHOUSE_USERNAME", ""), "password": env("XDP_CLICKHOUSE_PASSWORD", ""), "index": "app"}}}
	}

	auth := authFromEnv()
	var mysqlClient *mysqlstore.Client
	if os.Getenv("XDP_MYSQL_DISABLED") != "true" {
		for attempt := 0; attempt < 30; attempt++ {
			client, err := mysqlstore.Open(mysqlstore.Config{DSN: os.Getenv("XDP_MYSQL_DSN")})
			if err == nil {
				ctx, cancel := contextWithTimeout()
				if err := client.Ping(ctx); err == nil {
					_ = client.Migrate(ctx)
					_ = client.SeedPlugins(ctx, seedableBuiltinPluginMetadata(reg.List("")))
					if auth.Enabled {
						passwordHash, err := hashPassword(auth.Password)
						if err != nil {
							cancel()
							_ = client.Close()
							continue
						}
						_ = client.SeedAuth(ctx, mysqlstore.AuthSeed{
							Username:     auth.Username,
							DisplayName:  auth.Username,
							PasswordHash: passwordHash,
							PasswordAlgo: "bcrypt",
							RoleLabel:    "admin",
							TokenHash:    hashAuthSecret(auth.Token),
							TokenPrefix:  firstTokenPrefix(auth.Token),
							Source:       "env_seed",
						})
					}
					_ = client.EnsureRBACSeeds(ctx)
					mysqlClient = client
					cancel()
					break
				}
				cancel()
				_ = client.Close()
			}
			time.Sleep(time.Second)
		}
	}

	h := &Handler{
		logger:          logger,
		mux:             http.NewServeMux(),
		reg:             reg,
		runtime:         xdpruntime.NewExecutor(reg),
		pipeline:        pipe,
		clickhouse:      ch.New(ch.Config{Endpoint: env("XDP_CLICKHOUSE_ENDPOINT", "http://127.0.0.1:8123"), Database: env("XDP_CLICKHOUSE_DATABASE", "xdp"), Username: env("XDP_CLICKHOUSE_USERNAME", ""), Password: env("XDP_CLICKHOUSE_PASSWORD", "")}),
		mysql:           mysqlClient,
		metrics:         &Metrics{},
		auth:            auth,
		dataSources:     defaultDataSources(),
		indexConfigs:    defaultIndexConfigs(),
		parseRules:      defaultParseRules(),
		savedSearches:   defaultSavedSearches(),
		importedPlugins: map[string]PluginImportResponse{},
	}
	h.runtimePipelines = h.buildRuntimePipelines()
	if h.mysql != nil {
		ctx, cancel := contextWithTimeout()
		h.seedConfigStore(ctx)
		h.loadConfigStore(ctx)
		h.recoverExecutableSearchCommandRuntimes(ctx)
		cancel()
		h.startIndexSnapshotSampler()
	}
	h.routes()
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) routes() {
	h.mux.HandleFunc("GET /", h.web)
	h.mux.HandleFunc("GET /healthz", h.health)
	h.mux.HandleFunc("GET /readyz", h.health)
	h.mux.HandleFunc("GET /metrics", h.prometheus)
	h.mux.HandleFunc("GET /api/v1/writer/runtime", h.withPermission("index:read", h.writerRuntime))
	h.mux.HandleFunc("GET /api/v1/auth", h.authStatus)
	h.mux.HandleFunc("POST /api/v1/login", h.login)
	h.mux.HandleFunc("GET /api/v1/me", h.withAuthenticatedPrincipal(h.getCurrentUser))
	h.mux.HandleFunc("GET /api/v1/users", h.withPermission("rbac:manage", h.listUsers))
	h.mux.HandleFunc("POST /api/v1/users", h.withPermission("rbac:manage", h.createUser))
	h.mux.HandleFunc("GET /api/v1/users/{id}", h.withPermission("rbac:manage", h.getUser))
	h.mux.HandleFunc("PUT /api/v1/users/{id}", h.withPermission("rbac:manage", h.updateUser))
	h.mux.HandleFunc("DELETE /api/v1/users/{id}", h.withPermission("rbac:manage", h.deleteUser))
	h.mux.HandleFunc("PUT /api/v1/users/{id}/password", h.withPermission("rbac:manage", h.resetUserPassword))
	h.mux.HandleFunc("PUT /api/v1/users/{id}/roles", h.withPermission("rbac:manage", h.setUserRoles))
	h.mux.HandleFunc("GET /api/v1/roles", h.withPermission("rbac:manage", h.listRoles))
	h.mux.HandleFunc("POST /api/v1/roles", h.withPermission("rbac:manage", h.createRole))
	h.mux.HandleFunc("GET /api/v1/roles/{id}", h.withPermission("rbac:manage", h.getRole))
	h.mux.HandleFunc("PUT /api/v1/roles/{id}", h.withPermission("rbac:manage", h.updateRole))
	h.mux.HandleFunc("DELETE /api/v1/roles/{id}", h.withPermission("rbac:manage", h.deleteRole))
	h.mux.HandleFunc("GET /api/v1/permissions", h.withPermission("rbac:manage", h.listPermissions))
	h.mux.HandleFunc("GET /api/v1/tokens", h.withPermission("token:read", h.listTokens))
	h.mux.HandleFunc("POST /api/v1/tokens", h.withPermission("token:create", h.createToken))
	h.mux.HandleFunc("DELETE /api/v1/tokens/{id}", h.withPermission("token:revoke", h.revokeToken))
	h.mux.HandleFunc("GET /api/v1/audit-logs", h.withPermission("audit:read", h.listAuthAuditLogs))
	h.mux.HandleFunc("GET /api/v1/plugins", h.withAuthenticatedPrincipal(h.listPlugins))
	h.mux.HandleFunc("GET /api/v1/plugins/catalog", h.withAuthenticatedPrincipal(h.listPluginCatalog))
	h.mux.HandleFunc("POST /api/v1/plugins/import", h.withAuthenticatedPrincipal(h.importPlugin))
	h.mux.HandleFunc("GET /api/v1/plugins/{plugin_code}", h.withAuthenticatedPrincipal(h.getPlugin))
	h.mux.HandleFunc("GET /api/v1/plugins/{plugin_code}/schema", h.withAuthenticatedPrincipal(h.getPluginSchema))
	h.mux.HandleFunc("GET /api/v1/plugins/{plugin_code}/execution-audits", h.withAuthenticatedPrincipal(h.getPluginExecutionAudits))
	h.mux.HandleFunc("POST /api/v1/plugins/{plugin_code}/enable", h.withAuthenticatedPrincipal(h.enablePlugin))
	h.mux.HandleFunc("POST /api/v1/plugins/{plugin_code}/disable", h.withAuthenticatedPrincipal(h.disablePlugin))
	h.mux.HandleFunc("DELETE /api/v1/plugins/{plugin_code}", h.withAuthenticatedPrincipal(h.deletePlugin))
	h.mux.HandleFunc("GET /api/v1/input-plugins", h.withPermission("datasource:read", h.listInputPlugins))
	h.mux.HandleFunc("GET /api/v1/parser-plugins", h.withPermission("parse_rule:read", h.listParserPlugins))
	h.mux.HandleFunc("GET /api/v1/pipelines", h.withPermission("parse_rule:read", h.listPipelines))
	h.mux.HandleFunc("GET /api/v1/runtime/pipelines", h.withPermission("parse_rule:read", h.listRuntimePipelines))
	h.mux.HandleFunc("POST /api/v1/pipelines", h.withPermission("parse_rule:update", h.savePipeline))
	h.mux.HandleFunc("GET /api/v1/indexes", h.withPermission("index:read", h.listIndexes))
	h.mux.HandleFunc("POST /api/v1/indexes/snapshots", h.withPermission("index:manage", h.sampleIndexSnapshots))
	h.mux.HandleFunc("POST /api/v1/indexes", h.withPermission("index:manage", h.saveIndex))
	h.mux.HandleFunc("DELETE /api/v1/indexes", h.withPermission("index:manage", h.deleteIndex))
	h.mux.HandleFunc("GET /api/v1/indexes/{index_name}", h.withPermission("index:read", h.getIndex))
	h.mux.HandleFunc("PUT /api/v1/indexes/{index_name}", h.withPermission("index:manage", h.updateIndex))
	h.mux.HandleFunc("PATCH /api/v1/indexes/{index_name}/ttl", h.withPermission("index:manage", h.updateIndexTTL))
	h.mux.HandleFunc("GET /api/v1/indexes/{index_name}/trend", h.withPermission("index:read", h.getIndexTrend))
	h.mux.HandleFunc("DELETE /api/v1/indexes/{index_name}", h.withPermission("index:manage", h.deleteIndexPath))
	h.mux.HandleFunc("GET /api/v1/datasources", h.withPermission("datasource:read", h.listDataSources))
	h.mux.HandleFunc("POST /api/v1/datasources/port-check", h.withAnyPermission([]string{"datasource:create", "datasource:update"}, h.checkDataSourcePort))
	h.mux.HandleFunc("POST /api/v1/datasources/connectivity-check", h.withAnyPermission([]string{"datasource:create", "datasource:update"}, h.checkDataSourceConnectivity))
	h.mux.HandleFunc("POST /api/v1/datasources", h.withPermission("datasource:create", h.saveDataSource))
	h.mux.HandleFunc("GET /api/v1/datasources/{id}", h.withPermission("datasource:read", h.getDataSource))
	h.mux.HandleFunc("PUT /api/v1/datasources/{id}", h.withPermission("datasource:update", h.updateDataSource))
	h.mux.HandleFunc("PATCH /api/v1/datasources/{id}/status", h.withPermissionResolver(dataSourceStatusPermission, h.updateDataSourceStatus))
	h.mux.HandleFunc("GET /api/v1/datasources/{id}/runtime", h.withPermission("datasource:read", h.getDataSourceRuntime))
	h.mux.HandleFunc("DELETE /api/v1/datasources/{id}", h.withPermission("datasource:delete", h.deleteDataSource))
	h.mux.HandleFunc("GET /api/v1/parse-rules", h.withPermission("parse_rule:read", h.listParseRules))
	h.mux.HandleFunc("POST /api/v1/parse-rules", h.withPermission("parse_rule:create", h.createParseRule))
	h.mux.HandleFunc("GET /api/v1/parse-rules/{id}", h.withPermission("parse_rule:read", h.getParseRule))
	h.mux.HandleFunc("PUT /api/v1/parse-rules/{id}", h.withPermission("parse_rule:update", h.updateParseRule))
	h.mux.HandleFunc("PATCH /api/v1/parse-rules/{id}/status", h.withPermission("parse_rule:update", h.updateParseRuleStatus))
	h.mux.HandleFunc("DELETE /api/v1/parse-rules/{id}", h.withPermission("parse_rule:delete", h.deleteParseRule))
	h.mux.HandleFunc("POST /api/v1/parse-rules/{id}/test", h.withPermission("parse_rule:test", h.testParseRule))
	h.mux.HandleFunc("GET /api/v1/search", h.withPermission("search:execute", h.search))
	h.mux.HandleFunc("GET /api/v1/search/fields", h.withPermission("search:execute", h.searchFields))
	h.mux.HandleFunc("GET /api/v1/search/timeline", h.withPermission("search:execute", h.searchTimeline))
	h.mux.HandleFunc("GET /api/v1/search/favorites", h.withPermission("search:saved_search", h.listSavedSearches))
	h.mux.HandleFunc("POST /api/v1/search/favorites", h.withPermission("search:saved_search", h.createSavedSearch))
	h.mux.HandleFunc("DELETE /api/v1/search/favorites/{id}", h.withPermission("search:saved_search", h.deleteSavedSearch))
	h.mux.HandleFunc("GET /api/v1/deadletters", h.withPermission("parse_rule:read", h.deadletters))
	h.mux.HandleFunc("POST /api/v1/deadletters/retry", h.withPermission("parse_rule:update", h.retryDeadletter))
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *Handler) listPlugins(w http.ResponseWriter, r *http.Request) {
	filterType := pluginTypeFromRequest(r)
	if filterType == "" {
		writeErrorCode(w, http.StatusBadRequest, "PLUGIN_TYPE_REQUIRED", "plugin_type is required")
		return
	}
	if !supportedImportedPluginType(filterType) {
		writeErrorCode(w, http.StatusBadRequest, "PLUGIN_TYPE_UNSUPPORTED", "unsupported plugin_type")
		return
	}
	principal := h.currentPrincipal(r.Context())
	if !principal.HasAnyPluginScope("manage", filterType) {
		h.writePluginScopeDenied(w, r, principal, "manage", filterType, "")
		return
	}
	allPlugins := filterPluginResponsesByScope(h.allCurrentPlugins(r.Context()), principal, "manage", "")
	typeCounts := pluginTypeCounts(allPlugins)
	plugins := filterPluginsByType(allPlugins, filterType)
	pageItems, pagination := paginateList(plugins, r)
	writeJSON(w, http.StatusOK, map[string]any{
		"plugins":     pageItems,
		"pagination":  pagination,
		"type_counts": typeCounts,
	})
}

func (h *Handler) listPluginCatalog(w http.ResponseWriter, r *http.Request) {
	filterType := pluginTypeFromRequest(r)
	if filterType == "" {
		writeErrorCode(w, http.StatusBadRequest, "PLUGIN_TYPE_REQUIRED", "plugin_type is required")
		return
	}
	if !supportedImportedPluginType(filterType) {
		writeErrorCode(w, http.StatusBadRequest, "PLUGIN_TYPE_UNSUPPORTED", "unsupported plugin_type")
		return
	}
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = "enabled"
	}
	principal := h.currentPrincipal(r.Context())
	plugins := filterPluginsByType(h.allCurrentPlugins(r.Context()), filterType)
	filtered := make([]PluginImportResponse, 0, len(plugins))
	for _, item := range plugins {
		if !principal.AllowsPlugin("use", item.PluginType, item.PluginCode) {
			continue
		}
		if status == "all" || (status == "enabled" && isPluginEnabled(item.Status)) || strings.EqualFold(item.Status, status) {
			filtered = append(filtered, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": filtered})
}

func (h *Handler) allCurrentPlugins(ctx context.Context) []PluginImportResponse {
	items := pluginResponsesFromMetadata(h.reg.List(""), "")
	if h.mysql != nil {
		if records, err := h.mysql.ListPluginRecords(ctx); err == nil {
			items = append(items, pluginResponsesFromRecords(records, "")...)
		}
	}
	h.mu.RLock()
	for _, item := range h.importedPlugins {
		items = append(items, item)
	}
	h.mu.RUnlock()
	return currentPluginResponses(items)
}

func seedableBuiltinPluginMetadata(items []plugin.Metadata) []plugin.Metadata {
	filtered := make([]plugin.Metadata, 0, len(items))
	for _, item := range items {
		pluginType := normalizePluginType(string(item.Type))
		if productVisibleBuiltinPlugin(pluginType, item.Code) {
			item.Type = plugin.Type(pluginType)
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func currentPluginResponses(items []PluginImportResponse) []PluginImportResponse {
	current := map[string]PluginImportResponse{}
	for _, item := range deduplicatePluginResponses(items) {
		item = normalizePluginResponse(item)
		key := item.PluginType + "/" + item.PluginCode
		existing, ok := current[key]
		if !ok || preferCurrentPlugin(item, existing) {
			current[key] = item
		}
	}
	out := make([]PluginImportResponse, 0, len(current))
	for _, item := range current {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PluginType == out[j].PluginType {
			return out[i].PluginCode < out[j].PluginCode
		}
		return out[i].PluginType < out[j].PluginType
	})
	return out
}

func preferCurrentPlugin(candidate, existing PluginImportResponse) bool {
	candidateEnabled := isPluginEnabled(candidate.Status)
	existingEnabled := isPluginEnabled(existing.Status)
	if candidateEnabled != existingEnabled {
		return candidateEnabled
	}
	return compareSemanticVersion(candidate.PluginVersion, existing.PluginVersion) > 0
}

func filterPluginsByType(items []PluginImportResponse, pluginType string) []PluginImportResponse {
	filtered := make([]PluginImportResponse, 0, len(items))
	for _, item := range items {
		if item.PluginType == pluginType {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterPluginResponsesByScope(items []PluginImportResponse, principal AuthenticatedPrincipal, action string, pluginType string) []PluginImportResponse {
	filtered := make([]PluginImportResponse, 0, len(items))
	for _, item := range items {
		if pluginType != "" && item.PluginType != pluginType {
			continue
		}
		if principal.AllowsPlugin(action, item.PluginType, item.PluginCode) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func pluginTypeCounts(items []PluginImportResponse) map[string]int {
	counts := map[string]int{
		"input":          0,
		"parser":         0,
		"search_command": 0,
	}
	for _, item := range items {
		if _, ok := counts[item.PluginType]; ok {
			counts[item.PluginType]++
		}
	}
	return counts
}

func (h *Handler) importPlugin(w http.ResponseWriter, r *http.Request) {
	data, err := readPluginPackage(r)
	if err != nil {
		writePluginPackageError(w, err)
		return
	}
	manifest, err := parsePluginManifest(data)
	if err != nil {
		writePluginPackageError(w, err)
		return
	}
	if err := validatePluginPackageAssets(data, manifest); err != nil {
		writePluginPackageError(w, err)
		return
	}
	overwrite, _ := strconv.ParseBool(strings.TrimSpace(r.URL.Query().Get("overwrite")))
	item := manifest.toImportResponse(pluginChecksum(data))
	item.PackageBytes = data
	principal := h.currentPrincipal(r.Context())
	if !h.authorizePluginScope(w, r, principal, "manage", item.PluginType, item.PluginCode) {
		return
	}
	existing, exists := h.findPlugin(item.PluginType, item.PluginCode, "")
	if exists {
		if compareSemanticVersion(item.PluginVersion, existing.PluginVersion) <= 0 {
			if !overwrite {
				writeErrorCode(w, http.StatusConflict, "PLUGIN_ALREADY_EXISTS", "plugin already exists, confirm overwrite to replace the current plugin package")
				return
			}
		}
		if apiErr := h.validatePluginUpgrade(item); apiErr != nil {
			writeErrorCode(w, apiErr.status, apiErr.code, apiErr.message)
			return
		}
		item.Status = existing.Status
	}
	if isPluginEnabled(item.Status) {
		if err := prepareExecutableSearchCommandPlugin(item); err != nil {
			writeErrorCode(w, http.StatusBadRequest, "PLUGIN_RUNTIME_PREPARE_FAILED", err.Error())
			return
		}
	}
	if h.mysql != nil {
		if err := h.mysql.UpsertPluginRecord(r.Context(), item.toStoreRecord()); err != nil {
			writeErrorCode(w, http.StatusInternalServerError, "PLUGIN_PERSIST_FAILED", err.Error())
			return
		}
	}
	h.mu.Lock()
	h.importedPlugins[item.key()] = item
	h.mu.Unlock()
	if exists && isPluginEnabled(item.Status) && existing.Checksum != "" && existing.Checksum != item.Checksum {
		if err := removeExecutableSearchCommandPluginRuntime(existing); err != nil {
			h.logger.Warn("remove previous plugin runtime failed", "plugin_type", existing.PluginType, "plugin_code", existing.PluginCode, "error", err)
		}
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) getPlugin(w http.ResponseWriter, r *http.Request) {
	pluginType := pluginTypeFromRequest(r)
	code := strings.TrimSpace(r.PathValue("plugin_code"))
	principal := h.currentPrincipal(r.Context())
	if !h.authorizePluginScope(w, r, principal, "manage", pluginType, code) {
		return
	}
	item, ok := h.findPlugin(pluginType, code, "")
	if !ok {
		writeErrorCode(w, http.StatusNotFound, "PLUGIN_NOT_FOUND", "plugin not found")
		return
	}
	writeJSON(w, http.StatusOK, h.pluginDetail(item))
}

func (h *Handler) getPluginSchema(w http.ResponseWriter, r *http.Request) {
	pluginType := pluginTypeFromRequest(r)
	code := strings.TrimSpace(r.PathValue("plugin_code"))
	principal := h.currentPrincipal(r.Context())
	if !h.authorizePluginScope(w, r, principal, "manage", pluginType, code) {
		return
	}
	item, ok := h.findPlugin(pluginType, code, "")
	if !ok {
		writeErrorCode(w, http.StatusNotFound, "PLUGIN_NOT_FOUND", "plugin not found")
		return
	}
	payload := map[string]any{
		"plugin_code":       item.PluginCode,
		"plugin_type":       item.PluginType,
		"plugin_version":    item.PluginVersion,
		"config_schema":     item.ConfigSchema,
		"ui_schema":         item.UISchema,
		"input_schema":      item.InputSchema,
		"output_schema":     item.OutputSchema,
		"permission_schema": item.PermissionSchema,
		"runtime_config":    item.RuntimeConfig,
	}
	if strings.TrimSpace(item.Runtime) == "executable_search_command" {
		payload["effective_runtime_config"] = effectiveExecutablePluginRuntimeConfig(item.RuntimeConfig)
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) getPluginExecutionAudits(w http.ResponseWriter, r *http.Request) {
	pluginType := pluginTypeFromRequest(r)
	if pluginType != "search_command" {
		writeErrorCode(w, http.StatusBadRequest, "PLUGIN_TYPE_UNSUPPORTED", "execution audits only support search_command plugins")
		return
	}
	code := strings.TrimSpace(r.PathValue("plugin_code"))
	principal := h.currentPrincipal(r.Context())
	if !h.authorizePluginScope(w, r, principal, "manage", pluginType, code) {
		return
	}
	item, ok := h.findPlugin(pluginType, code, "")
	if !ok {
		writeErrorCode(w, http.StatusNotFound, "PLUGIN_NOT_FOUND", "plugin not found")
		return
	}
	if productVisibleBuiltinPlugin(item.PluginType, item.PluginCode) {
		writeErrorCode(w, http.StatusConflict, "BUILTIN_PLUGIN_PROTECTED", "builtin plugin does not expose execution audits")
		return
	}
	limit := ch.ParseLimit(r.URL.Query().Get("limit"), 20)
	if limit > 100 {
		limit = 100
	}
	audits := []map[string]any{}
	if h.mysql != nil {
		items, err := h.mysql.ListSearchCommandExecutionAudits(r.Context(), item.PluginCode, limit)
		if err != nil {
			writeErrorCode(w, http.StatusBadGateway, "PLUGIN_EXEC_AUDIT_QUERY_FAILED", err.Error())
			return
		}
		for _, audit := range items {
			audits = append(audits, searchCommandExecutionAuditResponse(audit))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plugin_code": item.PluginCode,
		"plugin_type": item.PluginType,
		"audits":      audits,
	})
}

func searchCommandExecutionAuditResponse(item mysqlstore.SearchCommandExecutionAudit) map[string]any {
	return map[string]any{
		"request_id":       item.RequestID,
		"search_id":        item.SearchID,
		"plugin_type":      item.PluginType,
		"plugin_code":      item.PluginCode,
		"plugin_version":   item.PluginVersion,
		"command_name":     item.CommandName,
		"runtime":          item.Runtime,
		"interpreter":      item.Interpreter,
		"timeout_ms":       item.TimeoutMS,
		"max_input_rows":   item.MaxInputRows,
		"max_output_bytes": item.MaxOutputBytes,
		"input_rows":       item.InputRows,
		"output_rows":      item.OutputRows,
		"elapsed_ms":       item.ElapsedMS,
		"success":          item.Success,
		"error_code":       item.ErrorCode,
		"error_message":    item.ErrorMessage,
		"stdout_bytes":     item.StdoutBytes,
		"stderr_bytes":     item.StderrBytes,
		"created_at":       item.CreatedAt.Format(time.RFC3339Nano),
	}
}

func (h *Handler) enablePlugin(w http.ResponseWriter, r *http.Request) {
	h.setPluginStatus(w, r, "enabled")
}

func (h *Handler) disablePlugin(w http.ResponseWriter, r *http.Request) {
	pluginType := pluginTypeFromRequest(r)
	code := strings.TrimSpace(r.PathValue("plugin_code"))
	if productVisibleBuiltinPlugin(pluginType, code) {
		writeErrorCode(w, http.StatusConflict, "BUILTIN_PLUGIN_PROTECTED", "builtin plugin is protected")
		return
	}
	if h.pluginReferenceCount(pluginType, code) > 0 {
		writeErrorCode(w, http.StatusConflict, "PLUGIN_IN_USE", "plugin is in use")
		return
	}
	h.setPluginStatus(w, r, "disabled")
}

func (h *Handler) deletePlugin(w http.ResponseWriter, r *http.Request) {
	pluginType := pluginTypeFromRequest(r)
	code := strings.TrimSpace(r.PathValue("plugin_code"))
	principal := h.currentPrincipal(r.Context())
	if !h.authorizePluginScope(w, r, principal, "manage", pluginType, code) {
		return
	}
	if productVisibleBuiltinPlugin(pluginType, code) {
		writeErrorCode(w, http.StatusConflict, "BUILTIN_PLUGIN_PROTECTED", "builtin plugin is protected")
		return
	}
	if h.pluginReferenceCount(pluginType, code) > 0 {
		writeErrorCode(w, http.StatusConflict, "PLUGIN_IN_USE", "plugin is in use")
		return
	}
	if h.mysql != nil {
		item, ok := h.findPlugin(pluginType, code, "")
		if !ok {
			writeErrorCode(w, http.StatusNotFound, "PLUGIN_NOT_FOUND", "plugin not found")
			return
		}
		if item.Status == "enabled" || item.Status == "active" {
			writeErrorCode(w, http.StatusConflict, "PLUGIN_IN_USE", "enabled plugin cannot be deleted")
			return
		}
		if err := h.mysql.DeletePlugin(r.Context(), pluginType, code); err != nil {
			writeErrorCode(w, http.StatusNotFound, "PLUGIN_NOT_FOUND", "plugin not found")
			return
		}
		if err := removeExecutableSearchCommandPluginCodeRuntime(item); err != nil {
			h.logger.Warn("remove plugin runtime failed", "plugin_type", item.PluginType, "plugin_code", item.PluginCode, "error", err)
		}
		h.mu.Lock()
		delete(h.importedPlugins, pluginKey(pluginType, code, ""))
		h.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	key := pluginKey(pluginType, code, "")
	item, ok := h.importedPlugins[key]
	if !ok {
		writeErrorCode(w, http.StatusNotFound, "PLUGIN_NOT_FOUND", "plugin not found")
		return
	}
	if item.Status == "enabled" || item.Status == "active" {
		writeErrorCode(w, http.StatusConflict, "PLUGIN_IN_USE", "enabled plugin cannot be deleted")
		return
	}
	if err := removeExecutableSearchCommandPluginCodeRuntime(item); err != nil {
		h.logger.Warn("remove plugin runtime failed", "plugin_type", item.PluginType, "plugin_code", item.PluginCode, "error", err)
	}
	delete(h.importedPlugins, key)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setPluginStatus(w http.ResponseWriter, r *http.Request, status string) {
	pluginType := pluginTypeFromRequest(r)
	code := strings.TrimSpace(r.PathValue("plugin_code"))
	principal := h.currentPrincipal(r.Context())
	if !h.authorizePluginScope(w, r, principal, "manage", pluginType, code) {
		return
	}
	if productVisibleBuiltinPlugin(pluginType, code) {
		writeErrorCode(w, http.StatusConflict, "BUILTIN_PLUGIN_PROTECTED", "builtin plugin is protected")
		return
	}
	if h.mysql != nil {
		if status == "enabled" {
			item, ok := h.findPlugin(pluginType, code, "")
			if !ok {
				writeErrorCode(w, http.StatusNotFound, "PLUGIN_NOT_FOUND", "plugin not found")
				return
			}
			if err := prepareExecutableSearchCommandPlugin(item); err != nil {
				writeErrorCode(w, http.StatusBadRequest, "PLUGIN_RUNTIME_PREPARE_FAILED", err.Error())
				return
			}
		}
		item, err := h.mysql.SetPluginStatus(r.Context(), pluginType, code, status)
		if err != nil {
			writeErrorCode(w, http.StatusNotFound, "PLUGIN_NOT_FOUND", "plugin not found")
			return
		}
		plugins := pluginResponsesFromRecords([]mysqlstore.PluginRecord{item}, pluginType)
		if len(plugins) == 0 {
			writeErrorCode(w, http.StatusNotFound, "PLUGIN_NOT_FOUND", "plugin not found")
			return
		}
		h.mu.Lock()
		h.importedPlugins[plugins[0].key()] = plugins[0]
		h.mu.Unlock()
		if status == "disabled" {
			if err := removeExecutableSearchCommandPluginCodeRuntime(plugins[0]); err != nil {
				h.logger.Warn("remove plugin runtime failed", "plugin_type", plugins[0].PluginType, "plugin_code", plugins[0].PluginCode, "error", err)
			}
		}
		writeJSON(w, http.StatusOK, plugins[0])
		return
	}
	h.mu.Lock()
	key := pluginKey(pluginType, code, "")
	item, ok := h.importedPlugins[key]
	if !ok {
		h.mu.Unlock()
		writeErrorCode(w, http.StatusNotFound, "PLUGIN_NOT_FOUND", "plugin not found")
		return
	}
	h.mu.Unlock()
	if status == "enabled" {
		if err := prepareExecutableSearchCommandPlugin(item); err != nil {
			writeErrorCode(w, http.StatusBadRequest, "PLUGIN_RUNTIME_PREPARE_FAILED", err.Error())
			return
		}
	}
	item.Status = status
	h.mu.Lock()
	h.importedPlugins[key] = item
	h.mu.Unlock()
	if status == "disabled" {
		if err := removeExecutableSearchCommandPluginCodeRuntime(item); err != nil {
			h.logger.Warn("remove plugin runtime failed", "plugin_type", item.PluginType, "plugin_code", item.PluginCode, "error", err)
		}
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) recoverExecutableSearchCommandRuntimes(ctx context.Context) {
	for _, item := range h.allCurrentPlugins(ctx) {
		if item.PluginType != "search_command" || !isPluginEnabled(item.Status) || strings.TrimSpace(item.Runtime) != "executable_search_command" {
			continue
		}
		if err := prepareExecutableSearchCommandPlugin(item); err != nil {
			h.logger.Warn("recover executable search command runtime failed", "plugin_code", item.PluginCode, "error", err)
		}
	}
}

func (h *Handler) validatePluginUpgrade(candidate PluginImportResponse) *collectAPIError {
	switch candidate.PluginType {
	case "input":
		h.mu.RLock()
		configs := make([]map[string]any, 0)
		for _, source := range h.dataSources {
			if source.Status != "deleted" && source.PluginCode == candidate.PluginCode {
				configs = append(configs, cloneAnyMap(source.PluginConfig))
			}
		}
		h.mu.RUnlock()
		for _, config := range configs {
			if apiErr := validateConfigBySchema(candidate.ConfigSchema, config); apiErr != nil {
				return &collectAPIError{status: http.StatusConflict, code: "PLUGIN_UPGRADE_INCOMPATIBLE", message: apiErr.message}
			}
		}
	case "parser":
		h.mu.RLock()
		configs := make([]map[string]any, 0)
		for _, rule := range h.parseRules {
			if rule.Status != "deleted" && rule.ParserPlugin == candidate.PluginCode {
				configs = append(configs, cloneAnyMap(rule.PluginConfig))
			}
		}
		h.mu.RUnlock()
		for _, config := range configs {
			if apiErr := validateConfigBySchema(candidate.ConfigSchema, config); apiErr != nil {
				return &collectAPIError{status: http.StatusConflict, code: "PLUGIN_UPGRADE_INCOMPATIBLE", message: apiErr.message}
			}
		}
	}
	return nil
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (h *Handler) pluginDetail(item PluginImportResponse) map[string]any {
	references := h.pluginReferences(item.PluginType, item.PluginCode)
	detail := map[string]any{
		"plugin_code":       item.PluginCode,
		"plugin_type":       item.PluginType,
		"plugin_version":    item.PluginVersion,
		"name":              item.Name,
		"description":       item.Description,
		"runtime":           item.Runtime,
		"entrypoint":        item.Entrypoint,
		"status":            item.Status,
		"checksum":          item.Checksum,
		"config_schema":     item.ConfigSchema,
		"ui_schema":         item.UISchema,
		"input_schema":      item.InputSchema,
		"output_schema":     item.OutputSchema,
		"permission_schema": item.PermissionSchema,
		"runtime_config":    item.RuntimeConfig,
		"built_in":          productVisibleBuiltinPlugin(item.PluginType, item.PluginCode),
		"references": map[string]any{
			"count": len(references),
			"items": references,
		},
	}
	if strings.TrimSpace(item.Runtime) == "executable_search_command" {
		detail["effective_runtime_config"] = effectiveExecutablePluginRuntimeConfig(item.RuntimeConfig)
	}
	return detail
}

func (h *Handler) findPlugin(pluginType, code, version string) (PluginImportResponse, bool) {
	if pluginType == "" {
		return PluginImportResponse{}, false
	}
	items := h.pluginVersions(pluginType, code)
	if len(items) == 0 {
		return PluginImportResponse{}, false
	}
	for _, item := range items {
		if item.Status == "enabled" || item.Status == "active" {
			return item, true
		}
	}
	return items[0], true
}

func (h *Handler) pluginVersions(pluginType, code string) []PluginImportResponse {
	items := []PluginImportResponse{}
	seen := map[string]struct{}{}
	add := func(item PluginImportResponse) {
		item = normalizePluginResponse(item)
		key := pluginKey(item.PluginType, item.PluginCode, "")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		items = append(items, item)
	}
	if productVisibleBuiltinPlugin(pluginType, code) {
		for _, item := range pluginResponsesFromMetadata(h.reg.List(""), pluginType) {
			if item.PluginCode == code {
				add(item.withStatus("enabled"))
			}
		}
	}
	if h.mysql != nil {
		if record, err := h.mysql.GetPluginRecord(context.Background(), pluginType, code, ""); err == nil {
			for _, item := range pluginResponsesFromRecords([]mysqlstore.PluginRecord{record}, pluginType) {
				add(item)
			}
		}
	}
	h.mu.RLock()
	for _, item := range h.importedPlugins {
		normalized := normalizePluginResponse(item)
		if normalized.PluginType == pluginType && normalized.PluginCode == strings.TrimSpace(code) {
			add(normalized)
		}
	}
	h.mu.RUnlock()
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].PluginVersion < items[j].PluginVersion
	})
	return items
}

func (h *Handler) pluginReferenceCount(pluginType, code string) int {
	if pluginType == "search_command" && h.mysql != nil {
		if savedSearches, err := h.mysql.ListSavedSearches(context.Background()); err == nil {
			return countSavedSearchCommandReferences(savedSearches, code)
		}
	}

	return len(h.pluginReferences(pluginType, code))
}

func (h *Handler) pluginReferences(pluginType, code string) []map[string]string {
	refs := []map[string]string{}
	h.mu.RLock()
	defer h.mu.RUnlock()
	switch pluginType {
	case "input":
		for _, source := range h.dataSources {
			if source.Status != "deleted" && source.PluginCode == code {
				refs = append(refs, map[string]string{
					"type":   "datasource",
					"id":     source.ID,
					"name":   source.Name,
					"status": source.Status,
				})
			}
		}
	case "parser":
		for _, rule := range h.parseRules {
			if rule.Status != "deleted" && rule.ParserPlugin == code {
				refs = append(refs, map[string]string{
					"type":   "parse_rule",
					"id":     rule.ID,
					"name":   rule.Name,
					"status": rule.Status,
				})
			}
		}
	case "search_command":
		for _, saved := range mapValues(h.savedSearches) {
			if saved.Status != "deleted" && savedSearchUsesCommand(saved.SPL, code) {
				refs = append(refs, map[string]string{
					"type":   "saved_search",
					"id":     saved.ID,
					"name":   saved.Name,
					"status": saved.Status,
				})
			}
		}
	}
	return refs
}

func countSavedSearchCommandReferences(savedSearches []mysqlstore.SavedSearch, code string) int {
	count := 0
	for _, saved := range savedSearches {
		if saved.Status != "deleted" && savedSearchUsesCommand(saved.SPL, code) {
			count++
		}
	}
	return count
}

func savedSearchUsesCommand(spl, code string) bool {
	command := strings.ToLower(strings.TrimSpace(code))
	if command == "" {
		return false
	}
	for _, segment := range strings.Split(spl, "|") {
		fields := strings.Fields(strings.TrimSpace(segment))
		if len(fields) == 0 {
			continue
		}
		if strings.ToLower(fields[0]) == command {
			return true
		}
	}
	return false
}

func mapValues(values map[string]mysqlstore.SavedSearch) []mysqlstore.SavedSearch {
	items := make([]mysqlstore.SavedSearch, 0, len(values))
	for _, item := range values {
		items = append(items, item)
	}
	return items
}

func pluginTypeFromRequest(r *http.Request) string {
	pluginType := normalizePluginType(r.URL.Query().Get("plugin_type"))
	if pluginType == "" {
		pluginType = normalizePluginType(r.URL.Query().Get("type"))
	}
	return pluginType
}

func pluginKey(pluginType, code, version string) string {
	return normalizePluginType(pluginType) + "/" + strings.TrimSpace(code)
}

func writePluginPackageError(w http.ResponseWriter, err error) {
	if pluginErr, ok := err.(pluginPackageError); ok {
		status := http.StatusBadRequest
		switch pluginErr.code {
		case "BUILTIN_PLUGIN_PROTECTED":
			status = http.StatusConflict
		}
		writeErrorCode(w, status, pluginErr.code, pluginErr.message)
		return
	}
	writeErrorCode(w, http.StatusBadRequest, "PLUGIN_PACKAGE_INVALID", err.Error())
}

func (h *Handler) listPipelines(w http.ResponseWriter, r *http.Request) {
	if h.mysql != nil {
		if items, err := h.mysql.ListPipelines(r.Context()); err == nil && len(items) > 0 {
			writeJSON(w, http.StatusOK, items)
			return
		}
	}
	h.mu.RLock()
	items := append([]pipeline.Pipeline(nil), h.runtimePipelines...)
	h.mu.RUnlock()
	if len(items) == 0 {
		items = []pipeline.Pipeline{h.pipeline}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) savePipeline(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var pipe pipeline.Pipeline
	if err := json.NewDecoder(r.Body).Decode(&pipe); err != nil {
		writeError(w, http.StatusBadRequest, "invalid pipeline json")
		return
	}
	if err := pipe.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.pipeline = pipe
	if h.mysql != nil {
		_ = h.mysql.SavePipeline(r.Context(), pipe)
	}
	h.mu.Lock()
	replaced := false
	for i, existing := range h.runtimePipelines {
		if existing.Metadata.ID == pipe.Metadata.ID {
			h.runtimePipelines[i] = pipe
			replaced = true
			break
		}
	}
	if !replaced {
		h.runtimePipelines = append(h.runtimePipelines, pipe)
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, pipe)
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	principal := h.currentPrincipal(r.Context())
	limit := ch.ParseLimit(r.URL.Query().Get("limit"), 20)
	page := ch.ParseLimit(r.URL.Query().Get("page"), 1)
	offset := parseNonNegative(r.URL.Query().Get("offset"), (page-1)*limit)
	startTime, endTime, earliest, latest, err := searchTimeBoundsFromRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	query := SearchQuery{Index: r.URL.Query().Get("index"), Keyword: r.URL.Query().Get("keyword"), Field: r.URL.Query().Get("field"), Value: r.URL.Query().Get("value"), StartTime: startTime, EndTime: endTime, Limit: limit, Offset: offset, Q: r.URL.Query().Get("q"), Earliest: earliest, Latest: latest}
	query.NormalizeFieldFilters()
	if strings.TrimSpace(query.Q) != "" {
		parsed, err := splquery.Parse(query.Q)
		if err != nil {
			writeErrorCode(w, http.StatusBadRequest, searchParseErrorCode(query.Q, err), err.Error())
			return
		}
		query.ApplyFilters(parsed.Filters)
		if err := query.NormalizeIndex(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid index")
			return
		}
		if !h.authorizeSearchIndexScope(w, r, principal, query) {
			return
		}
		if parsed.Stats != nil {
			if !h.authorizePluginScope(w, r, principal, "use", "search_command", "stats") {
				return
			}
			if len(parsed.Commands) > 0 {
				h.searchStatsCommands(w, r, query, *parsed.Stats, parsed.Commands, started)
				return
			}
			h.searchStats(w, r, query, *parsed.Stats, started)
			return
		}
		if len(parsed.Commands) > 0 {
			h.searchCommands(w, r, query, parsed.Commands, started)
			return
		}
	}
	if err := query.NormalizeIndex(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid index")
		return
	}
	if !h.authorizeSearchIndexScope(w, r, principal, query) {
		return
	}
	events, pagination, err := h.findEvents(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusBadGateway, "search failed")
		return
	}
	writeJSON(w, http.StatusOK, newSearchResponse(query, "events", started, func(response *SearchResponse) {
		response.Events = searchEventsFromEvents(events)
		response.Pagination = &pagination
	}))
}

func (h *Handler) searchCommands(w http.ResponseWriter, r *http.Request, query SearchQuery, commands []splquery.Command, started time.Time) {
	principal := h.currentPrincipal(r.Context())
	for _, command := range commands {
		if !h.authorizePluginScope(w, r, principal, "use", "search_command", command.Name) {
			return
		}
	}
	fetchQuery := query
	fetchQuery.Limit = 1000
	fetchQuery.Offset = 0
	events, _, err := h.findEvents(r.Context(), fetchQuery)
	if err != nil {
		writeError(w, http.StatusBadGateway, "search failed")
		return
	}
	requestID := newRequestID()
	result, meta, err := h.executeSearchCommandPlugins(r.Context(), requestID, eventRowsFromEvents(events), nil, commands)
	if err != nil {
		writeSearchCommandPluginError(w, requestID, err)
		return
	}
	rows, pagination := paginateTableRows(result.Rows, query)
	result.Rows = rows
	writeJSON(w, http.StatusOK, newSearchResponse(query, "table", started, func(response *SearchResponse) {
		response.SearchCommand = meta
		response.Table = &SearchTableResult{Fields: result.Fields, Rows: result.Rows, Limit: pagination.Limit}
		response.Pagination = &pagination
	}))
}

func (h *Handler) searchStatsCommands(w http.ResponseWriter, r *http.Request, query SearchQuery, stats splstats.Query, commands []splquery.Command, started time.Time) {
	principal := h.currentPrincipal(r.Context())
	for _, command := range commands {
		if !h.authorizePluginScope(w, r, principal, "use", "search_command", command.Name) {
			return
		}
	}
	fetchQuery := query
	fetchQuery.Limit = 1000
	fetchQuery.Offset = 0
	input := plugin.SearchInput{
		Index:        fetchQuery.Index,
		Keyword:      fetchQuery.Keyword,
		Field:        fetchQuery.Field,
		Value:        fetchQuery.Value,
		FieldFilters: searchPluginFieldFilters(fetchQuery.FieldFilters),
		StartTime:    fetchQuery.StartTime,
		EndTime:      fetchQuery.EndTime,
		Limit:        fetchQuery.Limit,
		Offset:       fetchQuery.Offset,
		HotFields:    searchPluginHotFields(h.hotFieldsForIndex(fetchQuery.Index)),
	}
	if os.Getenv("XDP_OUTPUT") == "clickhouse" {
		input.Backend = clickHouseStatsBackend{client: h.clickhouse}
	} else {
		input.Backend = memoryStatsBackend{store: memoryoutput.DefaultStore()}
	}
	statsResult, err := h.executeStatsSearchPlugin(r.Context(), input, stats)
	if err != nil {
		h.logger.Warn("stats search plugin failed", "error", err)
		writeError(w, http.StatusBadGateway, "stats search plugin failed")
		return
	}
	requestID := newRequestID()
	result, meta, err := h.executeSearchCommandPlugins(r.Context(), requestID, statsResult.Rows, statsResult.Fields, commands)
	if err != nil {
		writeSearchCommandPluginError(w, requestID, err)
		return
	}
	rows, pagination := paginateTableRows(result.Rows, query)
	result.Rows = rows
	writeJSON(w, http.StatusOK, newSearchResponse(query, "table", started, func(response *SearchResponse) {
		response.SearchCommand = meta
		response.Table = &SearchTableResult{Fields: result.Fields, Rows: result.Rows, Limit: pagination.Limit}
		response.Pagination = &pagination
	}))
}

func (h *Handler) searchStats(w http.ResponseWriter, r *http.Request, query SearchQuery, stats splstats.Query, started time.Time) {
	fetchQuery := query
	fetchQuery.Limit = 1000
	fetchQuery.Offset = 0
	input := plugin.SearchInput{
		Index:        fetchQuery.Index,
		Keyword:      fetchQuery.Keyword,
		Field:        fetchQuery.Field,
		Value:        fetchQuery.Value,
		FieldFilters: searchPluginFieldFilters(fetchQuery.FieldFilters),
		StartTime:    fetchQuery.StartTime,
		EndTime:      fetchQuery.EndTime,
		Limit:        fetchQuery.Limit,
		Offset:       fetchQuery.Offset,
		HotFields:    searchPluginHotFields(h.hotFieldsForIndex(fetchQuery.Index)),
	}
	if os.Getenv("XDP_OUTPUT") == "clickhouse" {
		input.Backend = clickHouseStatsBackend{client: h.clickhouse}
	} else {
		input.Backend = memoryStatsBackend{store: memoryoutput.DefaultStore()}
	}
	result, err := h.executeStatsSearchPlugin(r.Context(), input, stats)
	if err != nil {
		h.logger.Warn("stats search plugin failed", "error", err)
		writeError(w, http.StatusBadGateway, "stats search plugin failed")
		return
	}
	rows, pagination := paginateStatsRows(result.Rows, query)
	result.Rows = rows
	result.Limit = pagination.Limit
	writeJSON(w, http.StatusOK, newSearchResponse(query, "stats", started, func(response *SearchResponse) {
		response.SearchCommand = h.statsSearchCommandMetadata()
		response.Stats = &result
		response.Pagination = &pagination
	}))
}

func (h *Handler) executeStatsSearchPlugin(ctx context.Context, input plugin.SearchInput, stats splstats.Query) (splstats.Result, error) {
	factory, _, err := h.reg.Get(plugin.TypeSearch, "stats", "1.0.0")
	if err != nil {
		return splstats.Result{}, err
	}
	searchPlugin, ok := factory().(plugin.SearchPlugin)
	if !ok {
		return splstats.Result{}, fmt.Errorf("plugin stats is not a search plugin")
	}
	if err := searchPlugin.Init(plugin.BasicInitContext{Ctx: ctx, Code: "stats", Version: "1.0.0"}, map[string]any{}); err != nil {
		return splstats.Result{}, err
	}
	defer searchPlugin.Close()
	return searchPlugin.Execute(ctx, input, stats)
}

func (h *Handler) executeSearchCommandPlugins(ctx context.Context, requestID string, rows []map[string]any, fields []string, commands []splquery.Command) (plugin.SearchCommandResult, *SearchCommandMeta, error) {
	result := plugin.SearchCommandResult{Rows: rows, Fields: fields, OutputMode: "rows"}
	var metaOut *SearchCommandMeta
	for _, command := range commands {
		item, ok := h.enabledSearchCommandPlugin(command.Name)
		if !ok {
			return plugin.SearchCommandResult{}, nil, fmt.Errorf("search command plugin %s is not enabled", command.Name)
		}
		next, err := h.executeSearchCommandPluginRuntimeWithAudit(ctx, requestID, result, item, command)
		if err != nil {
			return plugin.SearchCommandResult{}, nil, err
		}
		if len(next.Fields) == 0 {
			next.Fields = result.Fields
		}
		result = next
		nextMeta := &SearchCommandMeta{PluginCode: item.PluginCode, PluginType: "search_command", PluginVersion: item.PluginVersion, Runtime: item.Runtime, OutputMode: firstNonEmpty(result.OutputMode, "rows")}
		if metaOut == nil || nextMeta.OutputMode == "table" {
			metaOut = nextMeta
		}
	}
	if len(result.Fields) == 0 {
		result.Fields = inferSearchTableFields(result.Rows)
	}
	if result.OutputMode == "" {
		result.OutputMode = "table"
	}
	if metaOut == nil {
		metaOut = &SearchCommandMeta{PluginCode: "table", PluginType: string(plugin.TypeSearchCommand), PluginVersion: "1.0.0", Runtime: "go_builtin", OutputMode: "table"}
	}
	if metaOut.OutputMode == "rows" {
		metaOut.OutputMode = "table"
	}
	return result, metaOut, nil
}

func (h *Handler) executeSearchCommandPluginRuntimeWithAudit(ctx context.Context, requestID string, input plugin.SearchCommandResult, item PluginImportResponse, command splquery.Command) (plugin.SearchCommandResult, error) {
	if strings.TrimSpace(item.Runtime) != "executable_search_command" {
		return executeSearchCommandPluginRuntime(ctx, input, item, command)
	}
	result, audit, err := executeExecutableSearchCommandMeasured(ctx, input, item, command)
	h.saveSearchCommandExecutionAudit(ctx, requestID, item, command, audit)
	return result, err
}

func (h *Handler) saveSearchCommandExecutionAudit(ctx context.Context, requestID string, item PluginImportResponse, command splquery.Command, audit executableSearchCommandAudit) {
	if h == nil || h.mysql == nil {
		return
	}
	runtimeConfig := audit.RuntimeConfig
	if runtimeConfig == nil {
		runtimeConfig = effectiveExecutablePluginRuntimeConfig(item.RuntimeConfig)
	}
	record := mysqlstore.SearchCommandExecutionAudit{
		RequestID:      requestID,
		PluginType:     "search_command",
		PluginCode:     item.PluginCode,
		PluginVersion:  item.PluginVersion,
		CommandName:    command.Name,
		Runtime:        item.Runtime,
		Interpreter:    textFromMap(runtimeConfig, "interpreter"),
		TimeoutMS:      intFromRuntimeConfig(runtimeConfig, "timeout_ms"),
		MaxInputRows:   intFromRuntimeConfig(runtimeConfig, "max_input_rows"),
		MaxOutputBytes: intFromRuntimeConfig(runtimeConfig, "max_output_bytes"),
		InputRows:      int64(audit.InputRows),
		OutputRows:     int64(audit.OutputRows),
		ElapsedMS:      audit.ElapsedMS,
		Success:        audit.Success,
		ErrorCode:      audit.ErrorCode,
		ErrorMessage:   audit.ErrorMessage,
		StdoutBytes:    int64(audit.StdoutBytes),
		StderrBytes:    int64(audit.StderrBytes),
	}
	if err := h.mysql.SaveSearchCommandExecutionAudit(ctx, record); err != nil {
		h.logger.Warn("save search command execution audit failed", "plugin_code", item.PluginCode, "error", err)
	}
}

func intFromRuntimeConfig(config map[string]any, key string) int {
	switch value := config[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(value))
		return parsed
	default:
		return 0
	}
}

func (h *Handler) enabledSearchCommandPlugin(code string) (PluginImportResponse, bool) {
	code = strings.TrimSpace(code)
	if code == "" {
		return PluginImportResponse{}, false
	}
	item, ok := h.findPlugin("search_command", code, "")
	return item, ok && isPluginEnabled(item.Status) && !productVisibleBuiltinPlugin("search_command", code)
}

type clickHouseStatsBackend struct {
	client *ch.Client
}

func (b clickHouseStatsBackend) Stats(ctx context.Context, query plugin.SearchStatsQuery) (splstats.Result, error) {
	if b.client == nil {
		return splstats.Result{}, fmt.Errorf("clickhouse client is required")
	}
	return b.client.Stats(ctx, ch.StatsQuery{
		Index:        query.Index,
		Keyword:      query.Keyword,
		Field:        query.Field,
		Value:        query.Value,
		FieldFilters: clickHouseFieldFiltersFromPlugin(query.FieldFilters),
		StartTime:    query.StartTime,
		EndTime:      query.EndTime,
		Limit:        query.Limit,
		Offset:       query.Offset,
		Stats:        query.Stats,
		HotFields:    clickHouseHotFields(query.HotFields),
	})
}

type memoryStatsBackend struct {
	store *memoryoutput.Store
}

func (b memoryStatsBackend) Stats(ctx context.Context, query plugin.SearchStatsQuery) (splstats.Result, error) {
	store := b.store
	if store == nil {
		store = memoryoutput.DefaultStore()
	}
	return store.Stats(memoryoutput.StatsQuery{
		Index:        query.Index,
		Keyword:      query.Keyword,
		Field:        query.Field,
		Value:        query.Value,
		FieldFilters: memoryFieldFiltersFromPlugin(query.FieldFilters),
		StartTime:    query.StartTime,
		EndTime:      query.EndTime,
		Limit:        query.Limit,
		Offset:       query.Offset,
		Stats:        query.Stats,
	}), nil
}

func searchPluginHotFields(fields []ch.HotField) []plugin.SearchHotField {
	out := make([]plugin.SearchHotField, 0, len(fields))
	for _, field := range fields {
		out = append(out, plugin.SearchHotField{Name: field.Name, Type: field.Type, Searchable: field.Searchable, Aggregatable: field.Aggregatable, Aliases: field.Aliases})
	}
	return out
}

func searchPluginFieldFilters(filters []splquery.FieldFilter) []plugin.SearchFieldFilter {
	out := make([]plugin.SearchFieldFilter, 0, len(filters))
	for _, filter := range filters {
		out = append(out, plugin.SearchFieldFilter{Field: filter.Field, Value: filter.Value})
	}
	return out
}

func clickHouseFieldFilters(filters []splquery.FieldFilter) []ch.FieldFilter {
	out := make([]ch.FieldFilter, 0, len(filters))
	for _, filter := range filters {
		out = append(out, ch.FieldFilter{Field: filter.Field, Value: filter.Value})
	}
	return out
}

func clickHouseFieldFiltersFromPlugin(filters []plugin.SearchFieldFilter) []ch.FieldFilter {
	out := make([]ch.FieldFilter, 0, len(filters))
	for _, filter := range filters {
		out = append(out, ch.FieldFilter{Field: filter.Field, Value: filter.Value})
	}
	return out
}

func memoryFieldFilters(filters []splquery.FieldFilter) []memoryoutput.FieldFilter {
	out := make([]memoryoutput.FieldFilter, 0, len(filters))
	for _, filter := range filters {
		out = append(out, memoryoutput.FieldFilter{Field: filter.Field, Value: filter.Value})
	}
	return out
}

func memoryFieldFiltersFromPlugin(filters []plugin.SearchFieldFilter) []memoryoutput.FieldFilter {
	out := make([]memoryoutput.FieldFilter, 0, len(filters))
	for _, filter := range filters {
		out = append(out, memoryoutput.FieldFilter{Field: filter.Field, Value: filter.Value})
	}
	return out
}

func clickHouseHotFields(fields []plugin.SearchHotField) []ch.HotField {
	out := make([]ch.HotField, 0, len(fields))
	for _, field := range fields {
		out = append(out, ch.HotField{Name: field.Name, Type: field.Type, Searchable: field.Searchable, Aggregatable: field.Aggregatable, Aliases: field.Aliases})
	}
	return out
}

func (h *Handler) deadletters(w http.ResponseWriter, r *http.Request) {
	events := h.deadletterEvents(r.Context())
	writeJSON(w, http.StatusOK, SearchResponse{Events: searchEventsFromEvents(events), Deadletters: deadletterRecordsFromEvents(events)})
}

type Metrics struct {
	IngestEvents        atomic.Uint64
	OutputEvents        atomic.Uint64
	PluginErrors        atomic.Uint64
	DeadletterEvents    atomic.Uint64
	PluginDurationNanos atomic.Int64
}

func (h *Handler) prometheus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte("xdp_ingest_events_total " + strconv.FormatUint(h.metrics.IngestEvents.Load(), 10) + "\n"))
	_, _ = w.Write([]byte("xdp_output_events_total " + strconv.FormatUint(h.metrics.OutputEvents.Load(), 10) + "\n"))
	_, _ = w.Write([]byte("xdp_plugin_errors_total " + strconv.FormatUint(h.metrics.PluginErrors.Load(), 10) + "\n"))
	_, _ = w.Write([]byte("xdp_deadletter_events_total " + strconv.FormatUint(h.metrics.DeadletterEvents.Load(), 10) + "\n"))
	_, _ = w.Write([]byte("xdp_plugin_duration_seconds_sum " + strconv.FormatFloat(float64(h.metrics.PluginDurationNanos.Load())/1e9, 'f', 6, 64) + "\n"))
}

var deadletterStore = memoryoutput.NewStore()

type IngestResponse struct {
	Status string       `json:"status"`
	Event  *event.Event `json:"event"`
}

type SearchQuery struct {
	Index        string
	Keyword      string
	Field        string
	Value        string
	FieldFilters []splquery.FieldFilter
	StartTime    time.Time
	EndTime      time.Time
	Limit        int
	Offset       int
	Q            string
	Earliest     string
	Latest       string
}

func (q *SearchQuery) ApplyFilters(filters splquery.Filters) {
	if filters.Index != "" {
		q.Index = filters.Index
	}
	if filters.Keyword != "" {
		q.Keyword = filters.Keyword
	}
	if filters.Field != "" {
		q.Field = filters.Field
		q.Value = filters.Value
	}
	if len(filters.FieldFilters) > 0 {
		q.FieldFilters = filters.FieldFilters
	}
	q.NormalizeFieldFilters()
}

func (q *SearchQuery) NormalizeFieldFilters() {
	if len(q.FieldFilters) == 0 && strings.TrimSpace(q.Field) != "" {
		q.FieldFilters = []splquery.FieldFilter{{Field: q.Field, Value: q.Value}}
	}
	if len(q.FieldFilters) > 0 && strings.TrimSpace(q.Field) == "" {
		q.Field = q.FieldFilters[0].Field
		q.Value = q.FieldFilters[0].Value
	}
}

func (q *SearchQuery) NormalizeIndex() error {
	if q.Index == "" {
		return nil
	}
	index, err := ch.NormalizeIndexName(q.Index)
	if err != nil {
		return err
	}
	q.Index = index
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type SearchResponse struct {
	Mode          string             `json:"mode,omitempty"`
	SPL           string             `json:"spl,omitempty"`
	Index         string             `json:"index,omitempty"`
	TimeRange     *SearchTimeRange   `json:"time_range,omitempty"`
	ElapsedMS     int64              `json:"elapsed_ms,omitempty"`
	SearchCommand *SearchCommandMeta `json:"search_command,omitempty"`
	Events        []SearchEvent      `json:"events,omitempty"`
	Stats         *splstats.Result   `json:"stats,omitempty"`
	Table         *SearchTableResult `json:"table,omitempty"`
	Pagination    *Pagination        `json:"pagination,omitempty"`
	Deadletters   []DeadletterRecord `json:"deadletters,omitempty"`
}

type SearchTableResult struct {
	Fields []string         `json:"fields"`
	Rows   []map[string]any `json:"rows"`
	Limit  int              `json:"limit"`
}

type SearchCommandMeta struct {
	PluginCode    string `json:"plugin_code"`
	PluginType    string `json:"plugin_type"`
	PluginVersion string `json:"plugin_version"`
	Runtime       string `json:"runtime"`
	OutputMode    string `json:"output_mode"`
}

func (h *Handler) statsSearchCommandMetadata() *SearchCommandMeta {
	if h != nil && h.reg != nil {
		if _, meta, err := h.reg.Get(plugin.TypeSearch, "stats", "1.0.0"); err == nil {
			return &SearchCommandMeta{
				PluginCode:    meta.Code,
				PluginType:    string(plugin.TypeSearchCommand),
				PluginVersion: meta.Version,
				Runtime:       meta.Runtime,
				OutputMode:    firstNonEmpty(meta.Labels["output_mode"], "stats"),
			}
		}
	}
	return &SearchCommandMeta{
		PluginCode:    "stats",
		PluginType:    string(plugin.TypeSearchCommand),
		PluginVersion: "1.0.0",
		Runtime:       "go_builtin",
		OutputMode:    "stats",
	}
}

type SearchEvent struct {
	EventID         string                  `json:"event_id"`
	EventTime       time.Time               `json:"event_time"`
	IngestTime      time.Time               `json:"ingest_time"`
	PipelineID      string                  `json:"pipeline_id,omitempty"`
	PipelineVersion string                  `json:"pipeline_version,omitempty"`
	Source          event.Source            `json:"source"`
	Metadata        map[string]any          `json:"metadata"`
	Raw             string                  `json:"raw"`
	Fields          map[string]any          `json:"fields"`
	Labels          map[string]string       `json:"labels,omitempty"`
	Tags            []string                `json:"tags,omitempty"`
	Errors          []event.ProcessingError `json:"errors,omitempty"`
	Display         SearchEventDisplay      `json:"display"`
	Detail          SearchEventDetail       `json:"detail"`
}

type SearchEventDisplay struct {
	Time       string `json:"time"`
	Event      string `json:"event"`
	Expandable bool   `json:"expandable"`
}

type SearchEventDetail struct {
	Raw       string           `json:"raw"`
	FieldRows []SearchFieldRow `json:"field_rows"`
}

type SearchFieldRow struct {
	Category string `json:"category"`
	Name     string `json:"name"`
	Value    any    `json:"value"`
	Type     string `json:"type"`
}

type SearchTimeRange struct {
	Earliest  string `json:"earliest,omitempty"`
	Latest    string `json:"latest,omitempty"`
	StartTime string `json:"start_time,omitempty"`
	EndTime   string `json:"end_time,omitempty"`
	Start     string `json:"start,omitempty"`
	End       string `json:"end,omitempty"`
}

func newSearchResponse(query SearchQuery, mode string, started time.Time, apply func(*SearchResponse)) SearchResponse {
	response := SearchResponse{
		Mode:      mode,
		SPL:       effectiveSearchSPL(query),
		Index:     query.Index,
		TimeRange: searchTimeRange(query),
		ElapsedMS: time.Since(started).Milliseconds(),
	}
	if apply != nil {
		apply(&response)
	}
	return response
}

func searchEventsFromEvents(events []*event.Event) []SearchEvent {
	out := make([]SearchEvent, 0, len(events))
	for _, item := range events {
		if item == nil {
			continue
		}
		out = append(out, searchEventFromEvent(item))
	}
	return out
}

func eventRowsFromEvents(events []*event.Event) []map[string]any {
	rows := make([]map[string]any, 0, len(events))
	for _, item := range events {
		if item == nil {
			continue
		}
		metadata := copyMetadata(item.Metadata)
		index := firstNonEmpty(metadataText(metadata, "index"), "app")
		source := firstNonEmpty(item.Source.Name, metadataText(metadata, "source_name"))
		sourcetype := firstNonEmpty(metadataText(metadata, "sourcetype"), metadataText(metadata, "parse_rule_name"))
		row := copyFields(item.Fields)
		applyDefaultHotFieldAliases(row)
		row["_time"] = item.EventTime
		row["time"] = formatSearchTime(item.EventTime)
		row["event_time"] = item.EventTime
		row["ingest_time"] = item.IngestTime
		row["raw"] = item.Raw
		row["_raw"] = item.Raw
		row["event_id"] = item.EventID
		row["index"] = index
		row["source"] = source
		row["source_name"] = source
		row["sourcetype"] = sourcetype
		row["parse_status"] = firstNonEmpty(metadataText(metadata, "parse_status"), "unparsed")
		row["parse_rule_id"] = firstNonEmpty(metadataText(metadata, "parse_rule_id"), "")
		row["parse_rule_name"] = firstNonEmpty(metadataText(metadata, "parse_rule_name"), sourcetype)
		row["parse_error"] = firstNonEmpty(metadataText(metadata, "parse_error"), "")
		rows = append(rows, row)
	}
	return rows
}

func applyDefaultHotFieldAliases(row map[string]any) {
	if row == nil {
		return
	}
	defaultAliases := map[string]string{
		"src_ip": "src",
		"dst_ip": "dst",
	}
	for source, alias := range defaultAliases {
		value, ok := row[source]
		if !ok {
			continue
		}
		if _, exists := row[alias]; exists {
			continue
		}
		row[alias] = value
	}
}

func inferSearchTableFields(rows []map[string]any) []string {
	if len(rows) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(rows[0]))
	for key := range rows[0] {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func searchEventFromEvent(item *event.Event) SearchEvent {
	metadata := copyMetadata(item.Metadata)
	index := firstNonEmpty(metadataText(metadata, "index"), "app")
	source := firstNonEmpty(item.Source.Name, metadataText(metadata, "source_name"))
	sourcetype := firstNonEmpty(metadataText(metadata, "sourcetype"), metadataText(metadata, "parse_rule_name"))
	metadata["index"] = index
	metadata["source_name"] = source
	metadata["source"] = source
	metadata["sourcetype"] = sourcetype
	metadata["parse_status"] = firstNonEmpty(metadataText(metadata, "parse_status"), "unparsed")
	metadata["parse_rule_id"] = firstNonEmpty(metadataText(metadata, "parse_rule_id"), "")
	metadata["parse_rule_name"] = firstNonEmpty(metadataText(metadata, "parse_rule_name"), sourcetype)
	metadata["parse_error"] = firstNonEmpty(metadataText(metadata, "parse_error"), "")
	fields := copyFields(item.Fields)
	return SearchEvent{
		EventID:         item.EventID,
		EventTime:       item.EventTime,
		IngestTime:      item.IngestTime,
		PipelineID:      item.PipelineID,
		PipelineVersion: item.PipelineVersion,
		Source:          item.Source,
		Metadata:        metadata,
		Raw:             item.Raw,
		Fields:          fields,
		Labels:          item.Labels,
		Tags:            item.Tags,
		Errors:          item.Errors,
		Display: SearchEventDisplay{
			Time:       formatSearchTime(item.EventTime),
			Event:      item.Raw,
			Expandable: true,
		},
		Detail: SearchEventDetail{
			Raw:       item.Raw,
			FieldRows: searchFieldRows(index, source, sourcetype, metadata, fields, item),
		},
	}
}

func metadataText(metadata map[string]any, key string) string {
	value := metadata[key]
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func copyMetadata(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func copyFields(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func searchFieldRows(index, source, sourcetype string, metadata map[string]any, fields map[string]any, item *event.Event) []SearchFieldRow {
	rows := []SearchFieldRow{
		fieldRow("metadata", "index", index),
		fieldRow("metadata", "source", source),
		fieldRow("metadata", "sourcetype", sourcetype),
		fieldRow("metadata", "parse_status", metadata["parse_status"]),
		fieldRow("metadata", "parse_rule_id", metadata["parse_rule_id"]),
		fieldRow("metadata", "parse_rule_name", metadata["parse_rule_name"]),
		fieldRow("metadata", "parse_error", metadata["parse_error"]),
	}
	if parsedAt := strings.TrimSpace(fmt.Sprint(metadata["parsed_at"])); parsedAt != "" && parsedAt != "<nil>" {
		rows = append(rows, fieldRow("metadata", "parsed_at", parsedAt))
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		rows = append(rows, fieldRow("field", key, fields[key]))
	}
	rows = append(rows, fieldRow("system", "event_id", item.EventID))
	rows = append(rows, fieldRow("system", "event_time", formatSearchTime(item.EventTime)))
	rows = append(rows, fieldRow("system", "ingest_time", formatSearchTime(item.IngestTime)))
	return rows
}

func fieldRow(category, name string, value any) SearchFieldRow {
	return SearchFieldRow{Category: category, Name: name, Value: value, Type: fieldValueType(value)}
}

func fieldValueType(value any) string {
	switch value.(type) {
	case bool:
		return "bool"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return "number"
	case time.Time:
		return "datetime"
	case map[string]any, []any, map[string]string, []string:
		return "json"
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if _, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return "datetime"
		}
		return "string"
	}
}

func effectiveSearchSPL(query SearchQuery) string {
	if strings.TrimSpace(query.Q) != "" {
		return strings.TrimSpace(query.Q)
	}
	parts := []string{}
	if query.Index != "" {
		parts = append(parts, "index="+query.Index)
	}
	if query.Field != "" {
		parts = append(parts, query.Field+"="+query.Value)
	}
	if query.Keyword != "" {
		parts = append(parts, query.Keyword)
	}
	return strings.Join(parts, " ")
}

func searchParseErrorCode(input string, err error) string {
	if err != nil && searchLooksLikeSearchCommand(input) {
		return "SPL_COMMAND_VALIDATION_ERROR"
	}
	if err == nil || !searchLooksLikeStatsCommand(input) {
		return "SPL_PARSE_ERROR"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "unsupported stats function"):
		return "SPL_STATS_UNSUPPORTED_FUNCTION"
	case strings.Contains(message, "requires a field"):
		return "SPL_STATS_FIELD_REQUIRED"
	case strings.Contains(message, "invalid alias"):
		return "SPL_STATS_ALIAS_INVALID"
	case strings.Contains(message, "too many group by fields"):
		return "SPL_STATS_GROUP_LIMIT_EXCEEDED"
	default:
		return "SPL_STATS_PARSE_ERROR"
	}
}

func searchLooksLikeStatsCommand(input string) bool {
	raw := strings.TrimSpace(strings.ToLower(input))
	if strings.HasPrefix(raw, "stats ") || strings.HasPrefix(raw, "| stats ") {
		return true
	}
	_, command, ok := strings.Cut(raw, "|")
	return ok && strings.HasPrefix(strings.TrimSpace(command), "stats ")
}

func searchLooksLikeSearchCommand(input string) bool {
	raw := strings.ToLower(input)
	for _, part := range strings.Split(raw, "|") {
		command := strings.TrimSpace(part)
		for _, name := range []string{"table ", "sort ", "head ", "dedup "} {
			if strings.HasPrefix(command, name) || command == strings.TrimSpace(name) {
				return true
			}
		}
	}
	return false
}

func searchTimeRange(query SearchQuery) *SearchTimeRange {
	if query.StartTime.IsZero() && query.EndTime.IsZero() {
		return nil
	}
	out := &SearchTimeRange{Earliest: query.Earliest, Latest: query.Latest}
	if !query.StartTime.IsZero() {
		out.StartTime = formatSearchTime(query.StartTime)
		out.Start = out.StartTime
	}
	if !query.EndTime.IsZero() {
		out.EndTime = formatSearchTime(query.EndTime)
		out.End = out.EndTime
	}
	return out
}

func searchTimeBoundsFromRequest(r *http.Request) (time.Time, time.Time, string, string, error) {
	earliest := strings.TrimSpace(r.URL.Query().Get("earliest"))
	latest := strings.TrimSpace(r.URL.Query().Get("latest"))
	startValue := firstNonEmpty(r.URL.Query().Get("start_time"), r.URL.Query().Get("from"), r.URL.Query().Get("start"))
	endValue := firstNonEmpty(r.URL.Query().Get("end_time"), r.URL.Query().Get("to"), r.URL.Query().Get("end"))
	startTime, err := parseSearchBoundary(startValue, earliest)
	if err != nil {
		return time.Time{}, time.Time{}, earliest, latest, fmt.Errorf("invalid start_time")
	}
	endTime, err := parseSearchBoundary(endValue, latest)
	if err != nil {
		return time.Time{}, time.Time{}, earliest, latest, fmt.Errorf("invalid end_time")
	}
	return startTime, endTime, earliest, latest, nil
}

func parseSearchBoundary(absolute string, relative string) (time.Time, error) {
	if strings.TrimSpace(absolute) != "" {
		return eventtime.ParseOptional(absolute)
	}
	relative = strings.TrimSpace(relative)
	if relative == "" {
		return time.Time{}, nil
	}
	return parseSearchTimeExpression(relative)
}

func parseSearchTimeExpression(value string) (time.Time, error) {
	now := time.Now().In(searchLocation)
	text := strings.ToLower(strings.TrimSpace(value))
	switch text {
	case "now":
		return now, nil
	case "@d":
		return startOfSearchDay(now), nil
	}
	if strings.HasPrefix(text, "-") {
		rounded := strings.HasSuffix(text, "@d")
		if rounded {
			text = strings.TrimSuffix(text, "@d")
		}
		duration, err := parseRelativeDuration(text)
		if err != nil {
			return time.Time{}, err
		}
		target := now.Add(-duration)
		if rounded {
			target = startOfSearchDay(target)
		}
		return target, nil
	}
	return eventtime.ParseOptional(value)
}

func parseRelativeDuration(value string) (time.Duration, error) {
	if len(value) < 3 || value[0] != '-' {
		return 0, fmt.Errorf("invalid relative time")
	}
	amount, err := strconv.Atoi(value[1 : len(value)-1])
	if err != nil || amount < 0 {
		return 0, fmt.Errorf("invalid relative time")
	}
	switch value[len(value)-1] {
	case 'm':
		return time.Duration(amount) * time.Minute, nil
	case 'h':
		return time.Duration(amount) * time.Hour, nil
	case 'd':
		return time.Duration(amount) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid relative time")
	}
}

func startOfSearchDay(value time.Time) time.Time {
	year, month, day := value.In(searchLocation).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, searchLocation)
}

func formatSearchTime(value time.Time) string {
	return value.In(searchLocation).Format(time.RFC3339Nano)
}

func mustLoadSearchLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return location
}

func defaultPipeline() pipeline.Pipeline {
	return pipeline.SyslogCollectionPipeline()
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func contextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeErrorCode(w, status, defaultErrorCode(status), message)
}

func writeErrorCode(w http.ResponseWriter, status int, code string, message string) {
	writeErrorCodeWithRequestID(w, status, code, message, newRequestID())
}

func writeErrorCodeWithRequestID(w http.ResponseWriter, status int, code string, message string, requestID string) {
	if strings.TrimSpace(requestID) == "" {
		requestID = newRequestID()
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
		"request_id": requestID,
	})
}

func writeSearchCommandPluginError(w http.ResponseWriter, requestID string, err error) {
	code, ok := searchCommandPluginExecutionErrorCode(err)
	if !ok {
		writeErrorCodeWithRequestID(w, http.StatusBadRequest, "SPL_COMMAND_VALIDATION_ERROR", err.Error(), requestID)
		return
	}
	status := http.StatusBadRequest
	if code == pluginExecTimeoutCode {
		status = http.StatusGatewayTimeout
	}
	if code == pluginExecRuntimeNotReadyCode {
		status = http.StatusInternalServerError
	}
	writeErrorCodeWithRequestID(w, status, code, err.Error(), requestID)
}

func defaultErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "INVALID_REQUEST"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	case http.StatusMethodNotAllowed:
		return "METHOD_NOT_ALLOWED"
	case http.StatusBadGateway:
		return "BAD_GATEWAY"
	default:
		return "ERROR"
	}
}

func newRequestID() string {
	return fmt.Sprintf("req_%d_%d", time.Now().UnixNano(), requestSeq.Add(1))
}
