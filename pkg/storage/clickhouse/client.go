package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"xdp/pkg/event"
	"xdp/pkg/search/splstats"
)

type Client struct {
	Endpoint string
	Database string
	Username string
	Password string
	HTTP     *http.Client
}

type Config struct {
	Endpoint string
	Database string
	Username string
	Password string
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
	HotFields []HotField
}

type StatsQuery struct {
	Index     string
	Keyword   string
	Field     string
	Value     string
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	Offset    int
	Stats     splstats.Query
	HotFields []HotField
}

type TimelineQuery struct {
	Index     string
	Keyword   string
	Field     string
	Value     string
	StartTime time.Time
	EndTime   time.Time
	Interval  string
	HotFields []HotField
}

type StatsResult = splstats.Result

type TimelineBucket struct {
	Start time.Time
	Count int
}

type HotField struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Searchable   bool     `json:"searchable"`
	Aggregatable bool     `json:"aggregatable"`
	Aliases      []string `json:"aliases,omitempty"`
}

type IndexInfo struct {
	IndexName       string    `json:"index_name"`
	TableName       string    `json:"table_name"`
	Rows            uint64    `json:"rows"`
	LatestEventTime string    `json:"latest_event_time,omitempty"`
	StorageBytes    uint64    `json:"storage_bytes"`
	TTLDays         int       `json:"ttl_days"`
	UpdatedAt       time.Time `json:"updated_at"`
}

const (
	defaultIndexName        = "app"
	SystemUnparsedIndexName = "_unparsed"
	tablePrefix             = "events_"
	storageTimezone         = "Asia/Shanghai"
)

var storageLocation = mustLoadLocation(storageTimezone)

func New(cfg Config) *Client {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "http://127.0.0.1:8123"
	}
	database := cfg.Database
	if database == "" {
		database = "xdp"
	}
	return &Client{Endpoint: strings.TrimRight(endpoint, "/"), Database: database, Username: cfg.Username, Password: cfg.Password, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.exec(ctx, "SELECT 1")
	return err
}

func (c *Client) InsertEvents(ctx context.Context, events []*event.Event) error {
	if len(events) == 0 {
		return nil
	}

	grouped := map[string][]*event.Event{}
	for _, e := range events {
		index, err := NormalizeIndexName(stringValue(e.Metadata["index"], defaultIndexName))
		if err != nil {
			return err
		}
		e.Metadata["index"] = index
		grouped[index] = append(grouped[index], e)
	}

	for index, items := range grouped {
		if err := c.EnsureIndexTable(ctx, index); err != nil {
			return err
		}
		_, table, err := c.indexTableName(index)
		if err != nil {
			return err
		}
		var body strings.Builder
		body.WriteString("INSERT INTO ")
		body.WriteString(table)
		body.WriteString(" FORMAT JSONEachRow\n")
		for _, e := range items {
			row, err := eventToRow(e)
			if err != nil {
				return err
			}
			encoded, err := json.Marshal(row)
			if err != nil {
				return err
			}
			body.Write(encoded)
			body.WriteByte('\n')
		}
		if _, err := c.exec(ctx, body.String()); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Search(ctx context.Context, query SearchQuery) ([]*event.Event, error) {
	limit := query.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if err := c.EnsureIndexTable(ctx, query.Index); err != nil {
		return nil, err
	}
	_, table, err := c.indexTableName(query.Index)
	if err != nil {
		return nil, err
	}
	conditions := buildWhereConditions(query.Keyword, query.Field, query.Value, query.StartTime, query.EndTime, query.HotFields)
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	stmt := fmt.Sprintf(`SELECT index_name, event_id, event_time, ingest_time, pipeline_id, pipeline_version, source_type, source_name, source_host, source_ip, sourcetype, parse_status, parse_rule_id, parse_rule_name, parse_error, parsed_at, raw, fields_json, labels_json, tags, errors_json FROM %s WHERE %s ORDER BY event_time DESC, event_id DESC LIMIT %d OFFSET %d FORMAT JSONEachRow`, table, strings.Join(conditions, " AND "), limit, offset)
	data, err := c.exec(ctx, stmt)
	if err != nil {
		return nil, err
	}
	items := []*event.Event{}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row eventRow
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, err
		}
		items = append(items, row.toEvent())
	}
	return items, nil
}

func (c *Client) Count(ctx context.Context, query SearchQuery) (int, error) {
	if err := c.EnsureIndexTable(ctx, query.Index); err != nil {
		return 0, err
	}
	_, table, err := c.indexTableName(query.Index)
	if err != nil {
		return 0, err
	}
	conditions := buildWhereConditions(query.Keyword, query.Field, query.Value, query.StartTime, query.EndTime, query.HotFields)
	stmt := fmt.Sprintf("SELECT count() AS total FROM %s WHERE %s FORMAT JSONEachRow", table, strings.Join(conditions, " AND "))
	data, err := c.exec(ctx, stmt)
	if err != nil {
		return 0, err
	}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row struct {
			Total any `json:"total"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			return 0, err
		}
		return int(uint64FromAny(row.Total)), nil
	}
	return 0, nil
}

func (c *Client) Stats(ctx context.Context, query StatsQuery) (StatsResult, error) {
	if err := c.EnsureIndexTable(ctx, query.Index); err != nil {
		return StatsResult{}, err
	}
	stmt, fields, err := buildStatsSQL(c.Database, query)
	if err != nil {
		return StatsResult{}, err
	}
	data, err := c.exec(ctx, stmt)
	if err != nil {
		return StatsResult{}, err
	}
	rows := []map[string]any{}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			return StatsResult{}, err
		}
		item := map[string]any{}
		for i, group := range query.Stats.GroupBy {
			item[group.DisplayName()] = row[fmt.Sprintf("g%d", i)]
		}
		for i, agg := range query.Stats.Aggregates {
			item[agg.DisplayName()] = row[fmt.Sprintf("a%d", i)]
		}
		rows = append(rows, item)
	}
	return StatsResult{Query: query.Stats.Raw, Fields: fields, Rows: rows, Limit: normalizedLimit(query.Limit)}, nil
}

func (c *Client) Timeline(ctx context.Context, query TimelineQuery) ([]TimelineBucket, error) {
	if err := c.EnsureIndexTable(ctx, query.Index); err != nil {
		return nil, err
	}
	stmt, err := buildTimelineSQL(c.Database, query)
	if err != nil {
		return nil, err
	}
	data, err := c.exec(ctx, stmt)
	if err != nil {
		return nil, err
	}
	buckets := []TimelineBucket{}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row struct {
			Bucket string `json:"bucket"`
			Count  any    `json:"count"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, err
		}
		start, err := parseStorageTime(row.Bucket)
		if err != nil {
			return nil, err
		}
		buckets = append(buckets, TimelineBucket{Start: start, Count: int(uint64FromAny(row.Count))})
	}
	return buckets, nil
}

func (c *Client) ListIndexes(ctx context.Context) ([]IndexInfo, error) {
	db, err := safeIdentifier(c.Database, "database")
	if err != nil {
		return nil, err
	}
	stmt := "SELECT name FROM system.tables WHERE database = " + quote(c.Database) + " AND startsWith(name, " + quote(tablePrefix) + ") ORDER BY name FORMAT JSONEachRow"
	data, err := c.exec(ctx, stmt)
	if err != nil {
		return nil, err
	}
	items := []IndexInfo{}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, err
		}
		index := strings.TrimPrefix(row.Name, tablePrefix)
		info := IndexInfo{IndexName: index, TableName: row.Name, TTLDays: 30, UpdatedAt: time.Now().UTC()}
		metrics, err := c.indexMetrics(ctx, db+"."+row.Name)
		if err != nil {
			return nil, err
		}
		info.Rows = metrics.Rows
		info.LatestEventTime = metrics.LatestEventTime
		info.StorageBytes = metrics.StorageBytes
		items = append(items, info)
	}
	return items, nil
}

func (c *Client) indexMetrics(ctx context.Context, table string) (IndexInfo, error) {
	stmt := fmt.Sprintf("SELECT count() AS rows, ifNull(toString(max(event_time)), '') AS latest_event_time FROM %s FORMAT JSONEachRow", table)
	data, err := c.exec(ctx, stmt)
	if err != nil {
		return IndexInfo{}, err
	}
	var metrics IndexInfo
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row struct {
			Rows            any    `json:"rows"`
			LatestEventTime string `json:"latest_event_time"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			return IndexInfo{}, err
		}
		metrics.Rows = uint64FromAny(row.Rows)
		metrics.LatestEventTime = row.LatestEventTime
	}
	partsStmt := "SELECT ifNull(sum(bytes_on_disk), 0) AS storage_bytes FROM system.parts WHERE active AND database = " + quote(c.Database) + " AND table = " + quote(strings.TrimPrefix(table, c.Database+".")) + " FORMAT JSONEachRow"
	partsData, err := c.exec(ctx, partsStmt)
	if err != nil {
		return IndexInfo{}, err
	}
	for _, line := range bytes.Split(bytes.TrimSpace(partsData), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row struct {
			StorageBytes any `json:"storage_bytes"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			return IndexInfo{}, err
		}
		metrics.StorageBytes = uint64FromAny(row.StorageBytes)
	}
	return metrics, nil
}

func (c *Client) EnsureIndexTable(ctx context.Context, index string) error {
	_, table, err := c.indexTableName(index)
	if err != nil {
		return err
	}
	_, err = c.exec(ctx, indexTableDDL(table))
	return err
}

func (c *Client) EnsureHotFields(ctx context.Context, index string, fields []HotField) error {
	normalized, err := NormalizeHotFields(fields)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return nil
	}
	if err := c.EnsureIndexTable(ctx, index); err != nil {
		return err
	}
	_, table, err := c.indexTableName(index)
	if err != nil {
		return err
	}
	for _, stmt := range hotFieldStatements(table, normalized) {
		if _, err := c.exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) DropIndexTable(ctx context.Context, index string) error {
	_, table, err := c.indexTableName(index)
	if err != nil {
		return err
	}
	_, err = c.exec(ctx, "DROP TABLE IF EXISTS "+table)
	return err
}

func (c *Client) indexTableName(index string) (string, string, error) {
	return indexTableName(c.Database, index)
}

func buildStatsSQL(database string, query StatsQuery) (string, []string, error) {
	_, table, err := indexTableName(database, query.Index)
	if err != nil {
		return "", nil, err
	}
	selects := []string{}
	fields := []string{}
	groupAliases := []string{}
	for i, group := range query.Stats.GroupBy {
		expr, err := renderStringExpr(group, query.HotFields)
		if err != nil {
			return "", nil, err
		}
		alias := fmt.Sprintf("g%d", i)
		selects = append(selects, expr+" AS "+alias)
		fields = append(fields, group.DisplayName())
		groupAliases = append(groupAliases, alias)
	}
	for i, agg := range query.Stats.Aggregates {
		expr, err := renderAggregateExpr(agg, query.HotFields)
		if err != nil {
			return "", nil, err
		}
		alias := fmt.Sprintf("a%d", i)
		selects = append(selects, expr+" AS "+alias)
		fields = append(fields, agg.DisplayName())
	}
	conditions := buildWhereConditions(query.Keyword, query.Field, query.Value, query.StartTime, query.EndTime, query.HotFields)
	stmt := fmt.Sprintf("SELECT %s FROM %s WHERE %s", strings.Join(selects, ", "), table, strings.Join(conditions, " AND "))
	if len(groupAliases) > 0 {
		stmt += " GROUP BY " + strings.Join(groupAliases, ", ")
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	stmt += fmt.Sprintf(" ORDER BY a0 DESC LIMIT %d OFFSET %d FORMAT JSONEachRow", normalizedLimit(query.Limit), offset)
	return stmt, fields, nil
}

func buildTimelineSQL(database string, query TimelineQuery) (string, error) {
	_, table, err := indexTableName(database, query.Index)
	if err != nil {
		return "", err
	}
	bucketExpr := timelineBucketExpression(query.Interval)
	conditions := buildWhereConditions(query.Keyword, query.Field, query.Value, query.StartTime, query.EndTime, query.HotFields)
	stmt := fmt.Sprintf(
		"SELECT %s AS bucket, count() AS count FROM %s WHERE %s GROUP BY bucket ORDER BY bucket ASC FORMAT JSONEachRow",
		bucketExpr,
		table,
		strings.Join(conditions, " AND "),
	)
	return stmt, nil
}

func timelineBucketExpression(interval string) string {
	switch strings.ToLower(strings.TrimSpace(interval)) {
	case "minute":
		return "toStartOfMinute(event_time)"
	case "day":
		return "toDateTime(toDate(event_time), 'Asia/Shanghai')"
	case "month":
		return "toStartOfMonth(event_time)"
	default:
		return "toStartOfHour(event_time)"
	}
}

func buildWhereConditions(keyword, field, value string, startTime time.Time, endTime time.Time, hotFields []HotField) []string {
	conditions := []string{"1 = 1"}
	if !startTime.IsZero() {
		conditions = append(conditions, "event_time >= "+quote(formatTime(startTime)))
	}
	if !endTime.IsZero() {
		conditions = append(conditions, "event_time <= "+quote(formatTime(endTime)))
	}
	if keyword != "" {
		conditions = append(conditions, "positionCaseInsensitive(raw, "+quote(keyword)+") > 0")
	}
	if field != "" {
		if expr, ok := metadataStringColumn(field); ok {
			conditions = append(conditions, expr+" = "+quote(value))
		} else if hot, ok := resolveHotField(field, hotFields); ok && hot.Searchable {
			conditions = append(conditions, hot.Name+" = "+quote(value))
		} else {
			conditions = append(conditions, "JSONExtractString(fields_json, "+quote(field)+") = "+quote(value))
		}
	}
	return conditions
}

func renderAggregateExpr(agg splstats.Aggregate, hotFields []HotField) (string, error) {
	if agg.Func == "count" && agg.Field == nil {
		return "count()", nil
	}
	if agg.Field == nil {
		return "", fmt.Errorf("%s requires a field", agg.Func)
	}
	switch agg.Func {
	case "count":
		expr, err := renderStringExpr(*agg.Field, hotFields)
		if err != nil {
			return "", err
		}
		return "countIf(notEmpty(" + expr + "))", nil
	case "sum", "avg", "min", "max":
		expr, err := renderNumericExpr(*agg.Field, hotFields)
		if err != nil {
			return "", err
		}
		return agg.Func + "(" + expr + ")", nil
	default:
		return "", fmt.Errorf("unsupported stats function: %s", agg.Func)
	}
}

func renderStringExpr(field splstats.FieldRef, hotFields []HotField) (string, error) {
	if err := field.Validate(); err != nil {
		return "", err
	}
	switch field.Scope {
	case "", "fields":
		if expr, ok := metadataStringColumn(field.Name); ok {
			return expr, nil
		}
		if hot, ok := resolveHotField(field.Name, hotFields); ok && hot.Aggregatable {
			return hot.Name, nil
		}
		return "JSONExtractString(fields_json, " + quote(field.Name) + ")", nil
	case "metadata":
		if expr, ok := metadataStringColumn(field.Name); ok {
			return expr, nil
		}
	case "source":
		switch field.Name {
		case "type":
			return "source_type", nil
		case "name":
			return "source_name", nil
		case "host":
			return "source_host", nil
		case "ip":
			return "source_ip", nil
		}
	case "root":
		switch field.Name {
		case "index":
			return "index_name", nil
		case "raw_length":
			return "toString(raw_length)", nil
		}
	}
	return "", fmt.Errorf("unsupported field: %s", field.DisplayName())
}

func metadataStringColumn(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "source":
		return "source_name", true
	case "index":
		return "index_name", true
	case "sourcetype":
		return "sourcetype", true
	case "vendor":
		return "vendor", true
	case "product":
		return "product", true
	case "parse_status":
		return "parse_status", true
	case "parse_rule_id":
		return "parse_rule_id", true
	case "parse_rule_name":
		return "parse_rule_name", true
	case "parse_error":
		return "parse_error", true
	default:
		return "", false
	}
}

func renderNumericExpr(field splstats.FieldRef, hotFields []HotField) (string, error) {
	if field.Scope == "root" && field.Name == "raw_length" {
		return "toFloat64(raw_length)", nil
	}
	if field.Scope == "" || field.Scope == "fields" {
		if hot, ok := resolveHotField(field.Name, hotFields); ok && hot.Aggregatable && hotFieldIsNumeric(hot.Type) {
			return "toFloat64(" + hot.Name + ")", nil
		}
	}
	expr, err := renderStringExpr(field, hotFields)
	if err != nil {
		return "", err
	}
	return "toFloat64OrNull(" + expr + ")", nil
}

func hotFieldIsNumeric(fieldType string) bool {
	switch normalizeHotFieldType(fieldType) {
	case "uint64", "float64":
		return true
	default:
		return false
	}
}

func indexTableName(database string, index string) (string, string, error) {
	db, err := safeIdentifier(database, "database")
	if err != nil {
		return "", "", err
	}
	normalized, err := NormalizeIndexName(index)
	if err != nil {
		return "", "", err
	}
	return normalized, db + "." + tablePrefix + normalized, nil
}

func NormalizeIndexName(index string) (string, error) {
	index = strings.TrimSpace(index)
	if index == "" {
		index = defaultIndexName
	}
	if len(index) > 63 {
		return "", fmt.Errorf("index name is too long")
	}
	if strings.HasPrefix(index, "_") && (len(index) == 1 || index[1] < 'a' || index[1] > 'z') {
		return "", fmt.Errorf("invalid index name %q: system indexes must start with underscore followed by a lowercase letter", index)
	}
	for i, ch := range index {
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if i == 0 && ch == '_' {
			continue
		}
		if i > 0 && ch >= '0' && ch <= '9' {
			continue
		}
		if i > 0 && ch == '_' {
			continue
		}
		return "", fmt.Errorf("invalid index name %q: use lowercase letters, digits, and underscores; first character must be a lowercase letter or reserved system prefix", index)
	}
	return index, nil
}

func IsSystemIndexName(index string) bool {
	return strings.HasPrefix(strings.TrimSpace(index), "_")
}

func safeIdentifier(value string, kind string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", kind)
	}
	if len(value) > 63 {
		return "", fmt.Errorf("%s is too long", kind)
	}
	for i, ch := range value {
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if i > 0 && ch >= '0' && ch <= '9' {
			continue
		}
		if i > 0 && ch == '_' {
			continue
		}
		return "", fmt.Errorf("invalid %s %q", kind, value)
	}
	return value, nil
}

func NormalizeHotFields(fields []HotField) ([]HotField, error) {
	out := make([]HotField, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		name := strings.ToLower(strings.TrimSpace(field.Name))
		if name == "" {
			continue
		}
		if _, err := safeIdentifier(name, "hot field"); err != nil {
			return nil, err
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		field.Name = name
		field.Type = normalizeHotFieldType(field.Type)
		field.Aliases = normalizeHotFieldAliases(name, field.Aliases)
		out = append(out, field)
	}
	return out, nil
}

func normalizeHotFieldType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "uint64", "uint", "int", "integer", "number":
		return "uint64"
	case "float64", "float", "double":
		return "float64"
	case "bool", "boolean":
		return "bool"
	case "low_cardinality_string", "lowcardinality", "enum":
		return "low_cardinality_string"
	default:
		return "string"
	}
}

func normalizeHotFieldAliases(name string, aliases []string) []string {
	seen := map[string]struct{}{name: {}}
	out := []string{}
	for _, alias := range aliases {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if alias == "" {
			continue
		}
		if _, err := safeIdentifier(alias, "hot field alias"); err != nil {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		out = append(out, alias)
	}
	defaultAliases := map[string]string{"src_ip": "src", "dst_ip": "dst"}
	if alias, ok := defaultAliases[name]; ok {
		if _, exists := seen[alias]; !exists {
			out = append(out, alias)
		}
	}
	return out
}

func resolveHotField(field string, fields []HotField) (HotField, bool) {
	name := strings.ToLower(strings.TrimSpace(field))
	for _, item := range fields {
		if item.Name == name {
			return item, true
		}
		for _, alias := range item.Aliases {
			if alias == name {
				return item, true
			}
		}
	}
	return HotField{}, false
}

func hotFieldDDL(table string, fields []HotField) (string, error) {
	normalized, err := NormalizeHotFields(fields)
	if err != nil {
		return "", err
	}
	return strings.Join(hotFieldStatements(table, normalized), "\n"), nil
}

func hotFieldStatements(table string, fields []HotField) []string {
	statements := []string{}
	for _, field := range fields {
		statements = append(statements, "ALTER TABLE "+table+" ADD COLUMN IF NOT EXISTS "+field.Name+" "+hotFieldColumnDDL(field))
		if field.Searchable {
			statements = append(statements, "ALTER TABLE "+table+" ADD INDEX IF NOT EXISTS "+hotFieldIndexName(field.Name)+" "+field.Name+" "+hotFieldIndexDDL(field)+" GRANULARITY 4")
		}
	}
	return statements
}

func hotFieldColumnDDL(field HotField) string {
	extract := "JSONExtractString(fields_json, " + quote(field.Name) + ")"
	switch field.Type {
	case "uint64":
		return "UInt64 MATERIALIZED toUInt64OrZero(" + extract + ")"
	case "float64":
		return "Float64 MATERIALIZED toFloat64OrZero(" + extract + ")"
	case "bool":
		return "UInt8 MATERIALIZED if(lower(" + extract + ") IN ('true', '1', 'yes'), 1, 0)"
	case "low_cardinality_string":
		return "LowCardinality(String) MATERIALIZED " + extract
	default:
		return "String MATERIALIZED " + extract
	}
}

func hotFieldIndexDDL(field HotField) string {
	if field.Type == "low_cardinality_string" || field.Type == "bool" {
		return "TYPE set(1000)"
	}
	return "TYPE bloom_filter(0.01)"
}

func hotFieldIndexName(field string) string {
	name := "idx_hot_" + field
	if len(name) > 63 {
		return name[:63]
	}
	return name
}

func indexTableDDL(table string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s
(
    index_name LowCardinality(String),
    event_id String,
    event_date Date DEFAULT toDate(event_time),
    event_time DateTime64(3, 'Asia/Shanghai'),
    ingest_time DateTime64(3, 'Asia/Shanghai'),
    pipeline_id String,
    pipeline_version String,
    source_type LowCardinality(String),
    source_name String,
    source_host String,
    source_ip String,
    sourcetype LowCardinality(String),
    parse_status LowCardinality(String) DEFAULT 'unparsed',
    parse_rule_id String DEFAULT '',
    parse_rule_name String DEFAULT '',
    parse_error String DEFAULT '',
    parsed_at DateTime64(3, 'Asia/Shanghai') DEFAULT toDateTime64('1970-01-01 00:00:00', 3, 'Asia/Shanghai'),
    vendor LowCardinality(String),
    product LowCardinality(String),
    raw String,
    raw_length UInt32 DEFAULT length(raw),
    fields_json String,
    labels_json String,
    tags Array(String),
    errors_json String,
    has_error UInt8 DEFAULT if(length(errors_json) > 2, 1, 0),
    created_at DateTime64(3, 'Asia/Shanghai') DEFAULT now64(3, 'Asia/Shanghai')
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (event_time, event_id)
TTL event_date + INTERVAL 30 DAY DELETE
SETTINGS index_granularity = 8192`, table)
}

func normalizedLimit(limit int) int {
	if limit <= 0 || limit > 1000 {
		return 100
	}
	return limit
}

func (c *Client) exec(ctx context.Context, query string) ([]byte, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil {
		return nil, err
	}
	params := u.Query()
	params.Set("database", c.Database)
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(query))
	if err != nil {
		return nil, err
	}
	if c.Username != "" || c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("clickhouse error: %s", strings.TrimSpace(string(data)))
	}
	return data, nil
}

type eventRow struct {
	IndexName       string   `json:"index_name"`
	EventID         string   `json:"event_id"`
	EventTime       string   `json:"event_time"`
	IngestTime      string   `json:"ingest_time"`
	PipelineID      string   `json:"pipeline_id"`
	PipelineVersion string   `json:"pipeline_version"`
	SourceType      string   `json:"source_type"`
	SourceName      string   `json:"source_name"`
	SourceHost      string   `json:"source_host"`
	SourceIP        string   `json:"source_ip"`
	Sourcetype      string   `json:"sourcetype"`
	ParseStatus     string   `json:"parse_status"`
	ParseRuleID     string   `json:"parse_rule_id"`
	ParseRuleName   string   `json:"parse_rule_name"`
	ParseError      string   `json:"parse_error"`
	ParsedAt        string   `json:"parsed_at"`
	Vendor          string   `json:"vendor"`
	Product         string   `json:"product"`
	Raw             string   `json:"raw"`
	FieldsJSON      string   `json:"fields_json"`
	LabelsJSON      string   `json:"labels_json"`
	Tags            []string `json:"tags"`
	ErrorsJSON      string   `json:"errors_json"`
}

func eventToRow(e *event.Event) (eventRow, error) {
	index, err := NormalizeIndexName(stringValue(e.Metadata["index"], defaultIndexName))
	if err != nil {
		return eventRow{}, err
	}
	fields, err := json.Marshal(e.Fields)
	if err != nil {
		return eventRow{}, err
	}
	labels, err := json.Marshal(e.Labels)
	if err != nil {
		return eventRow{}, err
	}
	errorsJSON, err := json.Marshal(e.Errors)
	if err != nil {
		return eventRow{}, err
	}
	return eventRow{
		IndexName:       index,
		EventID:         e.EventID,
		EventTime:       formatTime(e.EventTime),
		IngestTime:      formatTime(e.IngestTime),
		PipelineID:      e.PipelineID,
		PipelineVersion: e.PipelineVersion,
		SourceType:      e.Source.Type,
		SourceName:      e.Source.Name,
		SourceHost:      e.Source.Host,
		SourceIP:        e.Source.IP,
		Sourcetype:      stringValue(e.Metadata["sourcetype"], ""),
		ParseStatus:     normalizedParseStatus(e.Metadata["parse_status"]),
		ParseRuleID:     stringValue(e.Metadata["parse_rule_id"], ""),
		ParseRuleName:   stringValue(e.Metadata["parse_rule_name"], ""),
		ParseError:      stringValue(e.Metadata["parse_error"], ""),
		ParsedAt:        formatParsedAt(e.Metadata["parsed_at"]),
		Vendor:          stringValue(e.Metadata["vendor"], ""),
		Product:         stringValue(e.Metadata["product"], ""),
		Raw:             e.Raw,
		FieldsJSON:      string(fields),
		LabelsJSON:      string(labels),
		Tags:            e.Tags,
		ErrorsJSON:      string(errorsJSON),
	}, nil
}

func (r eventRow) toEvent() *event.Event {
	fields := map[string]any{}
	labels := map[string]string{}
	errorsList := []event.ProcessingError{}
	_ = json.Unmarshal([]byte(r.FieldsJSON), &fields)
	_ = json.Unmarshal([]byte(r.LabelsJSON), &labels)
	_ = json.Unmarshal([]byte(r.ErrorsJSON), &errorsList)
	eventTime, _ := parseStorageTime(r.EventTime)
	ingestTime, _ := parseStorageTime(r.IngestTime)
	return &event.Event{
		EventID:         r.EventID,
		EventTime:       eventTime,
		IngestTime:      ingestTime,
		PipelineID:      r.PipelineID,
		PipelineVersion: r.PipelineVersion,
		Source:          event.Source{Type: r.SourceType, Name: r.SourceName, Host: r.SourceHost, IP: r.SourceIP},
		Metadata:        rowMetadata(r),
		Raw:             r.Raw,
		Fields:          fields,
		Labels:          labels,
		Tags:            r.Tags,
		Errors:          errorsList,
	}
}

func rowMetadata(r eventRow) map[string]any {
	return map[string]any{
		"index":           r.IndexName,
		"sourcetype":      r.Sourcetype,
		"vendor":          r.Vendor,
		"product":         r.Product,
		"parse_status":    firstNonEmpty(r.ParseStatus, "unparsed"),
		"parse_rule_id":   r.ParseRuleID,
		"parse_rule_name": r.ParseRuleName,
		"parse_error":     r.ParseError,
		"parsed_at":       r.ParsedAt,
	}
}

func normalizedParseStatus(value any) string {
	switch strings.TrimSpace(fmt.Sprint(value)) {
	case "parsed", "unparsed", "parse_failed":
		return strings.TrimSpace(fmt.Sprint(value))
	default:
		return "unparsed"
	}
}

func formatParsedAt(value any) string {
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return "1970-01-01 00:00:00.000"
		}
		return formatTime(typed)
	case string:
		if strings.TrimSpace(typed) != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
				return formatTime(parsed)
			}
			return strings.TrimSpace(typed)
		}
	}
	return "1970-01-01 00:00:00.000"
}

func stringValue(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	return fmt.Sprint(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func uint64FromAny(value any) uint64 {
	switch v := value.(type) {
	case float64:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case json.Number:
		n, _ := strconv.ParseUint(v.String(), 10, 64)
		return n
	case string:
		n, _ := strconv.ParseUint(v, 10, 64)
		return n
	default:
		text := fmt.Sprint(value)
		n, _ := strconv.ParseUint(text, 10, 64)
		return n
	}
}

func quote(value string) string {
	escaped := strings.NewReplacer(
		"\\", "\\\\",
		"'", "\\'",
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
	).Replace(value)
	return "'" + escaped + "'"
}

func formatTime(t time.Time) string {
	return t.In(storageLocation).Format("2006-01-02 15:04:05.000")
}

func parseStorageTime(value string) (time.Time, error) {
	text := strings.TrimSpace(strings.Replace(value, "T", " ", 1))
	if text == "" {
		return time.Time{}, nil
	}
	if strings.Contains(text, ".") {
		return time.ParseInLocation("2006-01-02 15:04:05.000", text, storageLocation)
	}
	return time.ParseInLocation(time.DateTime, text, storageLocation)
}

func mustLoadLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return location
}

func ParseLimit(value string, fallback int) int {
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return fallback
	}
	return limit
}
