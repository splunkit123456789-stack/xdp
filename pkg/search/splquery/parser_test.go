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

func TestParseIndexMultipleFieldSearch(t *testing.T) {
	query, err := Parse(`index=json_p1 service=checkout parse_status=parsed level="warn"`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if query.Filters.Index != "json_p1" || query.Filters.Field != "service" || query.Filters.Value != "checkout" {
		t.Fatalf("legacy filters = %#v", query.Filters)
	}
	want := []FieldFilter{
		{Field: "service", Value: "checkout"},
		{Field: "parse_status", Value: "parsed"},
		{Field: "level", Value: "warn"},
	}
	if len(query.Filters.FieldFilters) != len(want) {
		t.Fatalf("field filters = %#v, want %#v", query.Filters.FieldFilters, want)
	}
	for i := range want {
		if query.Filters.FieldFilters[i] != want[i] {
			t.Fatalf("field filters = %#v, want %#v", query.Filters.FieldFilters, want)
		}
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

func TestParseSearchCommandPipeline(t *testing.T) {
	query, err := Parse("index=audit | table _time service action bytes | sort - bytes | head 10 | dedup service action")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if query.Filters.Index != "audit" {
		t.Fatalf("index = %q, want audit", query.Filters.Index)
	}
	if query.Stats != nil {
		t.Fatal("stats should be nil")
	}
	if len(query.Commands) != 4 {
		t.Fatalf("commands = %#v, want 4", query.Commands)
	}
	assertCommand(t, query.Commands[0], "table", []string{"_time", "service", "action", "bytes"})
	assertCommand(t, query.Commands[1], "sort", []string{"-", "bytes"})
	assertCommand(t, query.Commands[2], "head", []string{"10"})
	assertCommand(t, query.Commands[3], "dedup", []string{"service", "action"})
}

func TestParseStatsThenSearchCommandPipeline(t *testing.T) {
	query, err := Parse("index=audit | stats count as total by level service | sort level service | head 5 | table level service total")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if query.Filters.Index != "audit" {
		t.Fatalf("index = %q, want audit", query.Filters.Index)
	}
	if query.Stats == nil {
		t.Fatal("stats should be parsed")
	}
	if len(query.Commands) != 3 {
		t.Fatalf("commands = %#v, want 3", query.Commands)
	}
	assertCommand(t, query.Commands[0], "sort", []string{"level", "service"})
	assertCommand(t, query.Commands[1], "head", []string{"5"})
	assertCommand(t, query.Commands[2], "table", []string{"level", "service", "total"})
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
		"index='a",
		"index=a | where service=api",
		"index=a | table",
		"index=a | sort",
		"index=a | sort -",
		"index=a | head 0",
		"index=a | dedup",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatal("Parse() expected error")
			}
		})
	}
}

func assertCommand(t *testing.T, got Command, name string, args []string) {
	t.Helper()
	if got.Name != name {
		t.Fatalf("command name = %q, want %q", got.Name, name)
	}
	if len(got.Args) != len(args) {
		t.Fatalf("command args = %#v, want %#v", got.Args, args)
	}
	for i := range args {
		if got.Args[i] != args[i] {
			t.Fatalf("command args = %#v, want %#v", got.Args, args)
		}
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
