package stats

import (
	"context"
	"testing"
	"time"

	"xdp/pkg/plugin"
	"xdp/pkg/search/splstats"
)

type testBackend struct {
	got plugin.SearchStatsQuery
}

func (b *testBackend) Stats(ctx context.Context, query plugin.SearchStatsQuery) (splstats.Result, error) {
	b.got = query
	return splstats.Result{
		Query:  query.Stats.Raw,
		Fields: []string{"service", "total"},
		Rows:   []map[string]any{{"service": "api", "total": 2}},
		Limit:  query.Limit,
	}, nil
}

func TestStatsPluginExecutesAgainstSearchBackend(t *testing.T) {
	query, err := splstats.Parse("stats count as total by service")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	backend := &testBackend{}
	input := plugin.SearchInput{
		Index:   "audit",
		Keyword: "deny",
		Field:   "action",
		Value:   "deny",
		FieldFilters: []plugin.SearchFieldFilter{
			{Field: "action", Value: "deny"},
			{Field: "parse_status", Value: "parsed"},
		},
		StartTime: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		Limit:     1000,
		Backend:   backend,
	}
	plugin := New()
	if err := plugin.Init(pluginInitContext{}, map[string]any{}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	result, err := plugin.Execute(context.Background(), input, query)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Query != query.Raw || len(result.Rows) != 1 || result.Rows[0]["service"] != "api" {
		t.Fatalf("result = %#v", result)
	}
	if backend.got.Index != "audit" || backend.got.Keyword != "deny" || backend.got.Field != "action" || backend.got.Stats.Raw != query.Raw {
		t.Fatalf("backend query = %#v", backend.got)
	}
	if len(backend.got.FieldFilters) != 2 || backend.got.FieldFilters[1].Field != "parse_status" {
		t.Fatalf("backend field filters = %#v", backend.got.FieldFilters)
	}
}

type pluginInitContext struct{}

func (pluginInitContext) Context() context.Context { return context.Background() }
func (pluginInitContext) PluginCode() string       { return "stats" }
func (pluginInitContext) PluginVersion() string    { return "1.0.0" }
