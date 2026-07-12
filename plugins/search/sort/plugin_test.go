package sortcmd

import (
	"context"
	"testing"

	"xdp/pkg/plugin"
)

func TestSortCommandSortsByMultipleFields(t *testing.T) {
	p := New()
	result, err := p.Execute(context.Background(), plugin.SearchCommandInput{
		Fields: []string{"level", "service", "count"},
		Rows: []map[string]any{
			{"level": "warn", "service": "checkout", "count": 1},
			{"level": "info", "service": "checkout", "count": 1},
			{"level": "warn", "service": "billing", "count": 1},
		},
	}, plugin.SearchCommand{Name: "sort", Args: []string{"level", "service"}, Raw: "sort level service"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := []string{
		result.Rows[0]["level"].(string) + "/" + result.Rows[0]["service"].(string),
		result.Rows[1]["level"].(string) + "/" + result.Rows[1]["service"].(string),
		result.Rows[2]["level"].(string) + "/" + result.Rows[2]["service"].(string),
	}
	want := []string{"info/checkout", "warn/billing", "warn/checkout"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows order = %#v, want %#v", got, want)
		}
	}
}

func TestSortCommandSupportsDescendingField(t *testing.T) {
	p := New()
	result, err := p.Execute(context.Background(), plugin.SearchCommandInput{
		Fields: []string{"service", "count"},
		Rows: []map[string]any{
			{"service": "a", "count": "2"},
			{"service": "b", "count": "10"},
		},
	}, plugin.SearchCommand{Name: "sort", Args: []string{"-", "count"}, Raw: "sort - count"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Rows[0]["service"] != "b" {
		t.Fatalf("rows = %#v, want count descending", result.Rows)
	}
}
