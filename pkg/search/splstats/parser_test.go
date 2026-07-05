package splstats

import "testing"

func TestParseStatsQueries(t *testing.T) {
	tests := []struct {
		input      string
		aggs       int
		groups     int
		firstAlias string
	}{
		{input: "stats count", aggs: 1},
		{input: "stats count by service", aggs: 1, groups: 1},
		{input: "stats count as total by service,status", aggs: 1, groups: 2, firstAlias: "total"},
		{input: "stats count(), avg(bytes) as avg_bytes by service", aggs: 2, groups: 1},
		{input: "| stats count by source.ip", aggs: 1, groups: 1},
		{input: "stats sum(bytes), min(bytes), max(bytes) by service", aggs: 3, groups: 1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			query, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(query.Aggregates) != tt.aggs {
				t.Fatalf("aggregates = %d, want %d", len(query.Aggregates), tt.aggs)
			}
			if len(query.GroupBy) != tt.groups {
				t.Fatalf("group by = %d, want %d", len(query.GroupBy), tt.groups)
			}
			if tt.firstAlias != "" && query.Aggregates[0].Alias != tt.firstAlias {
				t.Fatalf("alias = %q, want %q", query.Aggregates[0].Alias, tt.firstAlias)
			}
		})
	}
}

func TestParseStatsSupportsSplunkStyleWhitespaceLists(t *testing.T) {
	query, err := Parse("stats count as total sum(bytes) as total_bytes avg(bytes) as avg_bytes min(bytes) as min_bytes max(bytes) as max_bytes by service action")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(query.Aggregates) != 5 {
		t.Fatalf("aggregates = %d, want 5", len(query.Aggregates))
	}
	if query.Aggregates[1].DisplayName() != "total_bytes" || query.Aggregates[4].DisplayName() != "max_bytes" {
		t.Fatalf("aggregates = %#v", query.Aggregates)
	}
	if len(query.GroupBy) != 2 {
		t.Fatalf("group by = %d, want 2", len(query.GroupBy))
	}
	if query.GroupBy[0].DisplayName() != "service" || query.GroupBy[1].DisplayName() != "action" {
		t.Fatalf("group by = %#v", query.GroupBy)
	}
}

func TestParseRejectsUnsupportedQueries(t *testing.T) {
	tests := []string{
		"search error | stats count",
		"stats count by service | sort -count",
		"stats values(raw) by service",
		"stats count by service; drop table",
		"stats count by *",
		"stats count(eval(status=500))",
		"where status=500",
		"stats count by metadata.unknown",
		"stats avg by service",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatal("Parse() expected error")
			}
		})
	}
}

func TestAggregateDisplayName(t *testing.T) {
	query, err := Parse("stats count as total, avg(bytes) by service")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if query.Aggregates[0].DisplayName() != "total" {
		t.Fatalf("DisplayName() = %q, want total", query.Aggregates[0].DisplayName())
	}
	if query.Aggregates[1].DisplayName() != "avg_bytes" {
		t.Fatalf("DisplayName() = %q, want avg_bytes", query.Aggregates[1].DisplayName())
	}
}
