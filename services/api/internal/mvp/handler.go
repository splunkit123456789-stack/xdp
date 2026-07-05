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
	httpinput "xdp/plugins/input/http"
	sysloginput "xdp/plugins/input/syslog"
	clickhouseoutput "xdp/plugins/output/clickhouse"
	kafkaoutput "xdp/plugins/output/kafka"
	memoryoutput "xdp/plugins/output/memory"
	s3output "xdp/plugins/output/s3"
	jsonparser "xdp/plugins/parser/json"
	propsconfparser "xdp/plugins/parser/propsconf"
	regexparser "xdp/plugins/parser/regex"
	indexrouter "xdp/plugins/router/indexrouter"
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
	runtimePipelines []pipeline.Pipeline
}

var requestSeq atomic.Uint64

func NewHandler(logger *slog.Logger) http.Handler {
	reg := plugin.NewRegistry()
	must(httpinput.Register(reg))
	must(sysloginput.Register(reg))
	must(jsonparser.Register(reg))
	must(propsconfparser.Register(reg))
	must(regexparser.Register(reg))
	must(fieldmapping.Register(reg))
	must(typeconvert.Register(reg))
	must(indexrouter.Register(reg))
	must(geoip.Register(reg))
	must(kafkaoutput.Register(reg))
	must(memoryoutput.Register(reg))
	must(clickhouseoutput.Register(reg))
	must(s3output.Register(reg))

	pipe := pipeline.MVPJSONPipeline()
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
					_ = client.SeedPlugins(ctx, reg.List(""))
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
		logger:        logger,
		mux:           http.NewServeMux(),
		reg:           reg,
		runtime:       xdpruntime.NewExecutor(reg),
		pipeline:      pipe,
		clickhouse:    ch.New(ch.Config{Endpoint: env("XDP_CLICKHOUSE_ENDPOINT", "http://127.0.0.1:8123"), Database: env("XDP_CLICKHOUSE_DATABASE", "xdp"), Username: env("XDP_CLICKHOUSE_USERNAME", ""), Password: env("XDP_CLICKHOUSE_PASSWORD", "")}),
		mysql:         mysqlClient,
		metrics:       &Metrics{},
		auth:          auth,
		dataSources:   defaultDataSources(),
		indexConfigs:  defaultIndexConfigs(),
		parseRules:    defaultParseRules(),
		savedSearches: defaultSavedSearches(),
	}
	h.runtimePipelines = h.buildRuntimePipelines()
	if h.mysql != nil {
		ctx, cancel := contextWithTimeout()
		h.seedConfigStore(ctx)
		h.loadConfigStore(ctx)
		cancel()
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
	h.mux.HandleFunc("GET /api/v1/auth", h.authStatus)
	h.mux.HandleFunc("POST /api/v1/login", h.login)
	h.mux.HandleFunc("GET /api/v1/plugins", h.listPlugins)
	h.mux.HandleFunc("GET /api/v1/input-plugins", h.listInputPlugins)
	h.mux.HandleFunc("GET /api/v1/parser-plugins", h.listParserPlugins)
	h.mux.HandleFunc("GET /api/v1/pipelines", h.listPipelines)
	h.mux.HandleFunc("GET /api/v1/runtime/pipelines", h.listRuntimePipelines)
	h.mux.HandleFunc("POST /api/v1/pipelines", h.savePipeline)
	h.mux.HandleFunc("GET /api/v1/indexes", h.listIndexes)
	h.mux.HandleFunc("POST /api/v1/indexes", h.saveIndex)
	h.mux.HandleFunc("DELETE /api/v1/indexes", h.deleteIndex)
	h.mux.HandleFunc("GET /api/v1/datasources", h.listDataSources)
	h.mux.HandleFunc("POST /api/v1/datasources/port-check", h.checkDataSourcePort)
	h.mux.HandleFunc("POST /api/v1/datasources", h.saveDataSource)
	h.mux.HandleFunc("GET /api/v1/datasources/{id}", h.getDataSource)
	h.mux.HandleFunc("PUT /api/v1/datasources/{id}", h.updateDataSource)
	h.mux.HandleFunc("PATCH /api/v1/datasources/{id}/status", h.updateDataSourceStatus)
	h.mux.HandleFunc("GET /api/v1/datasources/{id}/runtime", h.getDataSourceRuntime)
	h.mux.HandleFunc("DELETE /api/v1/datasources/{id}", h.deleteDataSource)
	h.mux.HandleFunc("GET /api/v1/parse-rules", h.listParseRules)
	h.mux.HandleFunc("POST /api/v1/parse-rules", h.createParseRule)
	h.mux.HandleFunc("GET /api/v1/parse-rules/{id}", h.getParseRule)
	h.mux.HandleFunc("PUT /api/v1/parse-rules/{id}", h.updateParseRule)
	h.mux.HandleFunc("PATCH /api/v1/parse-rules/{id}/status", h.updateParseRuleStatus)
	h.mux.HandleFunc("DELETE /api/v1/parse-rules/{id}", h.deleteParseRule)
	h.mux.HandleFunc("POST /api/v1/parse-rules/{id}/test", h.testParseRule)
	h.mux.HandleFunc("POST /api/v1/ingest/json", h.ingestJSON)
	h.mux.HandleFunc("GET /api/v1/search", h.search)
	h.mux.HandleFunc("GET /api/v1/search/fields", h.searchFields)
	h.mux.HandleFunc("GET /api/v1/search/timeline", h.searchTimeline)
	h.mux.HandleFunc("GET /api/v1/search/favorites", h.listSavedSearches)
	h.mux.HandleFunc("POST /api/v1/search/favorites", h.createSavedSearch)
	h.mux.HandleFunc("DELETE /api/v1/search/favorites/{id}", h.deleteSavedSearch)
	h.mux.HandleFunc("GET /api/v1/deadletters", h.deadletters)
	h.mux.HandleFunc("POST /api/v1/deadletters/retry", h.retryDeadletter)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *Handler) listPlugins(w http.ResponseWriter, r *http.Request) {
	if h.mysql != nil {
		if items, err := h.mysql.ListPlugins(r.Context()); err == nil {
			writeJSON(w, http.StatusOK, items)
			return
		}
	}
	writeJSON(w, http.StatusOK, h.reg.List(""))
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

func (h *Handler) ingestJSON(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json request")
		return
	}
	if strings.TrimSpace(req.Raw) == "" {
		writeError(w, http.StatusBadRequest, "raw is required")
		return
	}
	now := time.Now().UTC()
	eventTime := now
	source := h.dataSource("http-json")
	explicitTimeField := req.TimeFieldName() != ""
	timeField := req.TimeFieldName()
	if timeField == "" {
		timeField = source.TimeField
	}
	if timeField != "" {
		parsed, err := eventtime.FromRaw(req.Raw, timeField)
		if err != nil {
			if explicitTimeField {
				writeError(w, http.StatusBadRequest, "invalid time_field")
				return
			}
			timeField = ""
		} else {
			eventTime = parsed
		}
	}
	e := event.New(req.Raw, event.Source{Type: "http", Name: "mvp-json"}, now)
	e.EventTime = eventTime
	e.Metadata["index"] = source.DefaultIndex
	if timeField != "" {
		e.Metadata["time_field"] = timeField
	}

	start := time.Now()
	pipe := h.pipelineForSource("http-json")
	result, err := h.runtime.Execute(r.Context(), pipe, e)
	h.metrics.PluginDurationNanos.Add(time.Since(start).Nanoseconds())
	h.metrics.IngestEvents.Add(1)
	if err != nil {
		h.metrics.PluginErrors.Add(1)
		h.metrics.DeadletterEvents.Add(1)
		deadletterStore.Append(result.Event)
		if h.mysql != nil {
			_ = h.mysql.SaveDeadletter(r.Context(), result.Event)
		}
		writeJSON(w, http.StatusAccepted, IngestResponse{Event: result.Event, Status: result.Status})
		return
	}
	h.metrics.OutputEvents.Add(1)
	writeJSON(w, http.StatusCreated, IngestResponse{Event: result.Event, Status: "indexed"})
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	limit := ch.ParseLimit(r.URL.Query().Get("limit"), 20)
	page := ch.ParseLimit(r.URL.Query().Get("page"), 1)
	offset := parseNonNegative(r.URL.Query().Get("offset"), (page-1)*limit)
	startTime, endTime, earliest, latest, err := searchTimeBoundsFromRequest(r)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	query := SearchQuery{Index: r.URL.Query().Get("index"), Keyword: r.URL.Query().Get("keyword"), Field: r.URL.Query().Get("field"), Value: r.URL.Query().Get("value"), StartTime: startTime, EndTime: endTime, Limit: limit, Offset: offset, Q: r.URL.Query().Get("q"), Earliest: earliest, Latest: latest}
	if strings.TrimSpace(query.Q) != "" {
		parsed, err := splquery.Parse(query.Q)
		if err != nil {
			writeErrorCode(w, http.StatusBadRequest, "SPL_PARSE_ERROR", "invalid search query")
			return
		}
		query.ApplyFilters(parsed.Filters)
		if err := query.NormalizeIndex(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid index")
			return
		}
		if parsed.Stats != nil {
			h.searchStats(w, r, query, *parsed.Stats, started)
			return
		}
	}
	if err := query.NormalizeIndex(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid index")
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

func (h *Handler) searchStats(w http.ResponseWriter, r *http.Request, query SearchQuery, stats splstats.Query, started time.Time) {
	fetchQuery := query
	fetchQuery.Limit = 1000
	fetchQuery.Offset = 0
	if os.Getenv("XDP_OUTPUT") == "clickhouse" {
		result, err := h.clickhouse.Stats(r.Context(), ch.StatsQuery{Index: fetchQuery.Index, Keyword: fetchQuery.Keyword, Field: fetchQuery.Field, Value: fetchQuery.Value, StartTime: fetchQuery.StartTime, EndTime: fetchQuery.EndTime, Limit: fetchQuery.Limit, Offset: fetchQuery.Offset, Stats: stats, HotFields: h.hotFieldsForIndex(fetchQuery.Index)})
		if err != nil {
			h.logger.Warn("clickhouse stats failed", "error", err)
			writeError(w, http.StatusBadGateway, "stats query failed")
			return
		}
		rows, pagination := paginateStatsRows(result.Rows, query)
		result.Rows = rows
		result.Limit = pagination.Limit
		writeJSON(w, http.StatusOK, newSearchResponse(query, "stats", started, func(response *SearchResponse) {
			response.Stats = &result
			response.Pagination = &pagination
		}))
		return
	}
	result := memoryoutput.DefaultStore().Stats(memoryoutput.StatsQuery{Index: fetchQuery.Index, Keyword: fetchQuery.Keyword, Field: fetchQuery.Field, Value: fetchQuery.Value, StartTime: fetchQuery.StartTime, EndTime: fetchQuery.EndTime, Limit: fetchQuery.Limit, Offset: fetchQuery.Offset, Stats: stats})
	rows, pagination := paginateStatsRows(result.Rows, query)
	result.Rows = rows
	result.Limit = pagination.Limit
	writeJSON(w, http.StatusOK, newSearchResponse(query, "stats", started, func(response *SearchResponse) {
		response.Stats = &result
		response.Pagination = &pagination
	}))
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

type IngestRequest struct {
	Raw            string `json:"raw"`
	TimeField      string `json:"time_field,omitempty"`
	EventTimeField string `json:"event_time_field,omitempty"`
}

func (r IngestRequest) TimeFieldName() string {
	if strings.TrimSpace(r.TimeField) != "" {
		return strings.TrimSpace(r.TimeField)
	}
	return strings.TrimSpace(r.EventTimeField)
}

type IngestResponse struct {
	Status string       `json:"status"`
	Event  *event.Event `json:"event"`
}

type SearchQuery struct {
	Index     string
	Keyword   string
	Field     string
	Value     string
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	Offset    int
	Q         string
	Earliest  string
	Latest    string
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
	Mode        string             `json:"mode,omitempty"`
	SPL         string             `json:"spl,omitempty"`
	Index       string             `json:"index,omitempty"`
	TimeRange   *SearchTimeRange   `json:"time_range,omitempty"`
	ElapsedMS   int64              `json:"elapsed_ms,omitempty"`
	Events      []SearchEvent      `json:"events,omitempty"`
	Stats       *splstats.Result   `json:"stats,omitempty"`
	Pagination  *Pagination        `json:"pagination,omitempty"`
	Deadletters []DeadletterRecord `json:"deadletters,omitempty"`
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
	return pipeline.Pipeline{
		APIVersion: pipeline.APIVersionV1Alpha1,
		Kind:       pipeline.KindPipeline,
		Metadata:   pipeline.Metadata{ID: "mvp-json-pipeline", Name: "MVP JSON Pipeline"},
		Spec: pipeline.Spec{
			Source:  pipeline.SourceSpec{ID: "http-json", Type: "input", Plugin: "http-input", Version: "1.0.0"},
			Stages:  []pipeline.StageSpec{{ID: "parse-json", Type: string(plugin.TypeParser), Plugin: "json-parser", Version: "1.0.0", Config: map[string]any{"source": "raw", "target": "fields"}, OnError: &pipeline.ErrorPolicy{Action: "dead_letter"}}},
			Outputs: []pipeline.OutputSpec{{ID: "memory-search", Plugin: "memory-output", Version: "1.0.0", Config: map[string]any{"index": "app"}}},
		},
	}
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
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
		"request_id": newRequestID(),
	})
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
