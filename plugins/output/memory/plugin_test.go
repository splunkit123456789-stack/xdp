package memory

import (
	"testing"
	"time"

	"context"
	"xdp/pkg/event"

	"xdp/pkg/plugin"
	"xdp/pkg/search/splstats"
)

func TestStoreSearchMatchesMultipleFieldFilters(t *testing.T) {
	store := NewStore()
	matched := event.New("json checkout", event.Source{Name: "Kafka JSON"}, time.Now())
	matched.Metadata["index"] = "json_p1"
	matched.Metadata["parse_status"] = "parsed"
	matched.Fields["service"] = "checkout"
	store.Append(matched)

	other := event.New("json checkout", event.Source{Name: "Kafka JSON"}, time.Now())
	other.Metadata["index"] = "json_p1"
	other.Metadata["parse_status"] = "unparsed"
	other.Fields["service"] = "checkout"
	store.Append(other)

	results := store.Search(SearchQuery{
		Index: "json_p1",
		FieldFilters: []FieldFilter{
			{Field: "service", Value: "checkout"},
			{Field: "parse_status", Value: "parsed"},
		},
	})
	if len(results) != 1 || results[0] != matched {
		t.Fatalf("results = %#v, want only matched event", results)
	}
}

func TestStoreStatsCountByField(t *testing.T) {
	store := NewStore()
	store.Append(testEvent("app", map[string]any{"service": "api", "bytes": 100}))
	store.Append(testEvent("app", map[string]any{"service": "api", "bytes": 200}))
	store.Append(testEvent("app", map[string]any{"service": "web", "bytes": 300}))
	stats, err := splstats.Parse("stats count as total by service")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	result := store.Stats(StatsQuery{Index: "app", Stats: stats, Limit: 10})
	if len(result.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(result.Rows))
	}
	if result.Rows[0]["service"] != "api" || result.Rows[0]["total"] != 2 {
		t.Fatalf("first row = %#v", result.Rows[0])
	}
}

func TestStoreStatsAvg(t *testing.T) {
	store := NewStore()
	store.Append(testEvent("app", map[string]any{"service": "api", "bytes": 100}))
	store.Append(testEvent("app", map[string]any{"service": "api", "bytes": 200}))
	stats, err := splstats.Parse("stats avg(bytes) as avg_bytes by service")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	result := store.Stats(StatsQuery{Index: "app", Stats: stats, Limit: 10})
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(result.Rows))
	}
	if result.Rows[0]["avg_bytes"] != 150.0 {
		t.Fatalf("avg row = %#v", result.Rows[0])
	}
}

func TestStoreStatsCountByBareSourceUsesSourceName(t *testing.T) {
	store := NewStore()
	first := testEvent("audit_p0", map[string]any{"action": "deny"})
	first.Source.Name = "Firewall Syslog"
	second := testEvent("audit_p0", map[string]any{"action": "allow"})
	second.Source.Name = "Firewall Syslog"
	store.Append(first)
	store.Append(second)

	stats, err := splstats.Parse("stats count as total by source")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	result := store.Stats(StatsQuery{Index: "audit_p0", Stats: stats, Limit: 10})
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %d, want 1: %#v", len(result.Rows), result.Rows)
	}
	if result.Rows[0]["source"] != "Firewall Syslog" || result.Rows[0]["total"] != 2 {
		t.Fatalf("stats row = %#v", result.Rows[0])
	}
}

func TestStoreSearchFiltersByEventTime(t *testing.T) {
	store := NewStore()
	oldEvent := testEvent("app", map[string]any{"service": "api"})
	oldEvent.EventTime = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	newEvent := testEvent("app", map[string]any{"service": "api"})
	newEvent.EventTime = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	store.Append(oldEvent)
	store.Append(newEvent)

	result := store.Search(SearchQuery{
		Index:     "app",
		StartTime: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 1, 2, 23, 59, 59, 0, time.UTC),
	})
	if len(result) != 1 || result[0].EventID != newEvent.EventID {
		t.Fatalf("result = %#v, want only new event", result)
	}
}

func TestStoreSearchFiltersByBareSourceUsesSourceName(t *testing.T) {
	store := NewStore()
	first := testEvent("audit_p0", map[string]any{"action": "deny"})
	first.Source.Name = "Firewall Syslog"
	second := testEvent("audit_p0", map[string]any{"action": "deny"})
	second.Source.Name = "Kafka Audit"
	store.Append(first)
	store.Append(second)

	result := store.Search(SearchQuery{Index: "audit_p0", Field: "source", Value: "Firewall Syslog"})
	if len(result) != 1 || result[0].Source.Name != "Firewall Syslog" {
		t.Fatalf("result = %#v, want only Firewall Syslog event", result)
	}
}

func TestOutputAppliesConfiguredIndex(t *testing.T) {
	output := New()
	if err := output.Init(plugin.BasicInitContext{Code: "memory-output", Version: "1.0.0"}, map[string]any{"index": "a"}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	e := event.New(`{"service":"api"}`, event.Source{Type: "test"}, time.Now().UTC())
	if err := output.Write(context.Background(), &plugin.EventBatch{Events: []*event.Event{e}}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if e.Metadata["index"] != "a" {
		t.Fatalf("index = %#v, want a", e.Metadata["index"])
	}
}

func testEvent(index string, fields map[string]any) *event.Event {
	e := event.New("raw", event.Source{Type: "test"}, time.Now().UTC())
	e.Metadata["index"] = index
	e.Fields = fields
	return e
}
