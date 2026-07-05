package eventtime

import (
	"testing"
	"time"
)

func TestFromRawParsesNamedField(t *testing.T) {
	got, err := FromRaw(`{"@timestamp":"2026-01-02T03:04:05Z","service":"api"}`, "@timestamp")
	if err != nil {
		t.Fatalf("FromRaw() error = %v", err)
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("time = %s, want %s", got, want)
	}
}

func TestFromRawParsesNestedUnixMillis(t *testing.T) {
	got, err := FromRaw(`{"event":{"ts":1767225600123}}`, "event.ts")
	if err != nil {
		t.Fatalf("FromRaw() error = %v", err)
	}
	want := time.Unix(1767225600, 123000000).UTC()
	if !got.Equal(want) {
		t.Fatalf("time = %s, want %s", got, want)
	}
}

func TestParseOptionalAcceptsDatetimeLocal(t *testing.T) {
	got, err := ParseOptional("2026-01-02T03:04")
	if err != nil {
		t.Fatalf("ParseOptional() error = %v", err)
	}
	want := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("time = %s, want %s", got, want)
	}
}
