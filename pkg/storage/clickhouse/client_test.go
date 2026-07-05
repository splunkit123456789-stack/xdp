package clickhouse

import (
	"strings"
	"testing"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/search/splstats"
)

func TestBuildStatsSQLCountByField(t *testing.T) {
	stats, err := splstats.Parse("stats count as total by service")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	sql, fields, err := buildStatsSQL("xdp", StatsQuery{Index: "app", Limit: 10, Stats: stats, HotFields: []HotField{{Name: "service", Type: "string", Searchable: true, Aggregatable: true}}})
	if err != nil {
		t.Fatalf("buildStatsSQL() error = %v", err)
	}
	mustContain(t, sql, "FROM xdp.events_app")
	mustContain(t, sql, "service AS g0")
	mustContain(t, sql, "count() AS a0")
	mustContain(t, sql, "GROUP BY g0")
	mustContain(t, sql, "ORDER BY a0 DESC LIMIT 10")
	if strings.Contains(sql, "JSONExtractString(fields_json, 'service')") || strings.Contains(sql, "total AS") {
		t.Fatalf("sql should use hot field column and internal aliases only: %s", sql)
	}
	if len(fields) != 2 || fields[0] != "service" || fields[1] != "total" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestBuildStatsSQLUsesOffsetForPagedAggregateRows(t *testing.T) {
	stats, err := splstats.Parse("stats count as total by service")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	sql, _, err := buildStatsSQL("xdp", StatsQuery{Index: "app", Limit: 10, Offset: 20, Stats: stats})
	if err != nil {
		t.Fatalf("buildStatsSQL() error = %v", err)
	}
	mustContain(t, sql, "ORDER BY a0 DESC LIMIT 10 OFFSET 20")
}

func TestBuildStatsSQLNumericAggregate(t *testing.T) {
	stats, err := splstats.Parse("stats avg(bytes) as avg_bytes by source.ip")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	sql, _, err := buildStatsSQL("xdp", StatsQuery{Limit: 100, Stats: stats})
	if err != nil {
		t.Fatalf("buildStatsSQL() error = %v", err)
	}
	mustContain(t, sql, "FROM xdp.events_app")
	mustContain(t, sql, "source_ip AS g0")
	mustContain(t, sql, "avg(toFloat64OrNull(JSONExtractString(fields_json, 'bytes'))) AS a0")
}

func TestBuildStatsSQLNumericAggregateUsesNumericHotFieldColumn(t *testing.T) {
	stats, err := splstats.Parse("stats sum(bytes) as total_bytes avg(bytes) as avg_bytes by src action")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	sql, _, err := buildStatsSQL("xdp", StatsQuery{
		Index: "audit",
		Limit: 10,
		Stats: stats,
		HotFields: []HotField{
			{Name: "src_ip", Type: "string", Searchable: true, Aggregatable: true, Aliases: []string{"src"}},
			{Name: "action", Type: "low_cardinality_string", Searchable: true, Aggregatable: true},
			{Name: "bytes", Type: "uint64", Aggregatable: true},
		},
	})
	if err != nil {
		t.Fatalf("buildStatsSQL() error = %v", err)
	}
	mustContain(t, sql, "src_ip AS g0")
	mustContain(t, sql, "action AS g1")
	mustContain(t, sql, "sum(toFloat64(bytes)) AS a0")
	mustContain(t, sql, "avg(toFloat64(bytes)) AS a1")
	if strings.Contains(sql, "toFloat64OrNull(bytes)") {
		t.Fatalf("numeric hot field column must not use toFloat64OrNull: %s", sql)
	}
}

func TestBuildStatsSQLMapsBareSourceToSourceName(t *testing.T) {
	stats, err := splstats.Parse("stats count as total by source")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	sql, fields, err := buildStatsSQL("xdp", StatsQuery{Index: "audit_p0", Limit: 10, Stats: stats})
	if err != nil {
		t.Fatalf("buildStatsSQL() error = %v", err)
	}
	mustContain(t, sql, "source_name AS g0")
	if strings.Contains(sql, "JSONExtractString(fields_json, 'source')") {
		t.Fatalf("bare source must use event metadata source_name, got SQL: %s", sql)
	}
	if len(fields) != 2 || fields[0] != "source" || fields[1] != "total" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestBuildStatsSQLIncludesFilters(t *testing.T) {
	stats, err := splstats.Parse("stats count by service")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	sql, _, err := buildStatsSQL("xdp", StatsQuery{
		Index:     "firewall",
		Keyword:   "error",
		Field:     "service",
		Value:     "api",
		StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		Stats:     stats,
		HotFields: []HotField{{Name: "service", Type: "string", Searchable: true, Aggregatable: true}},
	})
	if err != nil {
		t.Fatalf("buildStatsSQL() error = %v", err)
	}
	mustContain(t, sql, "FROM xdp.events_firewall")
	mustContain(t, sql, "event_time >= '2026-01-01 08:00:00.000'")
	mustContain(t, sql, "event_time <= '2026-01-02 08:00:00.000'")
	mustContain(t, sql, "positionCaseInsensitive(raw, 'error') > 0")
	mustContain(t, sql, "service = 'api'")
}

func TestBuildTimelineSQLAggregatesAllMatchingEventsWithoutLimit(t *testing.T) {
	sql, err := buildTimelineSQL("xdp", TimelineQuery{
		Index:     "audit_p0",
		Field:     "src",
		Value:     "10.0.1.8",
		StartTime: time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		Interval:  "hour",
		HotFields: []HotField{{Name: "src_ip", Type: "string", Searchable: true, Aggregatable: true, Aliases: []string{"src"}}},
	})
	if err != nil {
		t.Fatalf("buildTimelineSQL() error = %v", err)
	}
	mustContain(t, sql, "SELECT toStartOfHour(event_time) AS bucket, count() AS count")
	mustContain(t, sql, "FROM xdp.events_audit_p0")
	mustContain(t, sql, "src_ip = '10.0.1.8'")
	mustContain(t, sql, "GROUP BY bucket")
	mustContain(t, sql, "ORDER BY bucket ASC")
	if strings.Contains(sql, "LIMIT") || strings.Contains(sql, "OFFSET") {
		t.Fatalf("timeline SQL must aggregate all matching events without pagination: %s", sql)
	}
}

func TestIndexTableDDLUsesShanghaiTimezone(t *testing.T) {
	sql := indexTableDDL("xdp.events_audit")
	mustContain(t, sql, "event_time DateTime64(3, 'Asia/Shanghai')")
	mustContain(t, sql, "ingest_time DateTime64(3, 'Asia/Shanghai')")
	mustContain(t, sql, "created_at DateTime64(3, 'Asia/Shanghai') DEFAULT now64(3, 'Asia/Shanghai')")
	mustContain(t, sql, "parse_status LowCardinality(String) DEFAULT 'unparsed'")
	mustContain(t, sql, "parse_rule_id String DEFAULT ''")
	mustContain(t, sql, "parse_rule_name String DEFAULT ''")
	mustContain(t, sql, "parse_error String DEFAULT ''")
	mustContain(t, sql, "parsed_at DateTime64(3, 'Asia/Shanghai')")
	if strings.Contains(sql, "DateTime64(3, 'UTC')") {
		t.Fatalf("index table DDL must not use UTC timezone: %s", sql)
	}
}

func TestBuildWhereConditionsFallsBackToFieldsJSONForColdField(t *testing.T) {
	conditions := buildWhereConditions("", "cold_field", "value", time.Time{}, time.Time{}, nil)
	sql := strings.Join(conditions, " AND ")
	mustContain(t, sql, "JSONExtractString(fields_json, 'cold_field') = 'value'")
}

func TestBuildWhereConditionsMapsBareSourceToSourceName(t *testing.T) {
	conditions := buildWhereConditions("", "source", "Firewall Syslog", time.Time{}, time.Time{}, nil)
	sql := strings.Join(conditions, " AND ")
	mustContain(t, sql, "source_name = 'Firewall Syslog'")
	if strings.Contains(sql, "JSONExtractString(fields_json, 'source')") {
		t.Fatalf("bare source filter must use event metadata source_name, got SQL: %s", sql)
	}
}

func TestEventToRowUsesDatasourceNameAndParseRuleNameMetadata(t *testing.T) {
	ev := event.New("src=10.0.1.8 action=deny", event.Source{Type: "syslog", Name: "Firewall Syslog P0"}, time.Now().UTC())
	ev.Metadata["index"] = "audit_p0"
	ev.Metadata["sourcetype"] = "Firewall Regex"
	ev.Metadata["parse_status"] = "parsed"
	ev.Metadata["parse_rule_id"] = "pr_firewall_regex"
	ev.Metadata["parse_rule_name"] = "Firewall Regex"
	ev.Metadata["parse_error"] = ""

	row, err := eventToRow(ev)
	if err != nil {
		t.Fatalf("eventToRow() error = %v", err)
	}
	if row.SourceName != "Firewall Syslog P0" {
		t.Fatalf("source_name = %q, want Firewall Syslog P0", row.SourceName)
	}
	if row.Sourcetype != "Firewall Regex" {
		t.Fatalf("sourcetype = %q, want Firewall Regex", row.Sourcetype)
	}
	if row.ParseStatus != "parsed" || row.ParseRuleID != "pr_firewall_regex" || row.ParseRuleName != "Firewall Regex" || row.ParseError != "" {
		t.Fatalf("parse metadata row = %#v", row)
	}
}

func TestBuildWhereConditionsUsesParseStatusMetadataColumn(t *testing.T) {
	conditions := buildWhereConditions("", "parse_status", "parse_failed", time.Time{}, time.Time{}, nil)
	sql := strings.Join(conditions, " AND ")
	mustContain(t, sql, "parse_status = 'parse_failed'")
	if strings.Contains(sql, "JSONExtractString(fields_json, 'parse_status')") {
		t.Fatalf("parse_status must use metadata column, got SQL: %s", sql)
	}
}

func TestBuildStatsSQLCanGroupByParseStatus(t *testing.T) {
	stats, err := splstats.Parse("stats count by parse_status")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	sql, fields, err := buildStatsSQL("xdp", StatsQuery{Index: "audit", Limit: 10, Stats: stats})
	if err != nil {
		t.Fatalf("buildStatsSQL() error = %v", err)
	}
	mustContain(t, sql, "parse_status AS g0")
	mustContain(t, sql, "GROUP BY g0")
	if len(fields) != 2 || fields[0] != "parse_status" || fields[1] != "count" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestHotFieldDDLDoesNotMaterializeHistory(t *testing.T) {
	sql, err := hotFieldDDL("xdp.events_audit", []HotField{
		{Name: "src_ip", Type: "string", Searchable: true, Aggregatable: true},
		{Name: "bytes", Type: "uint64", Aggregatable: true},
	})
	if err != nil {
		t.Fatalf("hotFieldDDL() error = %v", err)
	}
	mustContain(t, sql, "ADD COLUMN IF NOT EXISTS src_ip String MATERIALIZED JSONExtractString(fields_json, 'src_ip')")
	mustContain(t, sql, "ADD INDEX IF NOT EXISTS idx_hot_src_ip src_ip TYPE bloom_filter(0.01) GRANULARITY 4")
	mustContain(t, sql, "ADD COLUMN IF NOT EXISTS bytes UInt64 MATERIALIZED toUInt64OrZero(JSONExtractString(fields_json, 'bytes'))")
	if strings.Contains(sql, "MATERIALIZE COLUMN") || strings.Contains(sql, "MATERIALIZE INDEX") {
		t.Fatalf("hot field DDL must not backfill historical data: %s", sql)
	}
}

func TestIndexTableNameRejectsUnsafeIndex(t *testing.T) {
	_, _, err := indexTableName("xdp", "bad-name")
	if err == nil {
		t.Fatal("indexTableName() expected error")
	}
}

func TestIndexTableNameAllowsSystemUnparsedIndex(t *testing.T) {
	normalized, table, err := indexTableName("xdp", "_unparsed")
	if err != nil {
		t.Fatalf("indexTableName() error = %v", err)
	}
	if normalized != "_unparsed" {
		t.Fatalf("normalized index = %q, want _unparsed", normalized)
	}
	if table != "xdp.events__unparsed" {
		t.Fatalf("table = %q, want xdp.events__unparsed", table)
	}
}

func mustContain(t *testing.T, value string, want string) {
	t.Helper()
	if !strings.Contains(value, want) {
		t.Fatalf("%q does not contain %q", value, want)
	}
}
