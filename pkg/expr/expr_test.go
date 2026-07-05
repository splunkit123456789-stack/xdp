package expr

import (
	"testing"
	"time"

	"xdp/pkg/event"
)

func TestEvalNullComparison(t *testing.T) {
	e := event.New("raw", event.Source{}, time.Now().UTC())
	matched, err := Eval("metadata.index == null", e)
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}
	if !matched {
		t.Fatal("missing metadata index should equal null")
	}
	e.Metadata["index"] = "app"
	matched, err = Eval("metadata.index != null", e)
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}
	if !matched {
		t.Fatal("present metadata index should not equal null")
	}
}

func TestEvalTagsContains(t *testing.T) {
	e := event.New("raw", event.Source{}, time.Now().UTC())
	e.Tags = []string{"firewall", "blocked"}
	matched, err := Eval("tags contains 'blocked'", e)
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}
	if !matched {
		t.Fatal("tags contains did not match existing tag")
	}
}

func TestEvalErrorsLength(t *testing.T) {
	e := event.New("raw", event.Source{}, time.Now().UTC())
	matched, err := Eval("errors.length == '0'", e)
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}
	if !matched {
		t.Fatal("errors.length did not match zero")
	}
}
