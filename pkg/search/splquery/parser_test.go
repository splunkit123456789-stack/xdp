package splquery

import "testing"

func TestParseIndexSearch(t *testing.T) {
	query, err := Parse("index=a")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if query.Filters.Index != "a" {
		t.Fatalf("index = %q, want a", query.Filters.Index)
	}
	if query.Stats != nil {
		t.Fatal("stats should be nil")
	}
}

func TestParseIndexFieldSearch(t *testing.T) {
	query, err := Parse("index=a service=api")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if query.Filters.Index != "a" || query.Filters.Field != "service" || query.Filters.Value != "api" {
		t.Fatalf("filters = %#v", query.Filters)
	}
}

func TestParseIndexStatsPipe(t *testing.T) {
	query, err := Parse("index=a | stats count as total by service")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if query.Filters.Index != "a" {
		t.Fatalf("index = %q, want a", query.Filters.Index)
	}
	if query.Stats == nil {
		t.Fatal("stats should be parsed")
	}
	if query.Stats.Aggregates[0].DisplayName() != "total" {
		t.Fatalf("aggregate = %q, want total", query.Stats.Aggregates[0].DisplayName())
	}
	if query.Stats.GroupBy[0].DisplayName() != "service" {
		t.Fatalf("group = %q, want service", query.Stats.GroupBy[0].DisplayName())
	}
}

func TestParseStatsOnlyStillWorks(t *testing.T) {
	query, err := Parse("stats count by service")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if query.Filters.Index != "" || query.Stats == nil {
		t.Fatalf("query = %#v", query)
	}
}

func TestParseRejectsUnsupportedSearch(t *testing.T) {
	tests := []string{
		"index=a | stats count | sort -count",
		"index='a",
		"service=api status=500",
		"index=a | where service=api",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatal("Parse() expected error")
			}
		})
	}
}

func TestParseFieldFilterWithQuotedValue(t *testing.T) {
	query, err := Parse(`index=audit src="10.0.1.8"`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if query.Filters.Index != "audit" || query.Filters.Field != "src" || query.Filters.Value != "10.0.1.8" {
		t.Fatalf("filters = %#v", query.Filters)
	}
}

func TestParseNormalizesSpacesAroundEquals(t *testing.T) {
	inputs := []string{
		"index=audit action=deny",
		"index= audit action=deny",
		"index =audit action=deny",
		"index = audit action = deny",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			query, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if query.Filters.Index != "audit" || query.Filters.Field != "action" || query.Filters.Value != "deny" {
				t.Fatalf("filters = %#v", query.Filters)
			}
		})
	}
}
